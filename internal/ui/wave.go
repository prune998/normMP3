// Waveform comparison view: when a processed file is clicked, the original
// MP3 and its normalized copy are both decoded into peak/RMS envelopes and
// drawn as two aligned lanes inside a modal, with the measured levels,
// peaks and durations side by side.
package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"

	"github.com/prune998/normMP3/internal/waveform"
)

// waveLaneW/H is the logical size of one waveform lane; the backing image is
// rendered at device resolution for crisp HiDPI output.
const (
	waveLaneW = float32(640)
	waveLaneH = float32(110)
	waveGainH = float32(80)
)

// View modes of the comparison modal: stacked lanes (original above,
// normalized below) or diff view (overlay + gain curve, changes in red).
const (
	waveModeStacked = "stacked"
	waveModeDiff    = "diff"
)

// waveLaneImg is one rendered lane, rebuilt only when its key (theme,
// scale, data identity) changes.
type waveLaneImg struct {
	key string
	img *image.RGBA
}

// waveCmp is the state of the original-vs-normalized comparison modal.
var waveCmp = struct {
	row      *row
	mode     string
	busy     bool
	status   string
	progress float32
	err      string
	orig     *waveform.Envelope
	norm     *waveform.Envelope
	diff     *waveform.Diff

	laneOrig    waveLaneImg
	laneNorm    waveLaneImg
	laneOverlay waveLaneImg
	laneGain    waveLaneImg
	gen         int
}{}

// waveCache caches computed envelope pairs per original/normalized path so
// reopening the comparison of the same file is instant.
var waveCache = struct {
	mu sync.Mutex
	m  map[string][2]*waveform.Envelope
}{m: map[string][2]*waveform.Envelope{}}

// waveOriginalPath returns the untouched source file of a processed row:
// rows live in a Fichiers_normalises folder next to the original, so the
// source is one directory up with the same base name.
func waveOriginalPath(r *row) string {
	if r.src != "" {
		return r.src
	}
	dir := filepath.Dir(r.path)
	if filepath.Base(dir) != outDirName {
		return ""
	}
	return filepath.Join(filepath.Dir(dir), filepath.Base(r.path))
}

func clearWaveCache() {
	waveCache.mu.Lock()
	waveCache.m = map[string][2]*waveform.Envelope{}
	waveCache.mu.Unlock()
}

// openWaveCompare opens the comparison modal for a processed row and starts
// the envelope computation in the background.
func openWaveCompare(r *row) {
	if S.busy {
		toastInfo("Attendez la fin du traitement pour comparer les formes d'onde")
		return
	}
	src := waveOriginalPath(r)
	if src == "" {
		toastInfo("Fichier original introuvable pour la comparaison")
		return
	}

	waveCmp.gen++
	gen := waveCmp.gen
	waveCmp.row = r
	waveCmp.mode = waveModeStacked
	waveCmp.busy = true
	waveCmp.status = ""
	waveCmp.progress = 0
	waveCmp.err = ""
	waveCmp.orig = nil
	waveCmp.norm = nil
	waveCmp.diff = nil
	S.modal = "wave"

	key := src + "\x00" + r.path
	waveCache.mu.Lock()
	cached, ok := waveCache.m[key]
	waveCache.mu.Unlock()
	if ok {
		waveCmp.orig = cached[0]
		waveCmp.norm = cached[1]
		waveCmp.diff = waveform.Compare(cached[0], cached[1])
		waveCmp.busy = false
		return
	}

	go func() {
		fail := func(err error) {
			frameUpdate(func() {
				if waveCmp.gen != gen {
					return
				}
				waveCmp.busy = false
				waveCmp.status = ""
				waveCmp.err = err.Error()
			})
		}

		orig, err := waveform.Compute(src, waveform.DefaultBuckets, func(f float64) {
			frameUpdate(func() {
				if waveCmp.gen != gen {
					return
				}
				waveCmp.status = "Analyse de l'original…"
				waveCmp.progress = float32(f / 2)
			})
		})
		if err != nil {
			fail(err)
			return
		}
		norm, err := waveform.Compute(r.path, waveform.DefaultBuckets, func(f float64) {
			frameUpdate(func() {
				if waveCmp.gen != gen {
					return
				}
				waveCmp.status = "Analyse du fichier normalisé…"
				waveCmp.progress = float32(0.5 + f/2)
			})
		})
		if err != nil {
			fail(err)
			return
		}

		waveCache.mu.Lock()
		waveCache.m[key] = [2]*waveform.Envelope{orig, norm}
		waveCache.mu.Unlock()
		frameUpdate(func() {
			if waveCmp.gen != gen {
				return
			}
			waveCmp.orig = orig
			waveCmp.norm = norm
			waveCmp.diff = waveform.Compare(orig, norm)
			waveCmp.busy = false
			waveCmp.status = ""
			waveCmp.progress = 1
		})
	}()
}

