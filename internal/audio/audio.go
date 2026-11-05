// Package audio implements the analysis and processing pipeline of the
// application: loudness is measured in pure Go with a ReplayGain 1.0
// analyzer (the same algorithm as mp3gain, validated against the C
// reference), and the files are re-encoded with the original
// application's ffmpeg commands (volume / loudnorm, -ar 44100 -ab 64k),
// using the ffmpeg binary embedded by internal/ffmpeg.
package audio

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	mp3dec "github.com/hajimehoshi/go-mp3"

	"github.com/prune998/normMP3/internal/ffmpeg"
	"github.com/prune998/normMP3/internal/rgain"
)

const (
	// RefLevel is the ReplayGain 1.0 reference loudness (89 dB), the same
	// reference mp3gain uses.
	RefLevel = 89.0
	// ClipSample is the first amplitude value considered clipping.
	ClipSample = 32767
	// bitrateKbps matches the original application's ffmpeg "-ab 64k".
	bitrateKbps = "64k"
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
		if rerr == nil {
			continue
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		return Level{}, rerr
	}

	if read == 0 {
		return Level{}, ErrNoSamples
	}
	return levelFrom(an.Gain(), maxAmp), nil
}

// ApplyGainFile re-encodes path with the original application's ffmpeg
// volume pass (volume=<gain>dB -ar 44100 -ab 64k), then re-measures the
// file (as the original re-ran mp3gain). peakAmp is the source peak from
// the analysis; the clipping flag is raised when the gain would push it
// past full scale, which is what mp3gain's float decode reports.
func ApplyGainFile(path string, gainDB float64, peakAmp int, progress ProgressFn) (Level, error) {
	tmp := filepath.Join(filepath.Dir(path), ".normmp3-tmp.mp3")
	os.Remove(tmp)
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error", "-progress", "pipe:1",
		"-i", path,
		"-filter:a", fmt.Sprintf("volume=%.2fdB", gainDB),
		"-ar", "44100", "-ab", bitrateKbps,
		tmp,
	}
	durUs := probeDurationMicros(path)
	if err := ffmpeg.RunProgress(args, durUs, progress); err != nil {
		os.Remove(tmp)
		return Level{}, fmt.Errorf("audio: %s: %w", filepath.Base(path), err)
	}
	lv, err := replaceAndMeasure(tmp, path)
	if err != nil {
		return Level{}, err
	}
	if peakAmp > 0 && float64(peakAmp)*math.Pow(10, gainDB/20) > float64(ClipSample) {
		lv.Clipping = true
	}
	return lv, nil
}

// LoudNormFile re-encodes path with the original application's
// loudness-normalization pass:
//
//	ffmpeg -i <f> -filter:a loudnorm=I=-(112-target):TP=-2:LRA=7 -ar 44100 -ab 64k
//
// then re-measures the file.
func LoudNormFile(path string, target float64, progress ProgressFn) (Level, error) {
	targetI := -(112.0 - target)
	tmp := filepath.Join(filepath.Dir(path), ".normmp3-tmp.mp3")
	os.Remove(tmp)
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error", "-progress", "pipe:1",
		"-i", path,
		"-filter:a", fmt.Sprintf("loudnorm=I=%.2f:TP=-2:LRA=7", targetI),
		"-ar", "44100", "-ab", bitrateKbps,
		tmp,
	}
	durUs := probeDurationMicros(path)
	if err := ffmpeg.RunProgress(args, durUs, progress); err != nil {
		os.Remove(tmp)
		return Level{}, fmt.Errorf("audio: %s: %w", filepath.Base(path), err)
	}
	return replaceAndMeasure(tmp, path)
}

// replaceAndMeasure renames tmp over path and measures the result with the
// ReplayGain analyzer (the original re-ran mp3gain on the output).
func replaceAndMeasure(tmp, path string) (Level, error) {
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return Level{}, err
	}
	lv, err := AnalyzeFile(path, nil)
	if err != nil {
		return Level{}, err
	}
	return lv, nil
}

// probeDurationMicros estimates the file duration in microseconds (from the
// Xing/Info length when present, else a 128 kbps size estimate), for the
// ffmpeg progress fraction.
func probeDurationMicros(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	dec, err := mp3dec.NewDecoder(f)
	if err != nil {
		return 0
	}
	sr := dec.SampleRate()
	if n := dec.Length(); n > 0 {
		return n / 4 * int64(1e6) / int64(sr)
	}
	fi, err := f.Stat()
	if err != nil {
		return 0
	}
	return int64(float64(fi.Size()) * 8 / 128000 * 1e6)
}
