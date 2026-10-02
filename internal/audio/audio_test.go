package audio

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/bogem/id3v2/v2"
	mp3enc "github.com/braheezy/shine-mp3/pkg/mp3"
	mp3dec "github.com/hajimehoshi/go-mp3"
	"github.com/prune998/normMP3/internal/rgain"
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

func writeTestMP3(t *testing.T, path string, sr int, pcm []int16, withTags bool) {
	writeTestMP3Ch(t, path, sr, pcm, 2, withTags)
}

func writeTestMP3Ch(t *testing.T, path string, sr int, pcm []int16, channels int, withTags bool) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 128*1024)
	if withTags {
		tag := id3v2.NewEmptyTag()
		tag.AddTextFrame(tag.CommonID("Title"), id3v2.EncodingUTF8, "Test Titre")
		tag.AddTextFrame(tag.CommonID("Artist"), id3v2.EncodingUTF8, "Artiste Test")
		if _, err := tag.WriteTo(w); err != nil {
			t.Fatal(err)
		}
	}

	enc := mp3enc.NewEncoder(sr, channels)
	if err := setBitrate(enc, encodeBitrateKbps); err != nil {
		t.Fatal(err)
	}
	perPass := int(enc.Mpeg.GranulesPerFrame) * granuleSize * channels
	if channels == 1 {
		perPass = int(enc.Mpeg.GranulesPerFrame) * granuleSize
	}
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

func TestAnalyzeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp3")
	sr := 44100
	writeTestMP3(t, path, sr, genSine(sr, 3, 997, 0.5), false)

	lv, err := AnalyzeFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	manual := analyzeManually(t, path)
	if math.Abs(lv.GainRec-manual.GainRec) > 1e-9 || lv.MaxAmp != manual.MaxAmp {
		t.Errorf("AnalyzeFile %+v != manual decode %+v", lv, manual)
	}

	pure := RefLevel - (64.82 - 20*math.Log10(0.5*32767/math.Sqrt2))
	if math.Abs(lv.Loudness-pure) > 12 {
		t.Errorf("loudness %.2f too far from pure-signal %.2f", lv.Loudness, pure)
	}
	if lv.Clipping {
		t.Error("unexpected clipping flag on quiet file")
	}
}

func TestApplyGainClipsAndKeepsTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.mp3")
	sr := 44100
	writeTestMP3(t, path, sr, genSine(sr, 3, 997, 0.5), true)

	before, err := AnalyzeFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	after, err := ApplyGainFile(path, 6, nil)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(after.Loudness-(before.Loudness+6)) > 0.8 {
		t.Errorf("after boost: loudness %.3f, want ~%.3f", after.Loudness, before.Loudness+6)
	}
	if !after.Clipping {
		t.Error("expected clipping flag after +6 dB on -6 dBFS sine")
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
	dir := t.TempDir()
	path := filepath.Join(dir, "c.mp3")
	sr := 44100
	writeTestMP3(t, path, sr, genSine(sr, 3, 997, 0.9), false)

	before, err := AnalyzeFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	gain := RefLevel - before.Loudness
	after, err := LoudNormFile(path, gain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(after.Loudness-RefLevel) > 0.6 {
		t.Errorf("loudnorm: loudness %.3f, want ~%.3f", after.Loudness, RefLevel)
	}
	if after.Clipping {
		t.Errorf("loudnorm output should not clip, maxAmp=%d", after.MaxAmp)
	}
	if after.MaxAmp > ClipSample {
		t.Errorf("maxAmp %d exceeds full scale", after.MaxAmp)
	}
}

func TestMonoStaysMono(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.mp3")
	sr := 44100

	n := sr * 2
	pcm := make([]int16, 2*n)
	for i := 0; i < n; i++ {
		v := int16(math.Round(12000 * math.Sin(2*math.Pi*997*float64(i)/float64(sr))))
		pcm[2*i] = v
		pcm[2*i+1] = v
	}
	writeTestMP3Ch(t, path, sr, pcm, 1, false)

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
	after, err := ApplyGainFile(path, -1, nil)
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
	dir := t.TempDir()
	path := filepath.Join(dir, "s.mp3")
	sr := 44100
	writeTestMP3(t, path, sr, genSine(sr, 2, 997, 0.5), false)

	ch, err := Channels(path)
	if err != nil {
		t.Fatal(err)
	}
	if ch != 2 {
		t.Fatalf("stereo source should report 2 channels, got %d", ch)
	}
	if _, err := LoudNormFile(path, -2, nil); err != nil {
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

func TestReencodePreservesInterleaving(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e.mp3")
	sr := 44100
	n := sr / 2
	pcm := make([]int16, 2*n)
	for i := 0; i < n; i++ {
		l := int16(math.Round(20000 * math.Sin(2*math.Pi*440*float64(i)/float64(sr))))
		r := int16(math.Round(8000 * math.Sin(2*math.Pi*3000*float64(i)/float64(sr))))
		pcm[2*i] = l
		pcm[2*i+1] = r
	}
	writeTestMP3(t, path, sr, pcm, false)

	if _, err := ApplyGainFile(path, -1, nil); err != nil {
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
	found := false
	for {
		m, rerr := dec.Read(buf)
		if m > 0 {
			for i := 0; i+7 < m; i += 8 {
				l := int16(uint16(buf[i]) | uint16(buf[i+1])<<8)
				r := int16(uint16(buf[i+2]) | uint16(buf[i+3])<<8)
				if l > 8000 && r < 4000 {
					found = true
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	if !found {
		t.Error("L/R channels appear swapped or mixed after re-encode")
	}
}