// ---------------------------------------------------------------- the modal

func renderWaveModal() {
	r := waveCmp.row
	style := ModalStyleForScheme(CurrentColorScheme)
	ModalStyled(700, closeModal, style, func() {
		Container(Attrs(Pad(20), Gap(12)), func() {
			Container(Attrs(Row, CrossMid, Gap(10)), func() {
				Container(Attrs(FixSize(34, 34), CrossMid, MainAlign(AlignMiddle), Corners(8),
					Background(210, 60, 45, 0.14)), func() {
					Icon(SymChartBar, FontSize(17), TextColor(210, 60, 42, 1))
				})
				Container(Attrs(Grow(1), Clip), func() {
					Label("Comparaison des formes d'onde", FontSize(15.5), FontWeight(WeightBold))
					if r != nil {
						Label(r.name, FontSize(11.5), TextColorVec(mutedText()))
					}
				})
			})

			switch {
			case waveCmp.busy:
				Container(Attrs(Grow(1), CrossMid, MainAlign(AlignMiddle), FixHeight(280)), func() {
					Container(Attrs(FixWidth(320), Gap(8), CrossMid), func() {
						ProgressBar(waveCmp.progress)
						Label(waveCmp.status, FontSize(12.5), TextColorVec(mutedText()))
						BusyDots(FontSize(14))
					})
				})
			case waveCmp.err != "":
				Container(Attrs(Grow(1), CrossMid, MainAlign(AlignMiddle), FixHeight(280)), func() {
					Icon(SymFail, FontSize(24), TextColor(5, 65, 50, 1))
					Label("Analyse impossible", FontSize(14), FontWeight(WeightBold))
					Container(Attrs(Pad2(2, 0), Gap(4)), func() {
						for _, line := range wrapText(waveCmp.err, 62) {
							Label(line, FontSize(12.5), TextColorVec(mutedText()))
						}
					})
				})
			case r != nil && waveCmp.orig != nil && waveCmp.norm != nil:
				waveModeSwitcher()
				if waveCmp.mode == waveModeDiff && waveCmp.diff != nil {
					waveDiffView()
				} else {
					waveLane("wave-orig", "Original", r.level, waveCmp.orig, &waveCmp.laneOrig, waveColorsOrig())
					waveLane("wave-norm", "Normalisé", r.level2, waveCmp.norm, &waveCmp.laneNorm, waveColorsNorm())
				}
				waveStats(r)
			}

			Container(Attrs(Row, MainAlign(AlignEnd), Pad2(8, 0)), func() {
				NextButtonType(ButtonPrimary)
				if Button(NoIcon, "Fermer") {
					closeModal()
				}
			})
		})
	})
}

// waveLane draws one legend + lane pair; the lane is painted into a
// device-resolution image (cached per key) and shown in a fixed box.
func waveLane(key, title string, level float64, env *waveform.Envelope, slot *waveLaneImg, pal wavePalette) {
	Container(Attrs(Row, CrossMid, Gap(8)), func() {
		Container(Attrs(FixSize(10, 10), Corners(5), BackgroundVec(pal.legend)), func() {})
		Label(title, FontSize(12.5), FontWeight(WeightSemibold))
		Label(fmt.Sprintf("niveau %.2f dB — crête %.1f dBFS — durée %s",
			level, env.PeakDB(), fmtTimeShort(env.Duration())),
			FontSize(11), TextColorVec(mutedText()))
	})

	img := waveLaneRender(slot, env, pal)
	id := UseImage(key, img)
	ImageViewAt(id, Vec2{waveLaneW, waveLaneH})
}

