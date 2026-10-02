package audio

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/bogem/id3v2/v2"
	mp3dec "github.com/hajimehoshi/go-mp3"

	"github.com/prune998/normMP3/internal/ffmpeg"
	"github.com/prune998/normMP3/internal/rgain"
)

// requireFFmpeg skips the test when no ffmpeg binary is available (the
// embedded payload is extracted on first use).
func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := ffmpeg.Executable(); err != nil {
		t.Skipf("ffmpeg indisponible: %v", err)
	}
}

// genTestMP3 generates a 2 s 997 Hz sine MP3 with the embedded ffmpeg.
func genTestMP3(t *testing.T, path string, channels int, withTags bool) {
	t.Helper()
	meta := []string{}
	if withTags {
		meta = []string{"-metadata", "title=Test Titre", "-metadata", "artist=Artiste Test"}
	}
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=997:duration=2",
		"-ac", strconv.Itoa(channels), "-ar", "44100", "-b:a", "128k",
	}
	args = append(args, meta...)
	args = append(args, path)
	if err := ffmpeg.Run(args...); err != nil {
		t.Fatalf("génération du fichier de test: %v", err)
	}
}

func TestAnalyzeFile(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp3")
	genTestMP3(t, path, 2, false)

	lv, err := AnalyzeFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	manual := analyzeManually(t, path)
	if math.Abs(lv.GainRec-manual.GainRec) > 1e-9 || lv.MaxAmp != manual.MaxAmp {
		t.Errorf("AnalyzeFile %+v != manual decode %+v", lv, manual)
	}
	if lv.Loudness < 60 || lv.Loudness > 120 {
		t.Errorf("loudness %.2f outside sane range", lv.Loudness)
	}
	if lv.Clipping {
		t.Error("unexpected clipping flag on quiet file")
	}
}

func TestApplyGainClipsAndKeepsTags(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "b.mp3")
	genTestMP3(t, path, 2, true)

	before, err := AnalyzeFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	// the clipping flag is driven by the analysis-time peak: a +6 dB gain
	// on a file peaking at 20000 must be flagged (20000*2 > 32767)
	after, err := ApplyGainFile(path, 6, 20000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(after.Loudness-(before.Loudness+6)) > 0.8 {
		t.Errorf("after boost: loudness %.3f, want ~%.3f", after.Loudness, before.Loudness+6)
	}
	if !after.Clipping {
		t.Error("expected clipping flag when the gain doubles a 20000 peak")
	}

	after2, err := ApplyGainFile(path, 6, 10000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after2.Clipping {
		t.Error("unexpected clipping flag when the peak stays under full scale")
	}

	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tag.Close()
	tf := tag.GetTextFrame(tag.CommonID("Title"))
	if tf.Text != "Test Titre" {
		t.Errorf("title not preserved: %q", tf.Text)
	}
}

func TestLoudNorm(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "c.mp3")
	genTestMP3(t, path, 2, false)

	if _, err := AnalyzeFile(path, nil); err != nil {
		t.Fatal(err)
	}

	// loudnorm I=-(112-89)=-23 LUFS lands near cible-5 on the ReplayGain
	// scale; the two meters weight signals differently, so keep a wide band
	after, err := LoudNormFile(path, RefLevel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(after.Loudness-(RefLevel-5)) > 6 {
		t.Errorf("loudnorm: loudness %.3f, want ~%.3f", after.Loudness, RefLevel-5)
	}
	if after.MaxAmp > ClipSample {
		t.Errorf("maxAmp %d exceeds full scale", after.MaxAmp)
	}
}

func TestMonoStaysMono(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "m.mp3")
	genTestMP3(t, path, 1, false)

	ch, err := Channels(path)
	if err != nil {
		t.Fatal(err)
	}
	if ch != 1 {
		t.Fatalf("test source should be mono, header says %d", ch)
	}

	before, err := AnalyzeFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ApplyGainFile(path, -1, before.MaxAmp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(after.Loudness-(before.Loudness-1)) > 0.8 {
		t.Errorf("mono gain: loudness %.3f, want ~%.3f", after.Loudness, before.Loudness-1)
	}

	ch, err = Channels(path)
	if err != nil {
		t.Fatal(err)
	}
	if ch != 1 {
		t.Fatalf("re-encoded file should stay mono, header says %d", ch)
	}
}

func TestStereoStaysStereo(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "s.mp3")
	genTestMP3(t, path, 2, false)

	ch, err := Channels(path)
	if err != nil {
		t.Fatal(err)
	}
	if ch != 2 {
		t.Fatalf("stereo source should report 2 channels, got %d", ch)
	}
	if _, err := LoudNormFile(path, RefLevel, nil); err != nil {
		t.Fatal(err)
	}
	ch, err = Channels(path)
	if err != nil {
		t.Fatal(err)
	}
	if ch != 2 {
		t.Fatalf("re-encoded file should stay stereo, got %d", ch)
	}
}

// TestReencodePreservesInterleaving checks that a loud-left / silent-right
// file comes back with the same channel assignment.
func TestReencodePreservesInterleaving(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "e.mp3")

	if err := ffmpeg.Run(
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=3000:duration=1",
		"-filter_complex", "[0:a]volume=7[l];[1:a]volume=0.6[r];[l][r]join=inputs=2:channel_layout=stereo",
		"-ar", "44100", "-b:a", "128k", path,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := ApplyGainFile(path, -1, 0, nil); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec, err := mp3dec.NewDecoder(f)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64*1024)
	leftDominant := false
	for {
		m, rerr := dec.Read(buf)
		if m > 0 {
			for i := 0; i+7 < m; i += 8 {
				l := int16(uint16(buf[i]) | uint16(buf[i+1])<<8)
				r := int16(uint16(buf[i+2]) | uint16(buf[i+3])<<8)
				al, ar := int(l), int(r)
				if al < 0 {
					al = -al
				}
				if ar < 0 {
					ar = -ar
				}
				// left carries a loud tone, right a faint one: at the left
				// peaks the right channel is far below 5x
				if al > 10000 && al > 5*ar+500 {
					leftDominant = true
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	if !leftDominant {
		t.Error("channel assignment changed (left is no longer dominant)")
	}
}

func TestNoSamplesError(t *testing.T) {
	if _, err := AnalyzeFile(filepath.Join(t.TempDir(), "missing.mp3"), nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want not-exist error, got %v", err)
	}
}

func analyzeManually(t *testing.T, path string) Level {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec, err := mp3dec.NewDecoder(f)
	if err != nil {
		t.Fatal(err)
	}
	a, err := rgain.NewAnalyzer(dec.SampleRate())
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64*1024)
	pcm := make([]float64, 0, 64*1024/2)
	maxAmp := 0
	for {
		n, rerr := dec.Read(buf)
		if n > 0 {
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
			a.Add(pcm)
		}
		if rerr != nil {
			break
		}
	}
	return levelFrom(a.Gain(), maxAmp)
}
