package ui

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "go.hasen.dev/shirei"
	shireiAudio "go.hasen.dev/shirei/audio"

	"github.com/prune998/normMP3/internal/player"
	"github.com/prune998/normMP3/internal/waveform"
)

// fakeEnv builds a synthetic envelope shaped like a decaying sine so the
// modal has data to render without touching any MP3.
func fakeEnv(n int, amp float64) *waveform.Envelope {
	env := &waveform.Envelope{Rate: 44100, Frames: int64(n) * 1000}
	for i := 0; i < n; i++ {
		v := amp * (0.35 + 0.65*math.Abs(math.Sin(float64(i)*0.7)))
		env.Peaks = append(env.Peaks, float32(v))
		env.RMS = append(env.RMS, float32(v*0.6))
		if float32(v) > env.MaxPeak {
			env.MaxPeak = float32(v)
		}
	}
	return env
}

func resetWaveState() {
	waveCmp.row = nil
	waveCmp.mode = waveModeStacked
	waveCmp.busy = false
	waveCmp.status = ""
	waveCmp.progress = 0
	waveCmp.err = ""
	waveCmp.orig = nil
	waveCmp.norm = nil
	waveCmp.diff = nil
	waveCmp.laneOrig = waveLaneImg{}
	waveCmp.laneNorm = waveLaneImg{}
	waveCmp.laneOverlay = waveLaneImg{}
	waveCmp.laneGain = waveLaneImg{}
	waveCmp.gen++
	clearWaveCache()
}