// waveModeSwitcher offers the stacked and diff presentations.
func waveModeSwitcher() {
	Container(Attrs(Row, CrossMid, Gap(10)), func() {
		Label("Vue", FontSize(12), TextColorVec(mutedText()))
		SegmentedControl(&waveCmp.mode, func() {
			SegmentedCell("Empilée", waveModeStacked)
			SegmentedCell("Différence", waveModeDiff)
		})
	})
}

// waveDiffView paints the diff presentation: the two envelopes overlaid in
// one lane (removed level in red) above the per-bucket gain curve.
func waveDiffView() {
	waveDiffLegend()

	Container(Attrs(Corners(6), BorderWidth(1),
		BorderColor(0, 0, 50, 0.12)), func() {
		waveLaneOverlay()
	})

	Container(Attrs(Row, CrossMid, Gap(8)), func() {
		waveDot(waveColorsRemoved().legend, fmt.Sprintf(
			"Δ niveau — rouge : retiré, bleu : ajouté — ±%.1f dB max",
			math.Max(1, waveCmp.diff.MaxGain)))
	})
	Container(Attrs(Corners(6), BorderWidth(1),
		BorderColor(0, 0, 50, 0.12)), func() {
		waveLaneGain()
	})
}

// waveDiffLegend explains the overlay lane colors.
func waveDiffLegend() {
	Container(Attrs(Row, CrossMid, Gap(12), Pad2(2, 0)), func() {
		waveDot(waveColorsShadow().legend, "Original (dessous)")
		waveDot(waveColorsNorm().legend, "Normalisé (conservé)")
		waveDot(waveColorsRemoved().legend, "Retiré")
	})
}

func waveDot(c Vec4, label string) {
	Container(Attrs(Row, CrossMid, Gap(5)), func() {
		Container(Attrs(FixSize(10, 10), Corners(5), BackgroundVec(c)), func() {})
		Label(label, FontSize(11), TextColorVec(mutedText()))
	})
}

// waveScale is the device pixel density, at least 1.
func waveScale() float32 {
	scale := GetHost().WindowScale
	if scale < 1 {
		scale = 1
	}
	return scale
}

// waveLaneCached returns slot's image, rebuilding it when the key changes.
func waveLaneCached(slot *waveLaneImg, key string, render func() *image.RGBA) *image.RGBA {
	if slot.img != nil && slot.key == key {
		return slot.img
	}
	slot.key = key
	slot.img = render()
	return slot.img
}

// waveLaneRender renders the stacked lane pixels, reusing slot when the key
// (theme, scale, data identity) is unchanged.
func waveLaneRender(slot *waveLaneImg, env *waveform.Envelope, pal wavePalette) *image.RGBA {
	scale := waveScale()
	key := fmt.Sprintf("lane|%v|%v|%s", S.dark, scale, waveIdent(env))
	return waveLaneCached(slot, key, func() *image.RGBA {
		return renderWaveLane(env, int(waveLaneW*scale), int(waveLaneH*scale), pal)
	})
}

// waveLaneOverlay shows the original silhouette with the normalized
// waveform on top, modified buckets in red.
func waveLaneOverlay() {
	scale := waveScale()
	key := fmt.Sprintf("overlay|%v|%v|%s|%s|%d@%.3f", S.dark, scale,
		waveIdent(waveCmp.orig), waveIdent(waveCmp.norm),
		waveCmp.diff.ChangedCount, waveCmp.diff.MaxGain)
	img := waveLaneCached(&waveCmp.laneOverlay, key, func() *image.RGBA {
		return renderWaveOverlayLane(waveCmp.orig, waveCmp.norm, waveCmp.diff,
			int(waveLaneW*scale), int(waveLaneH*scale))
	})
	id := UseImage("wave-overlay", img)
	ImageViewAt(id, Vec2{waveLaneW, waveLaneH})
}

