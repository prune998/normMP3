package waveform

import (
	"math"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/prune998/normMP3/internal/ffmpeg"
)

// requireFFmpeg skips the test when no ffmpeg binary is available (the
// embedded payload is extracted on first use).
func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := ffmpeg.Executable(); err != nil {
		t.Skipf("ffmpeg indisponible: %v", err)
	}
}

// genTestMP3 generates a 3 s sine MP3 with the embedded ffmpeg. volume is a
// linear multiplier on the sine source's native amplitude (~0.084), so
// volume=6 yields a ~0.5 full-scale peak.
func genTestMP3(t *testing.T, path string, volume float64) {
	t.Helper()
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=997:duration=3",
		"-filter:a", "volume=" + strconv.FormatFloat(volume, 'f', 2, 64),
		"-ac", "2", "-ar", "44100", "-b:a", "128k",
		path,
	}
	if err := ffmpeg.Run(args...); err != nil {
		t.Fatalf("génération du fichier de test: %v", err)
	}
}

func TestMergeBlocks(t *testing.T) {
	blocks := []Block{
		{Peak: 0.5, SumSq: 0.25 * 1024, Count: 1024},
		{Peak: 0.2, SumSq: 0.04 * 1024, Count: 1024},
		{Peak: 0.9, SumSq: 0.81 * 1024, Count: 1024},
		{Peak: 0.4, SumSq: 0.16 * 1024, Count: 1024},
	}

	// more buckets than blocks: one bucket per block, no merging
	peaks, rms := mergeBlocks(blocks, 8)
	if len(peaks) != 4 || len(rms) != 4 {
		t.Fatalf("bucket count = %d/%d, want 4/4", len(peaks), len(rms))
	}

	// two buckets: max of each pair, RMS over the pair
	peaks, rms = mergeBlocks(blocks, 2)
	if len(peaks) != 2 {
		t.Fatalf("bucket count = %d, want 2", len(peaks))
	}
	if peaks[0] != 0.5 || peaks[1] != 0.9 {
		t.Fatalf("merged peaks = %v %v, want 0.5 0.9", peaks[0], peaks[1])
	}
	wantRMS0 := math.Sqrt((0.25 + 0.04) / 2)
	wantRMS1 := math.Sqrt((0.81 + 0.16) / 2)
	if math.Abs(float64(rms[0])-wantRMS0) > 1e-6 || math.Abs(float64(rms[1])-wantRMS1) > 1e-6 {
		t.Fatalf("merged rms = %v %v, want %v %v", rms[0], rms[1], wantRMS0, wantRMS1)
	}

	// uneven split: 3 buckets over 4 blocks
	peaks, _ = mergeBlocks(blocks, 3)
	if len(peaks) != 3 {
		t.Fatalf("bucket count = %d, want 3", len(peaks))
	}
	maxAll := peaks[0]
	if peaks[1] > maxAll {
		maxAll = peaks[1]
	}
	if peaks[2] > maxAll {
		maxAll = peaks[2]
	}
	if maxAll != 0.9 {
		t.Fatalf("max bucket peak = %v, want 0.9", maxAll)
	}
}

func TestMergeBlocksUnevenCounts(t *testing.T) {
	// the final block can be partial: RMS must weight by sample count
	blocks := []Block{
		{Peak: 1.0, SumSq: 1.0 * 1000, Count: 1000},
		{Peak: 0.0, SumSq: 0, Count: 24},
	}
	_, rms := mergeBlocks(blocks, 1)
	want := math.Sqrt(1000.0 / 1024.0)
	if math.Abs(float64(rms[0])-want) > 1e-6 {
		t.Fatalf("rms with partial block = %v, want %v", rms[0], want)
	}
}

func TestComputeSine(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp3")
	genTestMP3(t, path, 6.0)

	var lastFraction float64
	env, err := Compute(path, DefaultBuckets, func(f float64) { lastFraction = f })
	if err != nil {
		t.Fatal(err)
	}

	if len(env.Peaks) != DefaultBuckets || len(env.RMS) != DefaultBuckets {
		t.Fatalf("bucket count %d/%d, want %d/%d", len(env.Peaks), len(env.RMS), DefaultBuckets, DefaultBuckets)
	}
	if d := env.Duration(); math.Abs(d.Seconds()-3.0) > 0.3 {
		t.Fatalf("duration %.2f s, want ~3 s", d.Seconds())
	}
	if env.Rate != 44100 {
		t.Fatalf("sample rate %d, want 44100", env.Rate)
	}
	if env.MaxPeak < 0.4 || env.MaxPeak > 0.6 {
		t.Fatalf("max peak %.3f, want ~0.5 (sine at volume 6)", env.MaxPeak)
	}
	if lastFraction < 0.99 {
		t.Fatalf("progress ended at %.2f, want 1", lastFraction)
	}
	for i, p := range env.Peaks {
		if p < 0 || p > 1 {
			t.Fatalf("bucket %d peak %.3f out of [0,1]", i, p)
		}
		if env.RMS[i] < 0 || env.RMS[i] > p+1e-6 {
			t.Fatalf("bucket %d rms %.3f not in [0, peak %.3f]", i, env.RMS[i], p)
		}
	}
	// a steady sine fills the whole file: no silent bucket in the middle
	// (the first buckets hold the MP3 encoder delay, which is silent)
	for i, p := range env.Peaks {
		if i < 10 || i >= len(env.Peaks)-2 {
			continue
		}
		if p < 0.3 {
			t.Fatalf("bucket %d peak %.3f unexpectedly quiet", i, p)
		}
	}
}

func TestComputeLouderIsHigher(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	quiet := filepath.Join(dir, "q.mp3")
	loud := filepath.Join(dir, "l.mp3")
	genTestMP3(t, quiet, 1.0)
	genTestMP3(t, loud, 6.0)

	q, err := Compute(quiet, 32, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := Compute(loud, 32, nil)
	if err != nil {
		t.Fatal(err)
	}
	if l.MaxPeak <= q.MaxPeak*2 {
		t.Fatalf("loud file peak %.3f should far exceed quiet file peak %.3f", l.MaxPeak, q.MaxPeak)
	}
	if l.PeakDB() <= q.PeakDB() {
		t.Fatalf("loud file %.1f dBFS should exceed quiet %.1f dBFS", l.PeakDB(), q.PeakDB())
	}
}

func TestComputeMissing(t *testing.T) {
	if _, err := Compute(filepath.Join(t.TempDir(), "missing.mp3"), 32, nil); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestEnvelopeHelpers(t *testing.T) {
	var nilEnv *Envelope
	if nilEnv.Duration() != 0 {
		t.Fatal("nil envelope duration should be 0")
	}
	if nilEnv.PeakDB() != -99 {
		t.Fatal("nil envelope peak should be -99 dB")
	}
	e := &Envelope{Frames: 44100, Rate: 44100, MaxPeak: 0.5}
	if e.Duration() != 1e9 { // one second in nanoseconds
		t.Fatalf("duration %v, want 1s", e.Duration())
	}
	if math.Abs(e.PeakDB()+6.02) > 0.01 {
		t.Fatalf("peak %.2f dBFS, want -6.02", e.PeakDB())
	}
}
