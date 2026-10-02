// Package player implements the built-in MP3 preview player: a feeder
// goroutine decodes the file, downmixes it to mono, resamples it to the
// mixer's output rate and streams it into a shirei StreamVoice.
package player

import (
	"math"
	"os"
	"sync"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"go.hasen.dev/shirei/audio"
)

// OutRate is the playback sample rate handed to app.StartAudio.
const OutRate = 44100

// State is a snapshot of the player for the UI.
type State struct {
	Path    string
	Playing bool
	Pos     int64 // current frame, in OutRate frames
	Dur     int64 // total frames estimate, 0 when unknown
	Ready   bool
}

// Player plays one file at a time through the given mixer.
type Player struct {
	mixer *audio.Mixer

	mu      sync.Mutex
	gen     int64
	path    string
	dur     int64
	pos     int64
	playing bool
	voice   *audio.StreamVoice
}

// New returns a player that renders through mixer. The mixer's Fill must be
// wired to app.StartAudio by the caller.
func New(mixer *audio.Mixer) *Player {
	return &Player{mixer: mixer}
}

// State returns the current playback state.
func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return State{
		Path:    p.path,
		Playing: p.playing,
		Pos:     p.pos,
		Dur:     p.dur,
		Ready:   p.path != "",
	}
}

// Load selects a file, paused at position 0. An empty path unloads.
func (p *Player) Load(path string) {
	p.stopSession()
	dur := int64(0)
	if path != "" {
		dur = probeDuration(path)
	}
	p.mu.Lock()
	p.path = path
	p.dur = dur
	p.pos = 0
	p.playing = false
	p.mu.Unlock()
}

// Play starts (or resumes) playback of the loaded file.
func (p *Player) Play() {
	p.mu.Lock()
	if p.path == "" || p.playing {
		p.mu.Unlock()
		return
	}
	if p.dur > 0 && p.pos >= p.dur {
		p.pos = 0
	}
	p.playing = true
	gen := p.gen + 1
	p.gen = gen
	voice := audio.NewStreamVoice(OutRate / 2)
	p.voice = voice
	path, start := p.path, p.pos
	p.mu.Unlock()

	p.mixer.Add(voice)
	go p.feeder(gen, path, start, voice)
}

// Pause stops playback, keeping the position.
func (p *Player) Pause() {
	p.mu.Lock()
	if !p.playing {
		p.mu.Unlock()
		return
	}
	p.playing = false
	p.mu.Unlock()
	p.stopSession()
}

// Toggle plays when paused and pauses when playing.
func (p *Player) Toggle() {
	if p.State().Playing {
		p.Pause()
		return
	}
	p.Play()
}

// SeekBy moves the position by delta frames (negative to go back), clamped
// to the file bounds. Playback continues from the new position.
func (p *Player) SeekBy(delta int64) {
	p.mu.Lock()
	if p.path == "" {
		p.mu.Unlock()
		return
	}
	target := p.pos + delta
	if target < 0 {
		target = 0
	}
	if p.dur > 0 && target > p.dur {
		target = p.dur
	}
	p.pos = target
	path, start, play := p.path, target, p.playing
	p.mu.Unlock()

	p.stopSession()
	if play {
		p.beginSession(path, start)
	}
}

// stopSession releases the current voice and invalidates any feeder.
func (p *Player) stopSession() {
	p.mu.Lock()
	p.gen++
	v := p.voice
	p.voice = nil
	p.playing = false
	p.mu.Unlock()
	if v != nil {
		v.Release()
	}
}

// beginSession starts a new feeder from start, marking the player as playing.
func (p *Player) beginSession(path string, start int64) {
	p.mu.Lock()
	p.playing = true
	gen := p.gen + 1
	p.gen = gen
	voice := audio.NewStreamVoice(OutRate / 2)
	p.voice = voice
	p.mu.Unlock()

	p.mixer.Add(voice)
	go p.feeder(gen, path, start, voice)
}

func (p *Player) abandoned(gen int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gen != gen
}

func (p *Player) finish(gen int64, atEnd bool) {
	p.mu.Lock()
	if p.gen == gen {
		p.playing = false
		p.voice = nil
		if atEnd && p.dur > 0 {
			p.pos = p.dur
		}
	}
	p.mu.Unlock()
}