// waveLaneGain shows the per-bucket level change (0 dB at the center line),
// red where the bucket was modified.
func waveLaneGain() {
	scale := waveScale()
	key := fmt.Sprintf("gain|%v|%v|%s|%s|%d@%.3f", S.dark, scale,
		waveIdent(waveCmp.orig), waveIdent(waveCmp.norm),
		waveCmp.diff.ChangedCount, waveCmp.diff.MaxGain)
	img := waveLaneCached(&waveCmp.laneGain, key, func() *image.RGBA {
		return renderWaveGainLane(waveCmp.diff, int(waveLaneW*scale), int(waveGainH*scale))
	})
	id := UseImage("wave-gain", img)
	ImageViewAt(id, Vec2{waveLaneW, waveGainH})
}

// waveStats shows the before/after measurements under the two lanes.
func waveStats(r *row) {
	Container(Attrs(Gap(4), Pad2(6, 0)), func() {
		waveStatRow("Action", fmt.Sprintf("%s (%+.2f dB)", r.action, r.corr))
		waveStatRow("Niveau", fmt.Sprintf("%.2f dB → %.2f dB", r.level, r.level2))
		waveStatRow("Crête", fmt.Sprintf("%.1f → %.1f dBFS", waveCmp.orig.PeakDB(), waveCmp.norm.PeakDB()))
		waveStatRow("Durée", fmt.Sprintf("%s → %s",
			fmtTimeShort(waveCmp.orig.Duration()), fmtTimeShort(waveCmp.norm.Duration())))
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			Container(Attrs(FixWidth(56), CrossMid), func() {
				Label("Écrêtage", FontSize(11.5), TextColorVec(mutedText()))
			})
			if r.clip2 {
				pill("Oui — fichier re-normalisé", "clip")
			} else {
				pill("Non", "ok")
			}
		})
	})
}

func waveStatRow(label, value string) {
	Container(Attrs(Row, CrossMid, Gap(8)), func() {
		Container(Attrs(FixWidth(56), CrossMid), func() {
			Label(label, FontSize(11.5), TextColorVec(mutedText()))
		})
		Label(value, FontSize(12.5))
	})
}

func fmtTimeShort(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// wrapText breaks msg into display lines of at most n characters,
// respecting existing newlines (error strings from the pipeline can be long).
func wrapText(msg string, n int) []string {
	var out []string
	for _, para := range strings.Split(msg, "\n") {
		for len(para) > n {
			cut := strings.LastIndex(para[:n+1], " ")
			if cut <= 0 {
				cut = n
			}
			out = append(out, para[:cut])
			para = strings.TrimLeft(para[cut:], " ")
		}
		out = append(out, para)
	}
	return out
}

// ------------------------------------------------------------ lane painting

// wavePalette holds the lane paint: the legend color (shirei HSL Vec4) plus
// the pixel colors for the peak bars, the RMS core and the guide lines.
type wavePalette struct {
	legend Vec4
	peak   color.RGBA
	rms    color.RGBA
	guide  color.RGBA
	center color.RGBA
}

func waveColorsOrig() wavePalette {
	return wavePalette{
		legend: Vec4{215, 12, 55, 1},
		peak:   hslRGBA(215, 12, 62, 0.30),
		rms:    hslRGBA(215, 12, 55, 0.85),
		guide:  hslRGBA(0, 0, 50, 0.10),
		center: hslRGBA(0, 0, 50, 0.22),
	}
}

func waveColorsNorm() wavePalette {
	return wavePalette{
		legend: Vec4{210, 60, 45, 1},
		peak:   hslRGBA(210, 60, 55, 0.32),
		rms:    hslRGBA(210, 60, 45, 0.90),
		guide:  hslRGBA(0, 0, 50, 0.10),
		center: hslRGBA(0, 0, 50, 0.22),
	}
}

// waveColorsRemoved paints the level that normalization took out (diff
// view) in the app's red.
func waveColorsRemoved() wavePalette {
	return wavePalette{
		legend: Vec4{5, 65, 50, 1},
		peak:   hslRGBA(5, 65, 55, 0.40),
		rms:    hslRGBA(5, 65, 48, 0.88),
		guide:  hslRGBA(0, 0, 50, 0.10),
		center: hslRGBA(0, 0, 50, 0.22),
	}
}

// waveColorsShadow paints the original silhouette that sits behind the
// normalized waveform in the diff overlay: same hue as waveColorsOrig but
// more translucent so the foreground reads through.
func waveColorsShadow() wavePalette {
	return wavePalette{
		legend: Vec4{215, 12, 55, 1},
		peak:   hslRGBA(215, 12, 62, 0.16),
		rms:    hslRGBA(215, 12, 55, 0.38),
		guide:  hslRGBA(0, 0, 50, 0.10),
		center: hslRGBA(0, 0, 50, 0.22),
	}
}

// waveColorsMuted paints the unchanged buckets of the gain curve, visibly
// quieter than the red of the modified ones.
func waveColorsMuted() wavePalette {
	return wavePalette{
		legend: Vec4{215, 12, 55, 1},
		peak:   hslRGBA(215, 12, 60, 0.14),
		rms:    hslRGBA(215, 12, 60, 0.30),
		guide:  hslRGBA(0, 0, 50, 0.10),
		center: hslRGBA(0, 0, 50, 0.22),
	}
}

// waveIdent summarizes the envelope data so a rebuilt image key follows it.
func waveIdent(e *waveform.Envelope) string {
	return fmt.Sprintf("%d@%.4f", len(e.Peaks), e.MaxPeak)
}

// renderWaveLane paints the peak envelope into a transparent RGBA image at
// device resolution: faint guide lines, semi-transparent peak bars and a
// solid RMS core per bucket.
func renderWaveLane(env *waveform.Envelope, w, h int, pal wavePalette) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}
	drawWaveGuides(img, pal)

	nb := len(env.Peaks)
	if nb == 0 {
		return img
	}
	mid := h / 2
	amp := float64(mid) - 2
	for i := 0; i < nb; i++ {
		x0, x1 := waveBarSpan(i, nb, w)
		ph := int(clamp01(env.Peaks[i]) * amp)
		rh := int(clamp01(env.RMS[i]) * amp)
		if ph > 0 {
			fillRows(img, x0, x1, mid-ph, mid+ph, pal.peak)
		}
		if rh > 0 {
			fillRows(img, x0, x1, mid-rh, mid+rh, pal.rms)
		}
	}
	return img
}

