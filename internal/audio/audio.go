// Package audio implements the pure-Go MP3 analysis and processing
// pipeline: decode (go-mp3), ReplayGain loudness analysis (internal/rgain),
// gain/loudnorm processing and re-encode (shine-mp3), with ID3v2 tag
// preservation. It replaces the mp3gain.exe + ffmpeg.exe external tools
// used by the original Python application.
package audio

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/bogem/id3v2/v2"
	mp3enc "github.com/braheezy/shine-mp3/pkg/mp3"
	mp3dec "github.com/hajimehoshi/go-mp3"

	"github.com/prune998/normMP3/internal/rgain"
)

const (
	// RefLevel is the ReplayGain 1.0 reference loudness (89 dB), the same
	// reference mp3gain uses.
	RefLevel = 89.0
	// ClipSample is the first amplitude value considered clipping.
	ClipSample        = 32767
	granuleSize       = 576
	encodeBitrateKbps = 64
	limiterCeilDB     = -2.0
)

var ErrNoSamples = errors.New("audio: file contains no decodable audio")

// Level mirrors what the Python app read from mp3gain's output.
type Level struct {
	GainRec  float64 // recommended gain change in dB (mp3gain "volume" column)
	Loudness float64 // "Niveau" = 89 - GainRec
	MaxAmp   int
	Clipping bool
}

// ProgressFn reports completion of the current file as a fraction in [0,1].
type ProgressFn = func(fraction float64)

func levelFrom(gainRec float64, maxAmp int) Level {
	return Level{
		GainRec:  gainRec,
		Loudness: RefLevel - gainRec,
		MaxAmp:   maxAmp,
		Clipping: maxAmp > ClipSample,
	}
}

