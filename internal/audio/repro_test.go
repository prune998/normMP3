package audio

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	mp3dec "github.com/hajimehoshi/go-mp3"
	mp3enc "github.com/prune998/normMP3/internal/mp3enc"
)

func encodeWithPipeline(t *testing.T, dir string, sr, channels int) string {
	t.Helper()
	path := filepath.Join(dir, "src.mp3")
	// build a source mp3 at the given rate/channels, then run it through
	// the real reencode path so output is exactly what the app writes
	pcm := make([]int16, 2*sr*channels)
	for i := 0; i < 2*sr; i++ {
		v := int16(8000 + (i%1000)*3)
		if channels == 1 {
			pcm[i] = v
		} else {
			pcm[2*i] = v
			pcm[2*i+1] = v / 2
		}
	}
	f, _ := os.Create(path)
	w := bufio.NewWriterSize(f, 128*1024)
	enc := mp3enc.NewEncoder(sr, channels)
	if err := setBitrate(enc, encodeBitrateKbps); err != nil {
		t.Fatal(err)
	}
	perPass := int(enc.Mpeg.GranulesPerFrame) * granuleSize * channels
	for i := 0; i < len(pcm); i += perPass {
		end := i + perPass
		if end > len(pcm) {
			end = len(pcm)
		}
		chunk := pcm[i:end]
		for len(chunk) < perPass {
			chunk = append(chunk, 0)
		}
		out, n := enc.EncodeBufferInterleaved(chunk)
		if n > 0 {
			w.Write(out[:n])
		}
	}
	w.Flush()
	f.Close()
	return path
}

func TestReproFreeBitrate(t *testing.T) {
	dir := t.TempDir()
	for _, sr := range []int{44100, 48000, 32000, 24000, 22050, 16000} {
		for _, ch := range []int{1, 2} {
			src := encodeWithPipeline(t, dir, sr, ch)
			if _, err := ApplyGainFile(src, 2, nil); err != nil {
				t.Errorf("sr=%d ch=%d: reencode failed: %v", sr, ch, err)
				continue
			}
			// decode the output fully
			f, _ := os.Open(src)
			dec, err := mp3dec.NewDecoder(f)
			if err != nil {
				t.Errorf("sr=%d ch=%d: DECODE FAILED: %v", sr, ch, err)
				f.Close()
				continue
			}
			buf := make([]byte, 64*1024)
			var total int64
			for {
				n, rerr := dec.Read(buf)
				total += int64(n)
				if rerr != nil {
					break
				}
			}
			f.Close()
			// the output keeps the source rate and layout; go-mp3 always
			// yields 4 bytes per frame (stereo int16)
			want := int64(2*sr) * 4
			if total < want*9/10 || total > want*13/10 {
				t.Errorf("sr=%d ch=%d: decoded %d bytes, want ~%d", sr, ch, total, want)
				continue
			}
			t.Logf("sr=%d ch=%d: decoded %d bytes OK", sr, ch, total)
		}
	}
}