// renderWaveOverlayLane paints the diff overlay: the original envelope as a
// translucent silhouette, the normalized waveform in front in blue, and —
// where normalization took level out — the span that disappeared between
// the two heights filled in red.
func renderWaveOverlayLane(orig, norm *waveform.Envelope, diff *waveform.Diff, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}
	drawWaveGuides(img, waveColorsNorm())

	nb := len(norm.Peaks)
	no := len(orig.Peaks)
	if nb == 0 || no == 0 {
		return img
	}
	shadow, plain, removed := waveColorsShadow(), waveColorsNorm(), waveColorsRemoved()
	mid := h / 2
	amp := float64(mid) - 2
	for i := 0; i < nb; i++ {
		x0, x1 := waveBarSpan(i, nb, w)

		// original silhouette (behind), mapped proportionally in time
		j := i * no / nb
		op := int(clamp01(orig.Peaks[j]) * amp)
		or := int(clamp01(orig.RMS[j]) * amp)
		if op > 0 {
			fillRows(img, x0, x1, mid-op, mid+op, shadow.peak)
		}
		if or > 0 {
			fillRows(img, x0, x1, mid-or, mid+or, shadow.rms)
		}

		np := int(clamp01(norm.Peaks[i]) * amp)
		nr := int(clamp01(norm.RMS[i]) * amp)

		// the removed span: the gap the normalization carved between the
		// original height and what remains, at peak and RMS level
		if diff != nil && diff.Removed[i] {
			if op > np {
				fillRows(img, x0, x1, mid-op, mid-np, removed.peak)
				fillRows(img, x0, x1, mid+np, mid+op, removed.peak)
			}
			if or > nr {
				fillRows(img, x0, x1, mid-or, mid-nr, removed.rms)
				fillRows(img, x0, x1, mid+nr, mid+or, removed.rms)
			}
		}

		// normalized waveform (front)
		if np > 0 {
			fillRows(img, x0, x1, mid-np, mid+np, plain.peak)
		}
		if nr > 0 {
			fillRows(img, x0, x1, mid-nr, mid+nr, plain.rms)
		}
	}
	return img
}

