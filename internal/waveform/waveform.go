// Package waveform computes peak-envelope waveform data from MP3 files for
// the original-vs-normalized comparison view. It decodes the file in pure Go
// (the same decoder as the built-in player), downmixes it to mono, and
// aggregates the samples into equal-duration buckets of peak and RMS
// amplitude.
package waveform

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	mp3dec "github.com/hajimehoshi/go-mp3"
)

// ProgressFn reports completion of the current file as a fraction in [0,1].
type ProgressFn = func(fraction float64)

// DefaultBuckets is the bucket count of the comparison waveforms.
const DefaultBuckets = 256

// blockFrames is the granularity of the streaming aggregation: the decoded
// stream is first aggregated into fixed-size blocks, then blocks are merged
// into the requested number of buckets. This bounds memory on long files
// without knowing the duration upfront.
const blockFrames = 512

var ErrNoSamples = errors.New("waveform: file contains no decodable audio")

// Block is the running aggregate of one fixed-size span of mono samples.
type Block struct {
	Peak  float32 // max absolute amplitude in the block
	SumSq float64 // sum of squared amplitudes
	Count int64   // number of samples
}

// Envelope is the waveform shape of one file, split into equal-duration
// buckets. Amplitudes are in the [0,1] range (mono mix of full scale).
type Envelope struct {
	Peaks   []float32 // max amplitude per bucket
	RMS     []float32 // RMS amplitude per bucket
	Frames  int64     // total decoded mono frames
	Rate    int       // sample rate of the decoded file
	MaxPeak float32   // loudest bucket peak
}

// Duration returns the length of the audio.
func (e *Envelope) Duration() time.Duration {
	if e == nil || e.Rate == 0 {
		return 0
	}
	return time.Duration(float64(e.Frames) / float64(e.Rate) * float64(time.Second))
}

// PeakDB returns the loudest bucket peak in dBFS (negative, or -99 for
// digital silence).
func (e *Envelope) PeakDB() float64 {
	if e == nil || e.MaxPeak <= 0 {
		return -99
	}
	return 20 * math.Log10(float64(e.MaxPeak))
}

// Compute decodes path and returns its waveform envelope over buckets
// buckets (DefaultBuckets when <= 0). Progress reports the decode fraction
// when the file length is known.
func Compute(path string, buckets int, progress ProgressFn) (*Envelope, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec, err := mp3dec.NewDecoder(f)
	if err != nil {
		return nil, fmt.Errorf("waveform: %s: %w", filepath.Base(path), err)
	}
	rate := dec.SampleRate()

	// Length is the estimated PCM byte count when a Xing/Info header is
	// present; fall back to the file size for progress only.
	total := dec.Length()
	if total <= 0 {
		if fi, serr := f.Stat(); serr == nil {
			total = fi.Size()
		}
	}

	var blocks []Block
	var frames int64
	var read int64
	buf := make([]byte, 64*1024)
	cur := Block{}

	flush := func() {
		if cur.Count > 0 {
			blocks = append(blocks, cur)
			cur = Block{}
		}
	}

	for {
		n, rerr := dec.Read(buf)
		if n > 0 {
			read += int64(n)
			// Interleaved stereo int16 LE PCM: 4 bytes per frame.
			for i := 0; i+3 < n; i += 4 {
				l := int16(uint16(buf[i]) | uint16(buf[i+1])<<8)
				r := int16(uint16(buf[i+2]) | uint16(buf[i+3])<<8)
				m := (float32(l) + float32(r)) / (2 * 32768)
				a := m
				if a < 0 {
					a = -a
				}
				if a > cur.Peak {
					cur.Peak = a
				}
				cur.SumSq += float64(m) * float64(m)
				cur.Count++
				frames++
				if cur.Count >= blockFrames {
					flush()
				}
			}
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
		return nil, rerr
	}
	flush()

	if frames == 0 {
		return nil, ErrNoSamples
	}
	peaks, rms := mergeBlocks(blocks, buckets)
	env := &Envelope{Peaks: peaks, RMS: rms, Frames: frames, Rate: rate}
	for _, p := range peaks {
		if p > env.MaxPeak {
			env.MaxPeak = p
		}
	}
	return env, nil
}

// mergeBlocks merges per-block aggregates into n buckets (fewer when the
// file has fewer blocks than n): each bucket takes the max peak of its
// blocks and their overall RMS.
func mergeBlocks(blocks []Block, n int) (peaks, rms []float32) {
	if n <= 0 {
		n = DefaultBuckets
	}
	count := len(blocks)
	if count > n {
		count = n
	}
	peaks = make([]float32, count)
	rms = make([]float32, count)
	for i := 0; i < count; i++ {
		lo := i * len(blocks) / count
		hi := (i + 1) * len(blocks) / count
		if hi <= lo {
			hi = lo + 1
		}
		var peak float32
		var sumSq, samples float64
		for _, b := range blocks[lo:hi] {
			if b.Peak > peak {
				peak = b.Peak
			}
			sumSq += b.SumSq
			samples += float64(b.Count)
		}
		peaks[i] = peak
		if samples > 0 {
			rms[i] = float32(math.Sqrt(sumSq / samples))
		}
	}
	return peaks, rms
}
