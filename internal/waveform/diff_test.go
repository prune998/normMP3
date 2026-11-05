package waveform

import (
	"math"
	"testing"
)

// envFromRMS builds a synthetic envelope from RMS values (peaks mirror the
// RMS so limiting tests can shrink peaks independently).
func envFromRMS(rms []float64) *Envelope {
	e := &Envelope{Rate: 44100, Frames: int64(len(rms)) * 512}
	for _, v := range rms {
		e.RMS = append(e.RMS, float32(v))
		e.Peaks = append(e.Peaks, float32(v*1.2))
	}
	return e
}

func TestCompareIdentical(t *testing.T) {
	e := envFromRMS([]float64{0.5, 0.4, 0.3, 0.5, 0.2})
	d := Compare(e, e)
	if d == nil {
		t.Fatal("Compare returned nil")
	}
	if d.ChangedCount != 0 {
		t.Fatalf("%d buckets changed on identical envelopes, want 0", d.ChangedCount)
	}
	if d.RemovedCount != 0 {
		t.Fatalf("%d buckets removed on identical envelopes, want 0", d.RemovedCount)
	}
	for i, g := range d.GainDB {
		if g != 0 {
			t.Fatalf("bucket %d gain %v, want 0", i, g)
		}
	}
	if d.MaxGain != 0 {
		t.Fatalf("max gain %v, want 0", d.MaxGain)
	}
}

func TestCompareUniformBoost(t *testing.T) {
	a := envFromRMS([]float64{0.5, 0.4, 0.3, 0.5, 0.2})
	// +6.02 dB: doubled RMS
	b := envFromRMS([]float64{1.0, 0.8, 0.6, 1.0, 0.4})
	d := Compare(a, b)
	if d.ChangedCount != len(b.Peaks) {
		t.Fatalf("%d buckets changed, want all %d", d.ChangedCount, len(b.Peaks))
	}
	if d.RemovedCount != 0 {
		t.Fatalf("%d buckets removed on a pure boost, want 0", d.RemovedCount)
	}
	for i, g := range d.GainDB {
		if math.Abs(float64(g)-6.02) > 0.1 {
			t.Fatalf("bucket %d gain %.2f dB, want ~6.02", i, g)
		}
	}
	if math.Abs(d.MaxGain-6.02) > 0.1 {
		t.Fatalf("max gain %.2f, want ~6.02", d.MaxGain)
	}
}

func TestCompareUniformCut(t *testing.T) {
	// -6.02 dB: halved RMS — the whole level was removed
	a := envFromRMS([]float64{1.0, 0.8, 0.6, 1.0, 0.4})
	b := envFromRMS([]float64{0.5, 0.4, 0.3, 0.5, 0.2})
	d := Compare(a, b)
	if d.ChangedCount != len(b.Peaks) || d.RemovedCount != len(b.Peaks) {
		t.Fatalf("changed=%d removed=%d, want all=%d", d.ChangedCount, d.RemovedCount, len(b.Peaks))
	}
	for i, g := range d.GainDB {
		if math.Abs(float64(g)+6.02) > 0.1 {
			t.Fatalf("bucket %d gain %.2f dB, want ~-6.02", i, g)
		}
		if !d.Removed[i] {
			t.Fatalf("bucket %d not flagged removed", i)
		}
	}
}

func TestCompareSmallChangesAreUnchanged(t *testing.T) {
	// +0.2 dB per bucket: below the 0.5 dB threshold
	a := envFromRMS([]float64{0.5, 0.4, 0.3})
	b := envFromRMS([]float64{0.5 * math.Pow(10, 0.2/20), 0.4 * math.Pow(10, 0.2/20), 0.3 * math.Pow(10, 0.2/20)})
	d := Compare(a, b)
	if d.ChangedCount != 0 {
		t.Fatalf("%d buckets changed for a %.2f dB nudge, want 0", d.ChangedCount, 0.2)
	}
	if d.RemovedCount != 0 {
		t.Fatalf("%d buckets removed for a %.2f dB nudge, want 0", d.RemovedCount, 0.2)
	}
}

