package rgain

import (
	"math"
	"testing"
)

func sineAt(f, rate, amp float64, i int64) float64 {
	return math.Round(32767.0 * amp * math.Sin(2.0*math.Pi*f*float64(i)/rate))
}

func dualAt(f, rate, amp float64, i int64) float64 {
	t := float64(i) / rate
	return math.Round(32767.0 * amp * (math.Sin(2.0*math.Pi*440.0*t) + 0.5*math.Sin(2.0*math.Pi*3000.0*t)) / 1.5)
}

type goldenCase struct {
	name     string
	rate     int
	channels int
	f        float64
	amp      float64
	seconds  float64
	fn       func(float64, float64, float64, int64) float64
	golden   float64
}

var goldenCases = []goldenCase{
	{"case1_44100_stereo_fullscale", 44100, 2, 997.0, 1.0, 5.0, sineAt, -14.18},
	{"case2_44100_stereo_minus20", 44100, 2, 997.0, 0.1, 5.0, sineAt, 5.82},
	{"case3_48000_mono_minus10", 48000, 1, 440.0, 0.3162277627, 3.0, sineAt, -4.69},
	{"case4_32000_stereo_dual", 32000, 2, 0.0, 0.6, 4.0, dualAt, -9.88},
	{"case5_22050_mono_fullscale", 22050, 1, 997.0, 0.9, 2.0, sineAt, -12.87},
}

func runGolden(t *testing.T, tc goldenCase, feed func(a *Analyzer, l, r []float64)) {
	t.Helper()
	a, err := NewAnalyzer(tc.rate)
	if err != nil {
		t.Fatal(err)
	}
	total := int(float64(tc.rate) * tc.seconds)
	chunk := 1152
	l := make([]float64, chunk)
	r := make([]float64, chunk)
	var i int64
	for n := 0; n < total; {
		m := total - n
		if m > chunk {
			m = chunk
		}
		for k := 0; k < m; k++ {
			l[k] = tc.fn(tc.f, float64(tc.rate), tc.amp, i)
			r[k] = l[k]
			i++
		}
		feed(a, l[:m], r[:m])
		n += m
	}
	got := a.Gain()
	if math.Abs(got-tc.golden) > 0.05 {
		t.Errorf("%s: got %.4f dB, want %.4f dB", tc.name, got, tc.golden)
	}
}

func TestGoldenAgainstCReferenceInterleaved(t *testing.T) {
	for _, tc := range goldenCases {
		runGolden(t, tc, func(a *Analyzer, l, r []float64) {
			inter := make([]float64, 0, len(l)*2)
			for i := range l {
				inter = append(inter, l[i], r[i])
			}
			a.Add(inter)
		})
	}
}

func TestGoldenAgainstCReferencePlanar(t *testing.T) {
	for _, tc := range goldenCases {
		runGolden(t, tc, func(a *Analyzer, l, r []float64) {
			a.AddChannels(l, r)
		})
	}
}

func TestGoldenAgainstCReferenceMonoDuplicate(t *testing.T) {
	for _, tc := range goldenCases {
		if tc.channels != 1 {
			continue
		}
		runGolden(t, tc, func(a *Analyzer, l, r []float64) {
			a.AddChannels(l)
		})
	}
}

func TestNotEnoughSamples(t *testing.T) {
	a, err := NewAnalyzer(44100)
	if err != nil {
		t.Fatal(err)
	}
	if g := a.Gain(); g != GainNotEnoughSamples {
		t.Errorf("empty analyzer: got %v, want %v", g, GainNotEnoughSamples)
	}
}

func TestUnsupportedRate(t *testing.T) {
	if _, err := NewAnalyzer(44000); err == nil {
		t.Error("expected error for unsupported rate")
	}
}

func BenchmarkAdd(b *testing.B) {
	a, _ := NewAnalyzer(44100)
	buf := make([]float64, 2*1152)
	for i := range buf {
		buf[i] = float64(i%2000 - 1000)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		a.Add(buf)
	}
}
