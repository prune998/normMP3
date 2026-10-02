package player

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"go.hasen.dev/shirei/audio"

	"github.com/prune998/normMP3/internal/ffmpeg"
)

// writeTestMP3 generates a 3 s 997 Hz sine MP3 with the embedded ffmpeg.
func writeTestMP3(t *testing.T, path string) {
	t.Helper()
	if err := ffmpeg.Run(
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=997:duration=3",
		"-filter:a", "volume=0.5", "-ac", "2", "-ar", "44100", "-b:a", "128k",
		path,
	); err != nil {
		t.Skipf("ffmpeg indisponible pour générer le fichier de test: %v", err)
	}
}

func TestResamplerIdentity(t *testing.T) {
	r := &resampler{}
	src := make([]float32, 1000)
	for i := range src {
		src[i] = float32(i)
	}
	out := r.Convert(src, 44100, 44100)
	if len(out) != len(src) {
		t.Fatalf("identity resample changed length: %d -> %d", len(src), len(out))
	}
	for i := range out {
		if out[i] != src[i] {
			t.Fatalf("identity resample changed sample %d: %v != %v", i, out[i], src[i])
		}
	}
}

func TestResamplerAcrossBatches(t *testing.T) {
	// 48000 -> 44100: feeding one sample at a time must produce a continuous
	// ramp identical to feeding everything in one batch.
	r1 := &resampler{}
	var all []float32
	for i := 0; i < 500; i++ {
		all = append(all, r1.Convert([]float32{float32(i)}, 48000, 44100)...)
	}

	r2 := &resampler{}
	src := make([]float32, 500)
	for i := range src {
		src[i] = float32(i)
	}
	one := r2.Convert(src, 48000, 44100)

	if len(one) != len(all) {
		t.Fatalf("batched length %d != single length %d", len(all), len(one))
	}
	for i := range one {
		if math.Abs(float64(one[i]-all[i])) > 1e-6 {
			t.Fatalf("sample %d differs: %v vs %v", i, all[i], one[i])
		}
	}

	want := 459
	if diff := len(one) - want; diff < -2 || diff > 2 {
		t.Fatalf("output length %d, want ~%d", len(one), want)
	}
}

func TestResamplerUpsample(t *testing.T) {
	r := &resampler{}
	src := make([]float32, 1000)
	for i := range src {
		src[i] = float32(i)
	}
	out := r.Convert(src, 22050, 44100)
	want := 2 * 1000
	if diff := len(out) - want; diff < -2 || diff > 2 {
		t.Fatalf("upsample length %d, want ~%d", len(out), want)
	}
}

func TestPlayerLifecycle(t *testing.T) {
	if _, err := ffmpeg.Executable(); err != nil {
		t.Skip("ffmpeg indisponible")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "t.mp3")
	writeTestMP3(t, path)

	mixer := audio.NewMixer()
	p := New(mixer)

	if st := p.State(); st.Ready {
		t.Fatal("player should start empty")
	}

	p.Load(path)
	st := p.State()
	if !st.Ready || st.Path != path {
		t.Fatalf("after Load: %+v", st)
	}
	if st.Dur <= 0 || math.Abs(float64(st.Dur)/OutRate-3.0) > 0.5 {
		t.Fatalf("duration estimate %.2f s, want ~3 s", float64(st.Dur)/OutRate)
	}
	if st.Playing {
		t.Fatal("Load should leave the player paused")
	}

	p.Play()
	st = p.State()
	if !st.Playing {
		t.Fatal("Play should start playback")
	}

	time.Sleep(300 * time.Millisecond)
	st = p.State()
	if !st.Playing {
		t.Fatal("playback should still be running")
	}
	if st.Pos > int64(0.3*OutRate) {
		t.Fatalf("position %.2f s too far after 0.3 s (ring never drains without a mixer consumer)",
			float64(st.Pos)/OutRate)
	}

	p.SeekBy(1 * OutRate)
	st = p.State()
	if st.Pos != 1*OutRate {
		t.Fatalf("position after +1 s seek = %.2f s, want 1 s", float64(st.Pos)/OutRate)
	}

	p.SeekBy(-3 * OutRate)
	st = p.State()
	if st.Pos != 0 {
		t.Fatalf("position after -3 s seek = %.2f s, want 0", float64(st.Pos)/OutRate)
	}

	p.SeekBy(100 * OutRate)
	st = p.State()
	if st.Pos != st.Dur {
		t.Fatalf("seek past end should clamp to duration: %.2f vs %.2f",
			float64(st.Pos)/OutRate, float64(st.Dur)/OutRate)
	}

	p.Pause()
	if p.State().Playing {
		t.Fatal("Pause should stop playback")
	}

	p.Pause()
	if p.State().Playing {
		t.Fatal("double Pause should be a no-op")
	}

	p.Load("")
	if p.State().Ready {
		t.Fatal("Load(\"\") should unload")
	}
}

func TestPlayerLoadMissing(t *testing.T) {
	mixer := audio.NewMixer()
	p := New(mixer)
	p.Load(filepath.Join(t.TempDir(), "missing.mp3"))
	if st := p.State(); st.Dur != 0 || !st.Ready {
		t.Fatalf("missing file: %+v", st)
	}
	p.Play()
	time.Sleep(100 * time.Millisecond)
	if p.State().Playing {
		t.Fatal("playing a missing file should end immediately")
	}
}