// renderWaveGainLane paints the per-bucket level change as bars around the
// 0 dB center line (boost up, attenuation down), scaled to ±MaxGain (at
// least ±1 dB); removed level is red, added level is blue, unchanged
// buckets stay muted gray.
func renderWaveGainLane(diff *waveform.Diff, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}
	drawWaveGuides(img, waveColorsNorm())
	if diff == nil || len(diff.GainDB) == 0 {
		return img
	}

	scale := math.Max(diff.MaxGain, 1)
	muted, plain, removed := waveColorsMuted(), waveColorsNorm(), waveColorsRemoved()
	nb := len(diff.GainDB)
	mid := h / 2
	amp := float64(mid) - 2
	for i := 0; i < nb; i++ {
		g := float64(diff.GainDB[i])
		if g == 0 {
			continue
		}
		x0, x1 := waveBarSpan(i, nb, w)
		py := int(math.Max(-1, math.Min(1, g/scale)) * amp)
		pal := muted
		if diff.Changed[i] {
			pal = plain
		}
		if diff.Removed[i] {
			pal = removed
		}
		if py > 0 {
			fillRows(img, x0, x1, mid-py, mid+1, pal.rms)
		} else {
			fillRows(img, x0, x1, mid, mid-py+1, pal.rms)
		}
	}
	return img
}

// waveBarSpan returns the column span [x0,x1) of bucket i across width w.
func waveBarSpan(i, nb, w int) (int, int) {
	x0 := int(float64(i) * float64(w) / float64(nb))
	x1 := int(float64(i+1) * float64(w) / float64(nb))
	if x1 <= x0 {
		x1 = x0 + 1
	}
	return x0, x1
}

// drawWaveGuides paints the horizontal ±25/50/75 % guides and the
// emphasized center line.
func drawWaveGuides(img *image.RGBA, pal wavePalette) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	mid := h / 2
	for _, f := range []float64{0.75, 0.5, 0.25} {
		y := int(float64(mid) * f)
		fillRows(img, 0, w, mid-y-1, mid-y, pal.guide)
		fillRows(img, 0, w, mid+y, mid+y+1, pal.guide)
	}
	fillRows(img, 0, w, mid-1, mid+1, pal.center)
}

func clamp01(v float32) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return float64(v)
}

// fillRows blends color into the column span [x0,x1) and row span [y0,y1)
// with source-over compositing.
func fillRows(img *image.RGBA, x0, x1, y0, y1 int, c color.RGBA) {
	if x0 < 0 {
		x0 = 0
	}
	if x1 > img.Rect.Dx() {
		x1 = img.Rect.Dx()
	}
	if y0 < 0 {
		y0 = 0
	}
	if y1 > img.Rect.Dy() {
		y1 = img.Rect.Dy()
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			blendPixel(img, x, y, c)
		}
	}
}

// blendPixel composites a non-premultiplied RGBA source over the image.
func blendPixel(img *image.RGBA, x, y int, c color.RGBA) {
	i := img.PixOffset(x, y)
	sa := float64(c.A) / 255
	if sa <= 0 {
		return
	}
	da := float64(img.Pix[i+3]) / 255
	outA := sa + da*(1-sa)
	for k, ch := range [3]uint8{c.R, c.G, c.B} {
		dst := float64(img.Pix[i+k])
		val := float64(ch)*sa + dst*da*(1-sa)
		img.Pix[i+k] = uint8(math.Min(255, val+0.5))
	}
	img.Pix[i+3] = uint8(outA*255 + 0.5)
}

// hslRGBA converts hue (0-360), saturation and lightness (0-100) plus alpha
// (0-1) to a non-premultiplied RGBA color.
func hslRGBA(h, s, l, a float64) color.RGBA {
	s /= 100
	l /= 100
	c := (1 - math.Abs(2*l-1)) * s
	hp := math.Mod(h, 360) / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	to8 := func(v float64) uint8 {
		return uint8(math.Min(255, math.Round((v+m)*255)))
	}
	return color.RGBA{R: to8(r), G: to8(g), B: to8(b), A: uint8(math.Round(a * 255))}
}