// feeder decodes the file and streams it into voice. It exits when the
// session generation moves on (seek, pause, load) or at end of file.
func (p *Player) feeder(gen int64, path string, start int64, voice *audio.StreamVoice) {
	f, err := os.Open(path)
	if err != nil {
		p.finish(gen, false)
		return
	}
	defer f.Close()
	dec, err := mp3.NewDecoder(f)
	if err != nil {
		p.finish(gen, false)
		return
	}

	rate := dec.SampleRate()
	rs := &resampler{}
	buf := make([]byte, 32*1024)

	// decode-discard everything before the requested position
	skip := start
	for skip > 0 {
		if p.abandoned(gen) {
			return
		}
		n, rerr := dec.Read(buf)
		if n > 0 {
			out := rs.Convert(decodeMono(buf[:n]), rate, OutRate)
			drop := int64(len(out))
			if drop > skip {
				drop = skip
			}
			skip -= drop
		}
		if rerr != nil {
			p.finish(gen, true)
			return
		}
	}

	var written int64
	for {
		if p.abandoned(gen) {
			return
		}
		n, rerr := dec.Read(buf)
		if n > 0 {
			out := rs.Convert(decodeMono(buf[:n]), rate, OutRate)
			if len(out) == 0 {
				continue
			}
			if _, werr := voice.Write(out); werr != nil {
				return
			}
			written += int64(len(out))
			p.mu.Lock()
			if p.gen == gen {
				p.pos = start + written - int64(voice.Buffered())
			}
			p.mu.Unlock()
		}
		if rerr != nil {
			break
		}
	}

	voice.Close()
	for {
		if p.abandoned(gen) {
			return
		}
		buffered := voice.Buffered()
		p.mu.Lock()
		if p.gen == gen {
			p.pos = start + written - int64(buffered)
		}
		drained := buffered == 0
		p.mu.Unlock()
		if drained {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	p.finish(gen, true)
}

// decodeMono converts interleaved stereo int16 LE PCM to mono float32 in
// the [-1, 1) range.
func decodeMono(pcm []byte) []float32 {
	n := len(pcm) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		l := int16(uint16(pcm[4*i]) | uint16(pcm[4*i+1])<<8)
		r := int16(uint16(pcm[4*i+2]) | uint16(pcm[4*i+3])<<8)
		out[i] = (float32(l) + float32(r)) / (2 * 32768)
	}
	return out
}

// probeDuration estimates the file duration in OutRate frames: from the
// decoder's length when known, else from the file size assuming 128 kbps.
func probeDuration(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	dec, err := mp3.NewDecoder(f)
	if err != nil {
		return 0
	}
	if n := dec.Length(); n > 0 {
		return n / 4 * OutRate / int64(dec.SampleRate())
	}
	fi, err := f.Stat()
	if err != nil {
		return 0
	}
	return int64(float64(fi.Size()) * 8 / 128000 * OutRate)
}

// resampler is a linear-interpolation converter carrying its fractional
// position and previous sample across calls. Position -1 is the previous
// batch's last sample, so consecutive batches interpolate seamlessly.
type resampler struct {
	prev float32
	t    float64
}

// Convert resamples one batch of mono samples from rate to out. The
// returned slice is freshly allocated (the feeder is not the audio thread).
func (r *resampler) Convert(src []float32, rate, out int) []float32 {
	if len(src) == 0 {
		return nil
	}
	if rate == out {
		r.prev = src[len(src)-1]
		r.t = 0
		return src
	}
	step := float64(rate) / float64(out)
	res := make([]float32, 0, int(float64(len(src))/step)+2)
	t := r.t
	for t < float64(len(src)-1) {
		i := int(math.Floor(t))
		var a, b float32
		if i < 0 {
			a, b = r.prev, src[0]
		} else {
			a, b = src[i], src[i+1]
		}
		frac := float32(t - math.Floor(t))
		res = append(res, a+(b-a)*frac)
		t += step
	}
	r.prev = src[len(src)-1]
	r.t = t - float64(len(src))
	return res
}