func TestWaveModalRender(t *testing.T) {
	dir := t.TempDir()
	S.cwd = dir
	S.plyr = player.New(shireiAudio.NewMixer())
	S.rows = nil
	S.busy = false
	defer func() {
		S.modal = ""
		resetWaveState()
	}()

	r := &row{
		path:   "/x/Fichiers_normalises/a.mp3",
		src:    "/x/a.mp3",
		name:   "a.mp3",
		target: 89.0,
		level:  84.21,
		corr:   4.79,
		action: "Ampl.simple",
		level2: 89.10,
		done:   true,
	}

	// computing state
	S.modal = "wave"
	resetWaveState()
	waveCmp.row = r
	waveCmp.busy = true
	waveCmp.status = "Analyse de l'original…"
	waveCmp.progress = 0.3
	if err := RenderToPNG(filepath.Join(dir, "wave-busy.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("wave busy view: %v", err)
	}

	// error state
	resetWaveState()
	waveCmp.row = r
	waveCmp.err = "waveform: a.mp3: no decodable audio"
	if err := RenderToPNG(filepath.Join(dir, "wave-error.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("wave error view: %v", err)
	}

	// ready state with data
	resetWaveState()
	waveCmp.row = r
	waveCmp.orig = fakeEnv(waveform.DefaultBuckets, 0.55)
	waveCmp.norm = fakeEnv(waveform.DefaultBuckets, 0.95)
	waveCmp.norm.MaxPeak = 0.95
	waveCmp.diff = waveform.Compare(waveCmp.orig, waveCmp.norm)
	if err := RenderToPNG(filepath.Join(dir, "wave-ready.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("wave ready view: %v", err)
	}

	// diff mode: overlay + gain curve
	waveCmp.mode = waveModeDiff
	if err := RenderToPNG(filepath.Join(dir, "wave-diff.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("wave diff view: %v", err)
	}
	waveCmp.mode = waveModeStacked

	// dark mode rebuilds the lanes with different paint
	S.dark = true
	if err := RenderToPNG(filepath.Join(dir, "wave-ready-dark.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("wave ready dark view: %v", err)
	}
	S.dark = false

	for _, f := range []string{"wave-busy.png", "wave-error.png", "wave-ready.png", "wave-diff.png", "wave-ready-dark.png"} {
		if fi, err := os.Stat(filepath.Join(dir, f)); err != nil || fi.Size() < 1000 {
			t.Errorf("%s missing or suspiciously small", f)
		}
	}

	// the ready renders must have produced device-resolution lane images
	if waveCmp.laneOrig.img == nil || waveCmp.laneNorm.img == nil {
		t.Fatal("lane images were not rendered")
	}
	if waveCmp.laneOrig.key == waveCmp.laneNorm.key {
		t.Fatal("the two lanes share the same image key")
	}
}

func TestWaveLanePaint(t *testing.T) {
	env := fakeEnv(64, 0.8)
	pal := waveColorsNorm()
	img := renderWaveLane(env, 1280, 220, pal)
	if got := img.Bounds().Dx(); got != 1280 {
		t.Fatalf("width %d, want 1280", got)
	}
	if got := img.Bounds().Dy(); got != 220 {
		t.Fatalf("height %d, want 220", got)
	}

	// the lane must actually paint: count non-transparent pixels
	opaque := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.Pix[img.PixOffset(x, y)+3] > 0 {
				opaque++
			}
		}
	}
	total := 1280 * 220
	if opaque < total/4 {
		t.Fatalf("only %.1f%% of pixels painted", 100*float64(opaque)/float64(total))
	}

	// the center of the lane (between bars) must carry the center guide line
	mid := img.PixOffset(640, 110)
	if img.Pix[mid+3] == 0 {
		t.Fatal("center guide line missing")
	}

	// silence paints only the guides
	silent := &waveform.Envelope{Peaks: make([]float32, 64), RMS: make([]float32, 64)}
	imgSilent := renderWaveLane(silent, 400, 100, pal)
	almost := 0
	for i := 3; i < len(imgSilent.Pix); i += 4 {
		if imgSilent.Pix[i] > 0 {
			almost++
		}
	}
	if almost == 0 {
		t.Fatal("silent lane lost its guide lines")
	}
}

// isRedish reports whether the pixel reads as the diff red (R dominant).
func isRedish(pix []uint8, i int) bool {
	return pix[i+3] > 60 && pix[i] > pix[i+2]+30 && pix[i] > pix[i+1]+30
}

// isBlueish reports whether the pixel reads as the normalized blue (B dominant).
func isBlueish(pix []uint8, i int) bool {
	return pix[i+3] > 60 && pix[i+2] > pix[i]+30 && pix[i+2] > pix[i+1]+30
}

func countPixels(img *image.RGBA, pred func(pix []uint8, i int) bool) int {
	n := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if pred(img.Pix, i) {
			n++
		}
	}
	return n
}

func TestWaveOverlayPaint(t *testing.T) {
	// pure boost: nothing removed, no red anywhere
	orig := fakeEnv(64, 0.5)
	norm := fakeEnv(64, 0.9)
	boostDiff := waveform.Compare(orig, norm)
	imgBoost := renderWaveOverlayLane(orig, norm, boostDiff, 640, 220)
	if red := countPixels(imgBoost, isRedish); red != 0 {
		t.Fatalf("%d red pixels on a pure boost overlay, want 0", red)
	}
	if blue := countPixels(imgBoost, isBlueish); blue == 0 {
		t.Fatal("no blue pixels on a boost overlay")
	}

	// attenuation: the level taken out must paint red above the blue bars
	quiet := fakeEnv(64, 0.4)
	cutDiff := waveform.Compare(norm, quiet)
	if cutDiff.RemovedCount == 0 {
		t.Fatal("test data should flag removed buckets")
	}
	imgCut := renderWaveOverlayLane(norm, quiet, cutDiff, 640, 220)
	if red := countPixels(imgCut, isRedish); red == 0 {
		t.Fatal("no red pixels in an attenuated overlay")
	}
	if blue := countPixels(imgCut, isBlueish); blue == 0 {
		t.Fatal("no blue pixels in an attenuated overlay (the result stays blue)")
	}

	// identical envelopes: nothing removed
	same := waveform.Compare(orig, orig)
	imgSame := renderWaveOverlayLane(orig, orig, same, 640, 220)
	if red := countPixels(imgSame, isRedish); red != 0 {
		t.Fatalf("%d red pixels with identical envelopes, want 0", red)
	}
}

func TestWaveGainPaint(t *testing.T) {
	// attenuation: negative bars in red
	loud := fakeEnv(64, 0.9)
	quiet := fakeEnv(64, 0.4)
	cutDiff := waveform.Compare(loud, quiet)
	img := renderWaveGainLane(cutDiff, 640, 160)
	if red := countPixels(img, isRedish); red == 0 {
		t.Fatal("no red pixels in the gain curve of an attenuated file")
	}
	// the 0 dB center line must be present
	if img.Pix[img.PixOffset(320, 80)+3] == 0 {
		t.Fatal("gain curve center line missing")
	}

	// a pure boost paints blue (added level), not red
	boostDiff := waveform.Compare(quiet, loud)
	imgBoost := renderWaveGainLane(boostDiff, 640, 160)
	if red := countPixels(imgBoost, isRedish); red != 0 {
		t.Fatalf("%d red pixels in the gain curve of a pure boost, want 0", red)
	}
	if blue := countPixels(imgBoost, isBlueish); blue == 0 {
		t.Fatal("no blue pixels in the gain curve of a pure boost")
	}

	// a nil diff paints only guides
	imgEmpty := renderWaveGainLane(nil, 400, 100)
	n := 0
	for i := 3; i < len(imgEmpty.Pix); i += 4 {
		if imgEmpty.Pix[i] > 0 {
			n++
		}
	}
	if n == 0 {
		t.Fatal("nil diff lost its guide lines")
	}
}

func TestWaveOriginalPath(t *testing.T) {
	// built with filepath so the test passes on both POSIX and Windows
	// path separators (the CI run broke on a hardcoded "/m/album/…")
	root := filepath.Join(string(os.PathSeparator), "m", "album")
	norm := filepath.Join(root, outDirName, "song.mp3")
	if got := waveOriginalPath(&row{path: norm}); got != filepath.Join(root, "song.mp3") {
		t.Fatalf("derived original = %q, want %q", got, filepath.Join(root, "song.mp3"))
	}
	src := filepath.Join(root, "other.mp3")
	if got := waveOriginalPath(&row{path: norm, src: src}); got != src {
		t.Fatalf("src override = %q, want %q", got, src)
	}
	if got := waveOriginalPath(&row{path: filepath.Join(string(os.PathSeparator), "x", "song.mp3")}); got != "" {
		t.Fatalf("row outside %s should have no original, got %q", outDirName, got)
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("un deux trois quatre cinq six sept huit", 12)
	for _, line := range got {
		if len(line) > 12 {
			t.Fatalf("line %q longer than 12", line)
		}
	}
	if got := wrapText("abc", 10); len(got) != 1 || got[0] != "abc" {
		t.Fatalf("short text = %v", got)
	}
	if got := wrapText("ligne1\nligne2", 10); len(got) != 2 {
		t.Fatalf("newlines should be respected: %v", got)
	}
}
