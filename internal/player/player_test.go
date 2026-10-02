package player

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/braheezy/shine-mp3/pkg/mp3"
	"go.hasen.dev/shirei/audio"
)

func genSine(sr int, seconds float64, freq, amp float64) []int16 {
	n := int(float64(sr) * seconds)
	pcm := make([]int16, 2*n)
	for i := 0; i < n; i++ {
		v := int16(math.Round(32767 * amp * math.Sin(2*math.Pi*freq*float64(i)/float64(sr))))
		pcm[2*i] = v
		pcm[2*i+1] = v
	}
	return pcm
}

func writeTestMP3(t *testing.T, path string, sr int, pcm []int16) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 128*1024)

	enc := mp3.NewEncoder(sr, 2)
	perPass := int(enc.Mpeg.GranulesPerFrame) * 576 * 2
	for i := 0; i < len(pcm); i += perPass {
		end := i + perPass
		pad := false
		if end > len(pcm) {
			end = len(pcm)
			pad = true
		}
		chunk := pcm[i:end]
		if pad {
			for len(chunk) < perPass {
				chunk = append(chunk, 0)
			}
		}
		out, written := enc.EncodeBufferInterleaved(chunk)
		if written > 0 {
			if _, err := w.Write(out[:written]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
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
	dir := t.TempDir()
	path := filepath.Join(dir, "t.mp3")
	writeTestMP3(t, path, 44100, genSine(44100, 3, 997, 0.5))

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