func TestCompareLimitingFlags(t *testing.T) {
	// same RMS but the peaks got shaved by the limiter: the peak excess was
	// removed, so the bucket must be flagged removed even though the RMS
	// gain is ~0
	a := envFromRMS([]float64{0.5, 0.5})
	b := envFromRMS([]float64{0.5, 0.5})
	for i := range b.Peaks {
		b.Peaks[i] = a.Peaks[i] * 0.5 // -6 dB peak reduction
	}
	d := Compare(a, b)
	if d.ChangedCount != len(b.Peaks) {
		t.Fatalf("%d buckets changed, want all (limiting)", d.ChangedCount)
	}
	if d.RemovedCount != len(b.Peaks) {
		t.Fatalf("%d buckets removed, want all (limiting)", d.RemovedCount)
	}
}

func TestCompareBoostWithLimitedPeaks(t *testing.T) {
	// RMS up (boost) but peaks shaved: level was added AND the peak excess
	// removed — flagged changed and removed
	a := envFromRMS([]float64{0.5})
	b := envFromRMS([]float64{1.0})
	b.Peaks[0] = a.Peaks[0] * 0.8 // limiter kept the peak below the original
	d := Compare(a, b)
	if !d.Changed[0] || !d.Removed[0] {
		t.Fatalf("boost+limit: changed=%v removed=%v, want true/true", d.Changed[0], d.Removed[0])
	}
}

func TestCompareSilence(t *testing.T) {
	// sound -> silence removes everything; silence -> sound adds
	a := envFromRMS([]float64{0, 0.5})
	b := envFromRMS([]float64{0.4, 0})
	d := Compare(a, b)
	if !d.Changed[0] || d.Removed[0] || d.GainDB[0] != gainClampDB {
		t.Fatalf("silence→sound: changed=%v removed=%v gain=%v, want true/false/%v",
			d.Changed[0], d.Removed[0], d.GainDB[0], gainClampDB)
	}
	if !d.Changed[1] || !d.Removed[1] || d.GainDB[1] != -gainClampDB {
		t.Fatalf("sound→silence: changed=%v removed=%v gain=%v, want true/true/-%v",
			d.Changed[1], d.Removed[1], d.GainDB[1], gainClampDB)
	}
	// double silence stays unchanged and silent
	d = Compare(envFromRMS([]float64{0}), envFromRMS([]float64{0}))
	if d.ChangedCount != 0 || d.RemovedCount != 0 || d.GainDB[0] != 0 {
		t.Fatalf("silence→silence: %+v", d)
	}
}

func TestCompareDifferentLengths(t *testing.T) {
	// the two files can decode into slightly different bucket counts;
	// b's buckets must map proportionally into a's
	a := envFromRMS([]float64{0.5, 0.5, 0.5, 0.5}) // 4 buckets
	b := envFromRMS([]float64{1.0, 1.0, 1.0})      // 3 buckets (+6 dB)
	d := Compare(a, b)
	if len(d.Changed) != 3 {
		t.Fatalf("diff sized by the after envelope: %d, want 3", len(d.Changed))
	}
	if d.ChangedCount != 3 {
		t.Fatalf("%d buckets changed, want 3", d.ChangedCount)
	}
	for i, g := range d.GainDB {
		if math.Abs(float64(g)-6.02) > 0.1 {
			t.Fatalf("bucket %d gain %.2f, want ~6.02", i, g)
		}
	}
}

func TestCompareEmpty(t *testing.T) {
	if d := Compare(nil, envFromRMS([]float64{0.5})); d != nil {
		t.Fatal("nil before envelope should give a nil diff")
	}
	if d := Compare(envFromRMS([]float64{0.5}), &Envelope{}); d != nil {
		t.Fatal("empty after envelope should give a nil diff")
	}
}