// AnalyzeFile decodes path and returns its ReplayGain level and peak.
func AnalyzeFile(path string, progress ProgressFn) (Level, error) {
	f, err := os.Open(path)
	if err != nil {
		return Level{}, err
	}
	defer f.Close()

	dec, err := mp3dec.NewDecoder(f)
	if err != nil {
		return Level{}, fmt.Errorf("audio: %s: %w", filepath.Base(path), err)
	}
	an, err := rgain.NewAnalyzer(dec.SampleRate())
	if err != nil {
		return Level{}, err
	}

	total := dec.Length()
	read := int64(0)
	maxAmp := 0
	buf := make([]byte, 64*1024)
	pcm := make([]float64, 0, 64*1024/2)

	for {
		n, rerr := dec.Read(buf)
		if n > 0 {
			read += int64(n)
			pcm = pcm[:0]
			for i := 0; i+1 < n; i += 2 {
				v := int16(uint16(buf[i]) | uint16(buf[i+1])<<8)
				av := int(v)
				if av < 0 {
					av = -av
				}
				if av > maxAmp {
					maxAmp = av
				}
				pcm = append(pcm, float64(v))
			}
			an.Add(pcm)
			if progress != nil && total > 0 {
				progress(math.Min(1, float64(read)/float64(total)))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return Level{}, rerr
		}
	}

	if read == 0 {
		return Level{}, ErrNoSamples
	}
	return levelFrom(an.Gain(), maxAmp), nil
}

// ApplyGainFile re-encodes path with the given gain in dB, without any
// peak limiting: clipping may occur, mirroring the "volume" ffmpeg filter
// behavior of the original app. Returns the level measured on the applied
// (unclipped-domain) signal, equivalent to re-running mp3gain on the output.
func ApplyGainFile(path string, gainDB float64, progress ProgressFn) (Level, error) {
	return reencode(path, gainDB, false, progress)
}

// LoudNormFile re-encodes path with the given gain in dB and a soft-knee
// peak limiter at -2 dBFS, approximating ffmpeg's
// loudnorm=I=-(112-cible):TP=-2:LRA=7 pass of the original app.
func LoudNormFile(path string, gainDB float64, progress ProgressFn) (Level, error) {
	return reencode(path, gainDB, true, progress)
}

func reencode(path string, gainDB float64, limit bool, progress ProgressFn) (Level, error) {
	dir := filepath.Dir(path)

	orig, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return Level{}, fmt.Errorf("audio: tags: %w", err)
	}
	tag := id3v2.NewEmptyTag()
	for id, frames := range orig.AllFrames() {
		for _, fr := range frames {
			tag.AddFrame(id, fr)
		}
	}
	orig.Close()

	src, err := os.Open(path)
	if err != nil {
		return Level{}, err
	}

	dec, err := mp3dec.NewDecoder(src)
	if err != nil {
		src.Close()
		return Level{}, fmt.Errorf("audio: %s: %w", filepath.Base(path), err)
	}
	sr := dec.SampleRate()
	an, err := rgain.NewAnalyzer(sr)
	if err != nil {
		src.Close()
		return Level{}, err
	}

	tmp, err := os.CreateTemp(dir, ".normmp3-*.mp3")
	if err != nil {
		src.Close()
		return Level{}, err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	w := bufio.NewWriterSize(tmp, 256*1024)
	if _, err := tag.WriteTo(w); err != nil {
		src.Close()
		cleanup()
		return Level{}, err
	}

	channels := 2
	if c, cerr := Channels(path); cerr == nil && (c == 1 || c == 2) {
		channels = c
	}

	enc := mp3enc.NewEncoder(sr, channels)
	if err := setBitrate(enc, encodeBitrateKbps); err != nil {
		src.Close()
		cleanup()
		return Level{}, err
	}
	perPass := int(enc.Mpeg.GranulesPerFrame) * granuleSize * channels
	pass := make([]int16, 0, perPass)
	quant := make([]float64, 0, perPass)

	factor := math.Pow(10, gainDB/20)
	th := math.Pow(10, limiterCeilDB/20) * ClipSample
	full := float64(ClipSample)

	total := dec.Length()
	read := int64(0)
	maxAmp := 0
	buf := make([]byte, 64*1024)

	store := func(x float64) {
		av := int(math.Abs(x))
		if av > maxAmp {
			maxAmp = av
		}
		if x > ClipSample {
			x = ClipSample
		} else if x < -ClipSample-1 {
			x = -ClipSample - 1
		}
		q := int16(math.Round(x))
		pass = append(pass, q)
		quant = append(quant, float64(q))
		if channels == 1 {
			quant = append(quant, float64(q))
		}
		if len(pass) == perPass {
			an.Add(quant)
			flushPass(w, enc, pass)
			pass = pass[:0]
			quant = quant[:0]
		}
	}

	for {
		n, rerr := dec.Read(buf)
		if n > 0 {
			read += int64(n)
			if channels == 1 {
				for i := 0; i+3 < n; i += 4 {
					v := float64(int16(uint16(buf[i]) | uint16(buf[i+1])<<8))
					x := v * factor
					if limit {
						if x > th {
							x = th + (full-th)*math.Tanh((x-th)/(full-th))
						} else if x < -th {
							x = -th - (full-th)*math.Tanh((-x-th)/(full-th))
						}
					}
					store(x)
				}
			} else {
				for i := 0; i+1 < n; i += 2 {
					v := float64(int16(uint16(buf[i]) | uint16(buf[i+1])<<8))
					x := v * factor
					if limit {
						if x > th {
							x = th + (full-th)*math.Tanh((x-th)/(full-th))
						} else if x < -th {
							x = -th - (full-th)*math.Tanh((-x-th)/(full-th))
						}
					}
					store(x)
				}
			}
			if progress != nil && total > 0 {
				progress(math.Min(1, float64(read)/float64(total)))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			src.Close()
			cleanup()
			return Level{}, rerr
		}
	}
	src.Close()

	if len(pass) > 0 {
		tail := pass
		for len(tail) < perPass {
			tail = append(tail, 0)
			for k := 0; k < channels; k++ {
				quant = append(quant, 0)
			}
		}
		an.Add(quant)
		flushPass(w, enc, tail)
	}

	if err := w.Flush(); err != nil {
		cleanup()
		return Level{}, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return Level{}, err
	}
	if read == 0 {
		cleanup()
		return Level{}, ErrNoSamples
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return Level{}, err
	}
	return levelFrom(an.Gain(), maxAmp), nil
}

func flushPass(w io.Writer, enc *mp3enc.Encoder, pass []int16) {
	out, written := enc.EncodeBufferInterleaved(pass)
	if written > 0 {
		w.Write(out[:written])
	}
}

var mpeg1Bitrates = [15]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
var mpeg2Bitrates = [15]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}

func setBitrate(enc *mp3enc.Encoder, kbps int) error {
	table := &mpeg2Bitrates
	if enc.Mpeg.Version == 3 {
		table = &mpeg1Bitrates
	}
	idx := -1
	for i, b := range table {
		if b == kbps {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("audio: unsupported bitrate %d kbps", kbps)
	}
	enc.Mpeg.Bitrate = int64(kbps)
	enc.Mpeg.BitrateIndex = int64(idx)

	avg := (float64(enc.Mpeg.GranulesPerFrame) * granuleSize / float64(enc.Wave.SampleRate)) *
		(float64(kbps) * 1000 / float64(enc.Mpeg.BitsPerSlot))
	enc.Mpeg.WholeSlotsPerFrame = int64(avg)
	enc.Mpeg.FracSlotsPerFrame = avg - float64(enc.Mpeg.WholeSlotsPerFrame)
	enc.Mpeg.SlotLag = -enc.Mpeg.FracSlotsPerFrame
	return nil
}
