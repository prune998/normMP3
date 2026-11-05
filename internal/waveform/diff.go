// Diff support for the comparison view: Compare aligns two envelopes of the
// same music (before / after processing) bucket by bucket and flags what
// normalization did to each bucket, so the UI can paint the removed parts of
// the waveform in red.
package waveform

import "math"

// DiffThresholdDB is the per-bucket level change (in dB) above which a
// bucket counts as modified. Below that, gain-staging noise and MP3
// re-encoding artifacts are considered unchanged.
const DiffThresholdDB = 0.5

// gainClampDB bounds the reported per-bucket gain when one side is silent
// (the exact value would be infinite).
const gainClampDB = 20.0

// silenceAmp is the amplitude under which a bucket counts as digital
// silence (about -100 dBFS).
const silenceAmp = 1e-5

// Diff is the per-bucket comparison of two envelopes. Arrays are indexed by
// the "after" envelope's buckets (the normalized file), so they line up
// with the lane drawn for it.
type Diff struct {
	Changed      []bool    // bucket modified beyond DiffThresholdDB (either way)
	Removed      []bool    // level taken out: attenuated or peak-limited bucket
	GainDB       []float32 // level change in dB (after vs before), 0 when silent
	MaxGain      float64   // max |GainDB| across the file, for scaling the gain curve
	ChangedCount int       // how many buckets are flagged as modified
	RemovedCount int       // how many buckets had level removed
}

// Compare aligns envelopes a (before) and b (after) in time — both span the
// whole song, so b's bucket i maps to a's proportional bucket — and reports
// which buckets changed, and which of them had level removed: RMS level
// brought down, or peaks shaved by the limiter.
func Compare(a, b *Envelope) *Diff {
	if a == nil || b == nil {
		return nil
	}
	n := len(b.Peaks)
	if n == 0 || len(a.Peaks) == 0 {
		return nil
	}
	d := &Diff{
		Changed: make([]bool, n),
		Removed: make([]bool, n),
		GainDB:  make([]float32, n),
	}
	na := len(a.Peaks)
	for i := 0; i < n; i++ {
		j := i * na / n
		or := float64(a.RMS[j])
		nr := float64(b.RMS[i])
		op := float64(a.Peaks[j])
		np := float64(b.Peaks[i])

		switch {
		case or < silenceAmp && nr < silenceAmp:
			continue // silence stays silence: unchanged, gain 0
		case or < silenceAmp:
			d.set(i, gainClampDB, false) // sound added out of silence
		case nr < silenceAmp:
			d.set(i, -gainClampDB, true) // sound removed into silence
		default:
			gain := 20 * math.Log10(nr/or)
			limited := op > silenceAmp && np < op*math.Pow(10, -DiffThresholdDB/20)
			if math.Abs(gain) > DiffThresholdDB || limited {
				d.set(i, gain, gain < -DiffThresholdDB || limited)
			}
		}
	}
	return d
}

// set flags bucket i as modified with the given clamped gain, and as
// removed when the processing took level out of it.
func (d *Diff) set(i int, gain float64, removed bool) {
	d.Changed[i] = true
	d.GainDB[i] = float32(gain)
	if removed {
		d.Removed[i] = true
		d.RemovedCount++
	}
	d.ChangedCount++
	if math.Abs(gain) > d.MaxGain {
		d.MaxGain = math.Abs(gain)
	}
}
