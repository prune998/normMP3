// Package rgain implements ReplayGain 1.0 loudness analysis.
//
// Port of gain_analysis.c by David Robinson and Glen Sawyer (with Frank
// Klemm's optimizations), as shipped in LAME (LGPL-2.1+). Filter tables
// and the 89 dB calibration constant are unchanged.
package rgain

import (
	"errors"
	"math"
)

var ErrUnsupportedSampleRate = errors.New("rgain: unsupported sample rate (need 8000-48000 Hz MPEG rate)")

const (
	StepsPerDB    = 100.0
	MaxDB         = 120
	PinkRef       = 64.82
	RMSPercentile = 0.95
	MaxOrder      = 10
	// GainNotEnoughSamples is returned by Gain when no complete RMS
	// window was analyzed.
	GainNotEnoughSamples = -24601.0
)

var sampleRateIndex = map[int]int{
	48000: 0,
	44100: 1,
	32000: 2,
	24000: 3,
	22050: 4,
	16000: 5,
	12000: 6,
	11025: 7,
	8000:  8,
}

type channelState struct {
	in   [MaxOrder]float64
	yule [MaxOrder]float64
	bin  [2]float64
	bout [2]float64
}

func (c *channelState) step(x float64, k *[21]float64, b *[5]float64) float64 {
	y := k[0]*c.in[9] + k[1]*c.in[8] + k[2]*c.in[7] + k[3]*c.in[6] + k[4]*c.in[5] +
		k[5]*c.in[4] + k[6]*c.in[3] + k[7]*c.in[2] + k[8]*c.in[1] + k[9]*c.in[0] +
		k[10]*x -
		(k[11]*c.yule[9] + k[12]*c.yule[8] + k[13]*c.yule[7] + k[14]*c.yule[6] + k[15]*c.yule[5] +
			k[16]*c.yule[4] + k[17]*c.yule[3] + k[18]*c.yule[2] + k[19]*c.yule[1] + k[20]*c.yule[0])

	out := b[0]*c.bin[1] + b[2]*c.bin[0] + b[4]*y - b[1]*c.bout[1] - b[3]*c.bout[0]

	c.in[9], c.in[8], c.in[7], c.in[6], c.in[5], c.in[4], c.in[3], c.in[2], c.in[1], c.in[0] =
		c.in[8], c.in[7], c.in[6], c.in[5], c.in[4], c.in[3], c.in[2], c.in[1], c.in[0], x
	c.yule[9], c.yule[8], c.yule[7], c.yule[6], c.yule[5], c.yule[4], c.yule[3], c.yule[2], c.yule[1], c.yule[0] =
		c.yule[8], c.yule[7], c.yule[6], c.yule[5], c.yule[4], c.yule[3], c.yule[2], c.yule[1], c.yule[0], y
	c.bin[1], c.bin[0] = c.bin[0], y
	c.bout[1], c.bout[0] = c.bout[0], out
	return out
}

// Analyzer accumulates ReplayGain 1.0 statistics over a stream of samples.
type Analyzer struct {
	freqIdx      int
	sampleWindow int
	totsamp      int
	lsum, rsum   float64
	hist         [StepsPerDB * MaxDB]int32
	l, r         channelState
}

// NewAnalyzer returns an analyzer for the given sample rate.
func NewAnalyzer(sampleRate int) (*Analyzer, error) {
	idx, ok := sampleRateIndex[sampleRate]
	if !ok {
		return nil, ErrUnsupportedSampleRate
	}
	return &Analyzer{
		freqIdx:      idx,
		sampleWindow: (sampleRate*1 + 20 - 1) / 20,
	}, nil
}

// Add analyzes one batch of interleaved stereo samples. Samples must be
// in the int16 full-scale domain (±32768), e.g. float64(pcm16).
func (a *Analyzer) Add(interleaved []float64) {
	k := &abYule[a.freqIdx]
	b := &abButter[a.freqIdx]
	for i := 0; i+1 < len(interleaved); i += 2 {
		l := a.l.step(interleaved[i], k, b)
		r := a.r.step(interleaved[i+1], k, b)
		a.lsum += l * l
		a.rsum += r * r
		a.totsamp++
		if a.totsamp == a.sampleWindow {
			val := StepsPerDB * 10.0 * math.Log10((a.lsum+a.rsum)/float64(a.totsamp)*0.5+1.0e-37)
			ival := 0
			if val > 0 {
				ival = int(val)
				if ival >= len(a.hist) {
					ival = len(a.hist) - 1
				}
			}
			a.hist[ival]++
			a.lsum, a.rsum = 0, 0
			a.totsamp = 0
		}
	}
}

// AddChannels analyzes planar samples; mono input is duplicated to the
// right channel, matching the reference implementation.
func (a *Analyzer) AddChannels(channels ...[]float64) {
	k := &abYule[a.freqIdx]
	b := &abButter[a.freqIdx]
	n := len(channels[0])
	var rc []float64
	if len(channels) == 1 {
		rc = channels[0]
	} else {
		rc = channels[1]
	}
	for i := 0; i < n; i++ {
		l := a.l.step(channels[0][i], k, b)
		r := a.r.step(rc[i], k, b)
		a.lsum += l * l
		a.rsum += r * r
		a.totsamp++
		if a.totsamp == a.sampleWindow {
			val := StepsPerDB * 10.0 * math.Log10((a.lsum+a.rsum)/float64(a.totsamp)*0.5+1.0e-37)
			ival := 0
			if val > 0 {
				ival = int(val)
				if ival >= len(a.hist) {
					ival = len(a.hist) - 1
				}
			}
			a.hist[ival]++
			a.lsum, a.rsum = 0, 0
			a.totsamp = 0
		}
	}
}

// Gain returns the recommended gain change in dB (89 dB reference),
// identical in meaning to mp3gain's recommended change.
func (a *Analyzer) Gain() float64 {
	var elems int32
	for _, c := range a.hist {
		elems += c
	}
	if elems == 0 {
		return GainNotEnoughSamples
	}
	upper := int32(math.Ceil(float64(elems) * (1.0 - RMSPercentile)))
	var sum int32
	i := len(a.hist) - 1
	for ; i >= 0; i-- {
		sum += a.hist[i]
		if sum >= upper {
			break
		}
	}
	return PinkRef - float64(i)/StepsPerDB
}
