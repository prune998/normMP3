// Package ui implements the shirei-based GUI, mirroring the behavior of
// source/test_mp3gain_tk-Copie.py (menus, analysis table, processing
// table, progress, target-gain modal, multi-select file browser).
package ui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
	app "go.hasen.dev/shirei/app"
	. "go.hasen.dev/shirei/widgets"

	"github.com/prune998/normMP3/internal/audio"
)

type row struct {
	path   string
	name   string
	size   int64
	target float64
	level  float64
	corr   float64
	clip   bool
	action string
	level2 float64
	clip2  bool
	fixed  bool
	done   bool
}

var S = struct {
	version  string
	cwd      string
	outDir   string
	target   float64
	rows     []*row
	busy     bool
	busyKnd  string
	status   string
	progress float32
	modal    string
	mKind    string
	mTitle   string
	mMsg     string
	gainBuf  float32
	dark     bool
}{
	target: 89.0,
	modal:  "",
}

var brw = struct {
	cwd    string
	sel    map[string]bool
	anchor string
}{
	sel: map[string]bool{},
}

func Run(version string) {
	S.version = version
	S.cwd = workDir()
	S.outDir = filepath.Join(S.cwd, "Fichiers_normalises")
	loadMemo()
	loadConf()
	SetDarkMode(S.dark)
	brw.cwd = S.cwd

	app.SetupWindow("NormMP3 — Normalisation de fichiers MP3", 1280, 720)
	app.Run(RootView)
}

func RootView() {
	Container(Attrs(Viewport, UseSurface(SurfaceCanvas)), func() {
		topBar()
		Container(Attrs(Row, Grow(1), Expand, Gap(10), Pad2(10, 12), Clip), func() {
			tablePanel()
			selectionPanel()
		})
		bottomBar()
	})
	renderModals()
}

// ------------------------------------------------------------ status helpers

func hasResults() bool {
	for _, r := range S.rows {
		if r.target != 0 {
			return true
		}
	}
	return false
}

func rowState(r *row) string {
	switch {
	case r.action == "Aucune correction":
		return "ok"
	case r.action == "Ampl.simple" && r.fixed:
		return "fixed"
	case r.action == "Ampl.simple":
		return "boost"
	case r.action == "Normalisation":
		return "norm"
	case r.target != 0:
		return "ready"
	default:
		return "pending"
	}
}

// statusColors returns pill background/text pairs that read well on both
// light and dark surfaces: translucent background, saturated text.
var statusColors = map[string][2]Vec4{
	"ok":      {Vec4{130, 55, 40, 0.18}, Vec4{130, 55, 30, 1}},
	"boost":   {Vec4{210, 60, 45, 0.16}, Vec4{210, 60, 42, 1}},
	"norm":    {Vec4{5, 65, 50, 0.16}, Vec4{5, 65, 52, 1}},
	"fixed":   {Vec4{130, 55, 40, 0.18}, Vec4{130, 55, 30, 1}},
	"ready":   {Vec4{40, 70, 50, 0.14}, Vec4{40, 75, 38, 1}},
	"pending": {Vec4{0, 0, 50, 0.12}, Vec4{0, 0, 45, 1}},
	"clip":    {Vec4{5, 70, 50, 0.22}, Vec4{5, 70, 48, 1}},
}

func pill(label, kind string) {
	c := statusColors[kind]
	Container(Attrs(Row, CrossMid, MainAlign(AlignMiddle), Pad2(3, 10), Corners(99), BackgroundVec(c[0])), func() {
		Label(label, FontSize(11.5), FontWeight(WeightBold), TextColorVec(c[1]))
	})
}

// ------------------------------------------------------------------- top bar

func topBar() {
	Container(Attrs(Row, CrossMid, Pad2(10, 14), Gap(10), Expand, UseSurface(SurfaceToolbar)), func() {
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			Container(Attrs(FixSize(34, 34), CrossMid, MainAlign(AlignMiddle), Corners(8), Background(210, 60, 45, 1)), func() {
				Icon(SymVolHigh, TextColor(0, 0, 100, 1), FontSize(18))
			})
			Container(Attrs(Gap(1)), func() {
				Label("NormMP3", FontSize(15), FontWeight(WeightBold))
				Label("Normalisation de fichiers MP3", FontSize(11), TextColorVec(mutedToolbarText()))
			})
		})

		Filler(1)

		NextButtonAttrs(ButtonAttrs{Disabled: S.busy})
		if Button(SymFolder, "Choisir les fichiers") {
			openBrowser()
		}
		NextButtonAttrs(ButtonAttrs{Disabled: S.busy || !hasResults()})
		if Button(SymChartBar, "Analyse") {
			startAnalyse()
		}
		NextButtonAttrs(ButtonAttrs{Type: ButtonPrimary, Disabled: S.busy || !hasResults()})
		if Button(SymITick, "Traitement") {
			startTraitement()
		}

		Container(Attrs(Row, CrossMid, Gap(6), Pad2(5, 10), Corners(99),
			BackgroundVec(CurrentColorScheme.Surfaces.Canvas.Background), BorderWidth(1),
			BorderColorVec(CurrentColorScheme.Surfaces.Canvas.Border)), func() {
			if IsClicked() {
				openGainModal()
			}
			if IsHovered() {
				ModAttrs(Background(210, 40, 50, 0.08))
			}
			Icon(SymCog, FontSize(13), TextColorVec(CurrentColorScheme.Surfaces.Canvas.Text))
			Label("Cible", FontSize(12), TextColorVec(CurrentColorScheme.Surfaces.Canvas.Text))
			Label(fmt.Sprintf("%.1f dB", S.target), FontSize(12.5), FontWeight(WeightBold), TextColorVec(CurrentColorScheme.Surfaces.Canvas.Text))
		})

		NextButtonAttrs(ButtonAttrs{})
		if S.dark {
			if CtrlButton(SymShow, "Sombre", true) {
				setDark(false)
			}
		} else {
			if CtrlButton(SymHide, "Clair", true) {
				setDark(true)
			}
		}
	})
}

func setDark(dark bool) {
	S.dark = dark
	SetDarkMode(dark)
	saveConf()
}

// ------------------------------------------------------------- main panels

func cardHeader(title, sub string) ContainerId {
	return Container(Attrs(Row, CrossMid, Pad2(10, 12), Gap(8),
		UseSurface(SurfacePanel), Background(0, 0, 50, 0.04)), func() {
		Label(title, FontSize(13), FontWeight(WeightBold))
		if sub != "" {
			Label(sub, FontSize(11.5), TextColorVec(mutedText()))
		}
	})
}

func mutedText() Vec4 {
	c := CurrentColorScheme.Surfaces.Panel.Text
	return Vec4{c[0], c[1], c[2], c[3] * 0.62}
}

func mutedToolbarText() Vec4 {
	c := CurrentColorScheme.Surfaces.Toolbar.Text
	return Vec4{c[0], c[1], c[2], c[3] * 0.62}
}

func tablePanel() {
	Container(Attrs(Grow(1), Expand, Clip, Corners(10), UseSurface(SurfacePanel), BorderWidth(1)), func() {
		sub := ""
		switch {
		case S.busy && S.busyKnd == "analyse":
			sub = "analyse en cours…"
		case S.busy:
			sub = "traitement en cours…"
		case len(S.rows) > 0:
			sub = fmt.Sprintf("%d fichier(s)", len(S.rows))
		}
		cardHeader("Analyse et traitement", sub)

		if len(S.rows) == 0 {
			emptyState()
			return
		}

		Container(Attrs(Grow(1), Expand, Clip), func() {
			ScrollOnInput()
			tableHeader()
			for i, r := range S.rows {
				r := r
				tableRow(i, r)
			}
			ScrollBars()
		})
	})
}

func tableHeader() {
	Container(Attrs(Row, FixHeight(28), CrossMid, Pad2(0, 12), Background(0, 0, 50, 0.05)), func() {
		hcol("Fichier", 240, AlignStart)
		hcol("Niveau", 64, AlignEnd)
		hcol("Cible", 64, AlignEnd)
		hcol("Corr.", 64, AlignEnd)
		hcol("Action", 150, AlignMiddle)
		hcol("Niveau final", 90, AlignEnd)
		hcol("Ecr.", 44, AlignMiddle)
	})
}

func hcol(label string, w float32, al Alignment) {
	Container(Attrs(FixWidth(w), CrossMid, MainAlign(al)), func() {
		Label(label, FontSize(11), FontWeight(WeightBold), TextColorVec(mutedText()))
	})
}

func tableRow(i int, r *row) {
	ContainerWithKey(r.path, Attrs(Row, FixHeight(30), CrossMid, Pad2(0, 12)), func() {
		if i%2 == 1 {
			ModAttrs(Background(0, 0, 50, 0.035))
		}
		if IsHovered() {
			ModAttrs(Background(210, 50, 50, 0.07))
		}
		cellText(r.name, 240, AlignStart, FontSize(12.5))
		cellText(fmt.Sprintf("%.2f", r.level), 64, AlignEnd, FontSize(12.5))
		cellText(fmt.Sprintf("%.2f", r.target), 64, AlignEnd, FontSize(12.5))
		cellText(fmt.Sprintf("%+.2f", r.corr), 64, AlignEnd, FontSize(12.5), corrColor(r))
		Container(Attrs(FixWidth(150), CrossMid, MainAlign(AlignMiddle)), func() {
			switch rowState(r) {
			case "pending", "ready":
				pill("En attente", "pending")
			default:
				pill(r.action, rowState(r))
			}
		})
		if r.done {
			cellText(fmt.Sprintf("%.2f", r.level2), 90, AlignEnd, FontSize(12.5))
			Container(Attrs(FixWidth(44), CrossMid, MainAlign(AlignMiddle)), func() {
				if r.clip2 {
					pill("Y", "clip")
				} else {
					pill("—", "pending")
				}
			})
		} else {
			cellText("", 90, AlignEnd)
			Container(Attrs(FixWidth(44)), func() {})
		}
	})
}

func corrColor(r *row) TextStyleFn {
	if r.target == 0 {
		return TextColorVec(mutedText())
	}
	if r.corr > 0.05 {
		return TextColor(130, 55, 32, 1)
	}
	if r.corr < -0.05 {
		return TextColor(5, 65, 50, 1)
	}
	return TextColorVec(mutedText())
}

func cellText(text string, w float32, al Alignment, mods ...TextStyleFn) {
	Container(Attrs(FixWidth(w), CrossMid, MainAlign(al)), func() {
		Label(text, mods...)
	})
}

func emptyState() {
	Container(Attrs(Grow(1), Expand, CrossMid, MainAlign(AlignMiddle), Gap(10)), func() {
		Container(Attrs(FixSize(64, 64), CrossMid, MainAlign(AlignMiddle), Corners(16),
			Background(210, 55, 45, 0.12)), func() {
			Icon(SymAudio, FontSize(30), TextColor(210, 55, 42, 1))
		})
		Label("Aucun fichier sélectionné", FontSize(15), FontWeight(WeightBold))
		Label("Choisissez des fichiers MP3 : ils seront copiés dans", FontSize(12.5), TextColorVec(mutedText()))
		Label("Fichiers_normalises et normalisés au gain cible.", FontSize(12.5), TextColorVec(mutedText()))
		Container(Attrs(FixHeight(6)), func() {})
		NextButtonType(ButtonPrimary)
		if Button(SymFolder, "Choisir les fichiers") {
			openBrowser()
		}
	})
}

func selectionPanel() {
	Container(Attrs(FixWidth(300), Expand, Clip, Corners(10), UseSurface(SurfacePanel), BorderWidth(1)), func() {
		done := 0
		for _, r := range S.rows {
			if r.done {
				done++
			}
		}
		sub := ""
		if len(S.rows) > 0 {
			sub = fmt.Sprintf("%d / %d traité(s)", done, len(S.rows))
		}
		cardHeader("Sélection", sub)

		if len(S.rows) == 0 {
			Container(Attrs(Grow(1), Expand, CrossMid, MainAlign(AlignMiddle)), func() {
				Label("La liste des fichiers", FontSize(12), TextColorVec(mutedText()))
				Label("s'affichera ici.", FontSize(12), TextColorVec(mutedText()))
			})
			return
		}

		Container(Attrs(Grow(1), Expand, Clip), func() {
			ScrollOnInput()
			for _, r := range S.rows {
				r := r
				ContainerWithKey("sel-"+r.path, Attrs(Row, CrossMid, FixHeight(30), Gap(8), Pad2(0, 12)), func() {
					if IsHovered() {
						ModAttrs(Background(210, 50, 50, 0.07))
					}
					if IsDoubleClicked() {
						openWithDefault(r.path)
					}
					Icon(SymAudio, FontSize(13), TextColorVec(mutedText()))
					Container(Attrs(Grow(1), Clip), func() {
						Label(r.name, FontSize(12.5))
					})
					switch rowState(r) {
					case "ok":
						Icon(SymPass, FontSize(14), TextColor(130, 55, 34, 1))
					case "fixed":
						Icon(SymPass, FontSize(14), TextColor(130, 55, 34, 1))
					case "boost", "norm":
						Icon(SymWarn, FontSize(14), TextColor(40, 75, 38, 1))
					}
				})
			}
			ScrollBars()
		})
		Container(Attrs(Row, CrossMid, Pad2(8, 12), Gap(6), Background(0, 0, 50, 0.04)), func() {
			Label("Double clic : écouter un fichier", FontSize(11), TextColorVec(mutedText()))
		})
	})
}

// --------------------------------------------------------------- bottom bar

func bottomBar() {
	Container(Attrs(Row, CrossMid, FixHeight(44), Pad2(0, 14), Gap(12), Expand, UseSurface(SurfaceToolbar)), func() {
		switch {
		case S.busy:
			Container(Attrs(FixWidth(240), CrossMid), func() {
				ProgressBar(S.progress)
			})
			Label(fmt.Sprintf("%d%%", int(S.progress*100+0.5)), FontSize(12), FontWeight(WeightBold))
			BusyDots(FontSize(14))
			Container(Attrs(Grow(1), Clip), func() {
				Label(S.status, FontSize(12.5))
			})
		case len(S.rows) > 0:
			n := len(S.rows)
			done := 0
			for _, r := range S.rows {
				if r.done {
					done++
				}
			}
			if done == n {
				Label(fmt.Sprintf("Terminé — %d fichier(s) normalisé(s) dans %s", n, filepath.Base(S.outDir)),
					FontSize(12.5), TextColor(130, 55, 32, 1))
			} else {
				Label(fmt.Sprintf("%d fichier(s) prêt(s) — gain cible %.1f dB", n, S.target),
					FontSize(12.5))
			}
		default:
			Label("Prêt", FontSize(12.5))
		}
		Filler(1)
		Label("v"+S.version, FontSize(11), TextColorVec(mutedText()))
	})
}

// ------------------------------------------------------------------- modals

func renderModals() {
	switch S.modal {
	case "msg":
		style := ModalStyleForScheme(CurrentColorScheme)
		ModalStyled(520, closeModal, style, func() {
			kindIcon := SymInfo
			kindColor := Vec4{210, 60, 45, 1}
			switch S.mKind {
			case "error":
				kindIcon = SymFail
				kindColor = Vec4{5, 65, 50, 1}
			case "warn":
				kindIcon = SymWarn
				kindColor = Vec4{40, 75, 38, 1}
			}
			Container(Attrs(Pad(20), Gap(12)), func() {
				Container(Attrs(Row, CrossMid, Gap(10)), func() {
					Container(Attrs(FixSize(34, 34), CrossMid, MainAlign(AlignMiddle), Corners(8),
						BackgroundVec(Vec4{kindColor[0], kindColor[1], kindColor[2], 0.14})), func() {
						Icon(kindIcon, FontSize(17), TextColorVec(kindColor))
					})
					Label(S.mTitle, FontSize(15.5), FontWeight(WeightBold))
				})
				Container(Attrs(Pad2(2, 0), Gap(4)), func() {
					for _, line := range strings.Split(S.mMsg, "\n") {
						Label(line, FontSize(13))
					}
				})
				Container(Attrs(Row, MainAlign(AlignEnd), Pad2(8, 0)), func() {
					NextButtonType(ButtonPrimary)
					if Button(NoIcon, "OK") {
						closeModal()
					}
				})
			})
		})
	case "gain":
		style := ModalStyleForScheme(CurrentColorScheme)
		ModalStyled(440, closeModal, style, func() {
			Container(Attrs(Pad(20), Gap(14)), func() {
				Label("Choisissez le gain cible", FontSize(15.5), FontWeight(WeightBold))
				Container(Attrs(Row, CrossMid, MainAlign(AlignMiddle), Gap(10)), func() {
					Label(fmt.Sprintf("%.1f", S.gainBuf), FontSize(34), FontWeight(WeightBold))
					Label("dB", FontSize(13), TextColorVec(mutedText()))
				})
				Slider(&S.gainBuf, SliderAttrs{Min: 85, Max: 93, Step: 0.5, Width: 360})
				Container(Attrs(Row, MainAlign(AlignMiddle)), func() {
					Container(Attrs(FixWidth(360), Row, MainAlign(AlignEnd), Gap(6)), func() {
						Label("85", FontSize(10.5), TextColorVec(mutedText()))
						Filler(1)
						Label("93", FontSize(10.5), TextColorVec(mutedText()))
					})
				})
				Container(Attrs(Row, Gap(10), MainAlign(AlignEnd), Pad2(8, 0)), func() {
					if Button(NoIcon, "Sortie sans valider") {
						closeModal()
					}
					NextButtonType(ButtonPrimary)
					if Button(SymITick, "Valider") {
						validerGain(float64(S.gainBuf))
					}
				})
			})
		})
	case "browser":
		renderBrowser()
	case "quit":
		style := ModalStyleForScheme(CurrentColorScheme)
		ModalStyled(560, closeModal, style, func() {
			Container(Attrs(Pad(20), Gap(12)), func() {
				Container(Attrs(Row, CrossMid, Gap(10)), func() {
					Container(Attrs(FixSize(34, 34), CrossMid, MainAlign(AlignMiddle), Corners(8),
						Background(5, 65, 50, 0.14)), func() {
						Icon(SymWarn, FontSize(17), TextColor(5, 65, 50, 1))
					})
					Label("ARRÊT PRÉMATURÉ ...", FontSize(15.5), FontWeight(WeightBold))
				})
				Label("Le traitement est en cours d'exécution.", FontSize(13))
				Label("Certains fichiers pourraient être corrompus si vous arrêtez maintenant.", FontSize(13))
				Label("Voulez-vous vraiment arrêter ?", FontSize(13))
				Container(Attrs(Row, Gap(10), MainAlign(AlignEnd), Pad2(8, 0)), func() {
					if Button(NoIcon, "Non, continuer le traitement") {
						closeModal()
					}
					NextButtonAttrs(ButtonAttrs{Type: ButtonDestructive})
					if Button(NoIcon, "Oui, arrêter") {
						app.Quit()
					}
				})
			})
		})
	}
}

func renderBrowser() {
	style := ModalStyleForScheme(CurrentColorScheme)
	ModalStyled(660, closeModal, style, func() {
		Container(Attrs(Pad(16), Gap(10)), func() {
			Label("SELECTIONNER UN OU PLUSIEURS FICHIERS MP3", FontSize(14.5), FontWeight(WeightBold))
			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				NextButtonAttrs(ButtonAttrs{})
				if CtrlButton(SymArrowLeft, "Parent", true) {
					brw.cwd = filepath.Dir(brw.cwd)
				}
				NextButtonAttrs(ButtonAttrs{})
				if CtrlButton(SymHome, "Home", true) {
					brw.cwd = homeDir()
				}
				Label(truncateMiddle(brw.cwd, 52), FontSize(12), TextColorVec(mutedText()))
			})
			Label("Clic : cocher — Maj+clic : plage — Cmd/Ctrl+clic : basculer",
				FontSize(11.5), TextColorVec(mutedText()))
			Container(Attrs(Grow(1), Expand, Clip, FixHeight(360), Corners(8),
				BackgroundVec(CurrentColorScheme.Surfaces.Canvas.Background), BorderWidth(1),
				BorderColorVec(CurrentColorScheme.Surfaces.Canvas.Border)), func() {
				ScrollOnInput()
				for _, e := range browserEntries() {
					e := e
					if e.IsDir() {
						d := e.Name()
						ContainerWithKey("d-"+d, Attrs(Row, CrossMid, FixHeight(26), Gap(8), Pad2(0, 10)), func() {
							if IsHovered() {
								ModAttrs(Background(210, 50, 50, 0.07))
							}
							if IsClicked() {
								brw.cwd = filepath.Join(brw.cwd, d)
							}
							Icon(SymFolder, FontSize(13), TextColor(40, 65, 42, 1))
							Label(d, FontSize(12.5))
						})
						continue
					}
					n := e.Name()
					if !strings.HasSuffix(n, ".mp3") && !strings.HasSuffix(n, ".MP3") {
						continue
					}
					full := filepath.Join(brw.cwd, n)
					ContainerWithKey("f-"+full, Attrs(Row, CrossMid, FixHeight(26), Gap(8), Pad2(0, 10)), func() {
						if IsHovered() {
							ModAttrs(Background(210, 50, 50, 0.07))
						}
						if IsClicked() {
							browserClick(full)
						}
						if brw.sel[full] {
							Icon(SymBoxTick, FontSize(14), TextColor(130, 55, 32, 1))
							Label(n, FontSize(12.5), TextColor(130, 55, 32, 1))
						} else {
							Icon(SymBox, FontSize(14), TextColorVec(mutedText()))
							Label(n, FontSize(12.5))
						}
					})
				}
				ScrollBars()
			})
			Container(Attrs(Row, CrossMid, Gap(10)), func() {
				Label(fmt.Sprintf("%d fichier(s) sélectionné(s)", len(brw.sel)), FontSize(12.5), FontWeight(WeightBold))
				Filler(1)
				if Button(NoIcon, "Annuler") {
					closeModal()
				}
				NextButtonAttrs(ButtonAttrs{Type: ButtonPrimary, Disabled: len(brw.sel) == 0})
				if Button(SymITick, "Choisir") {
					choisirFichiers()
				}
			})
		})
	})
}

// ------------------------------------------------------------------- toasts

func toastInfo(msg string) {
	Toast(SymInfo, "Information", msg)
}

func toastSuccess(msg string) {
	Toast(SymPass, "Terminé", msg)
}

// ------------------------------------------------------------- user actions

func openGainModal() {
	S.gainBuf = float32(S.target)
	S.modal = "gain"
}

func validerGain(v float64) {
	S.target = v
	saveMemo()
	closeModal()
	toastSuccess(fmt.Sprintf("Gain cible fixé à %.1f dB — relance de l'analyse", v))
	startAnalyse()
}

func closeModal() { S.modal = "" }

func showMsg(title, msg string) {
	S.modal = "msg"
	S.mKind = "info"
	S.mTitle = title
	S.mMsg = msg
}

func showError(title, msg string) {
	S.modal = "msg"
	S.mKind = "error"
	S.mTitle = title
	S.mMsg = msg
}

func loadMemo() {
	data, err := os.ReadFile(filepath.Join(S.cwd, "memo.txt"))
	if err == nil {
		var v float64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%f", &v); err == nil && v >= 80 && v <= 100 {
			S.target = v
		}
	}
}

func saveMemo() {
	os.WriteFile(filepath.Join(S.cwd, "memo.txt"), []byte(fmt.Sprintf("%v", S.target)), 0o644)
}

func loadConf() {
	data, err := os.ReadFile(filepath.Join(S.cwd, "normmp3.conf"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if k == "dark" {
			S.dark = v == "true"
		}
	}
}

func saveConf() {
	mode := "false"
	if S.dark {
		mode = "true"
	}
	content := fmt.Sprintf("dark=%s\n", mode)
	os.WriteFile(filepath.Join(S.cwd, "normmp3.conf"), []byte(content), 0o644)
}

func workDir() string {
	cwd, err := os.Getwd()
	if err == nil && cwdWritable(cwd) {
		return cwd
	}
	home, herr := os.UserHomeDir()
	if herr != nil {
		return cwd
	}
	return home
}

func cwdWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".normmp3-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return S.cwd
	}
	return h
}

func listMP3(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && (strings.HasSuffix(n, ".mp3") || strings.HasSuffix(n, ".MP3")) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

// --------------------------------------------------------- file browser

func browserEntries() []os.DirEntry {
	entries, err := os.ReadDir(brw.cwd)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return entries[i].Name() < entries[j].Name()
	})
	var out []os.DirEntry
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e)
		}
	}
	return out
}

// browserFiles returns the MP3 files of the current listing, in display
// order, as absolute paths.
func browserFiles() []string {
	var out []string
	for _, e := range browserEntries() {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".mp3") || strings.HasSuffix(n, ".MP3") {
			out = append(out, filepath.Join(brw.cwd, n))
		}
	}
	return out
}

// selectRange marks every file between the anchor and clicked (both
// inclusive, in display order) as selected and returns how many files the
// range covers. It returns 0 (and selects the clicked file) when the anchor
// is unset or outside the current listing. The anchor is left untouched so
// several ranges can extend from the same start.
func selectRange(clicked string) int {
	paths := browserFiles()
	ai := slices.Index(paths, brw.anchor)
	ci := slices.Index(paths, clicked)
	if ai < 0 || ci < 0 {
		brw.sel[clicked] = true
		brw.anchor = clicked
		return 0
	}
	lo, hi := ai, ci
	if lo > hi {
		lo, hi = hi, lo
	}
	for i := lo; i <= hi; i++ {
		brw.sel[paths[i]] = true
	}
	return hi - lo + 1
}

// browserClick applies the click selection semantics on a file:
// plain click and cmd/ctrl-click toggle the file and move the anchor,
// shift-click selects the whole range between the anchor and the file
// (the anchor stays put, so several ranges can extend from it).
func browserClick(full string) {
	switch {
	case GetInputState().Modifiers&ModShift != 0:
		selectRange(full)
	case GetInputState().Modifiers&PrimaryMod() != 0:
		brw.sel[full] = !brw.sel[full]
		brw.anchor = full
	default:
		brw.sel[full] = !brw.sel[full]
		brw.anchor = full
	}
}

func openBrowser() {
	brw.sel = map[string]bool{}
	S.modal = "browser"
}

func truncateMiddle(s string, n int) string {
	if len(s) <= n {
		return s
	}
	half := (n - 3) / 2
	return s[:half] + "..." + s[len(s)-half:]
}

func choisirFichiers() {
	if S.busy {
		showError("ERREUR", "Un traitement est en cours, patientez.")
		return
	}
	if len(brw.sel) == 0 {
		showError("ERREUR", "VOUS DEVEZ CHOISIR UN OU PLUSIEURS FICHIERS")
		return
	}
	var picked []string
	for p := range brw.sel {
		picked = append(picked, p)
	}
	sort.Strings(picked)

	for _, p := range picked {
		if filepath.Dir(p) == S.outDir {
			showError("ERREUR", "VOUS NE POUVEZ PAS SELECTIONNER LE DOSSIER DES FICHIERS DEJA NORMALISES\nVEUILLEZ CHOISIR UN AUTRE DOSSIER OU FAIRE UNE COPIE DE VOS FICHIERS A NORMALISER")
			return
		}
	}
	closeModal()

	if err := os.MkdirAll(S.outDir, 0o755); err != nil {
		showError("ERREUR", err.Error())
		return
	}
	entries, _ := os.ReadDir(S.outDir)
	for _, e := range entries {
		if e.Type().IsRegular() {
			os.Remove(filepath.Join(S.outDir, e.Name()))
		}
	}
	for _, p := range picked {
		dst := filepath.Join(S.outDir, filepath.Base(p))
		if err := copyFile(p, dst); err != nil {
			showError("ERREUR", "Copie impossible: "+err.Error())
			return
		}
	}

	S.rows = nil
	for _, p := range listMP3(S.outDir) {
		S.rows = append(S.rows, &row{path: p, name: filepath.Base(p)})
	}
	startAnalyse()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if fi, err := os.Stat(src); err == nil {
		os.Chmod(dst, fi.Mode())
	}
	return nil
}

func raz() {
	if S.busy {
		showError("ERREUR", "Vous ne pouvez pas interrompre ce processus")
		return
	}
	if len(S.rows) == 0 {
		showError("ERREUR", "AUCUNE SELECTION A EFFACER")
		return
	}
	S.rows = nil
	S.progress = 0
	S.status = ""
	toastInfo("Sélection effacée")
}

func quitter() {
	if S.busy {
		S.modal = "quit"
		return
	}
	app.Quit()
}

func openReadme() {
	p := filepath.Join(S.cwd, "Norm_MP3.pdf")
	if _, err := os.Stat(p); err == nil {
		openWithDefault(p)
		return
	}
	toastInfo("Norm_MP3.pdf introuvable dans le dossier de travail")
}

func openWithDefault(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		toastInfo("Ouverture impossible: " + err.Error())
	}
}

func frameUpdate(fn func()) {
	WithFrameLock(func() {
		fn()
	})
	RequestNextFrame()
}

// ------------------------------------------------------------------ workers

func startAnalyse() {
	if S.busy {
		showError("ERREUR", "Vous ne pouvez pas interrompre ce processus")
		return
	}
	if len(S.rows) == 0 {
		showError("ERREUR", "AUCUNE SELECTION DE FICHIERS")
		return
	}

	files := listMP3(S.outDir)
	if len(files) == 0 {
		showError("ERREUR", "AUCUNE SELECTION DE FICHIERS")
		return
	}

	S.rows = S.rows[:0]
	for _, p := range files {
		S.rows = append(S.rows, &row{path: p, name: filepath.Base(p)})
	}

	go func() {
		start := time.Now()
		var total int64
		for _, r := range S.rows {
			if fi, err := os.Stat(r.path); err == nil {
				r.size = fi.Size()
				total += r.size
			}
		}
		frameUpdate(func() {
			S.busy = true
			S.busyKnd = "analyse"
			S.progress = 0
		})

		var cum int64
		var failed string
		for i, r := range S.rows {
			frameUpdate(func() {
				S.status = fmt.Sprintf("Analyse fichier (%d/%d) : %s   %d Ko", i+1, len(S.rows), r.name, r.size/1024)
			})
			lv, err := audio.AnalyzeFile(r.path, func(f float64) {
				frameUpdate(func() {
					S.progress = float32((float64(cum) + f*float64(r.size)) / float64(total))
				})
			})
			if err != nil {
				failed = r.name + ": " + err.Error()
				break
			}
			frameUpdate(func() {
				r.level = lv.Loudness
				r.clip = lv.Clipping
				r.target = S.target
				r.corr = S.target - lv.Loudness
				r.action = ""
				r.done = false
				r.fixed = false
				cum += r.size
			})
		}

		duree := fmtDuration(time.Since(start))
		frameUpdate(func() {
			S.busy = false
			S.progress = 1
			S.status = ""
			if failed != "" {
				showError("ERREUR", "Analyse interrompue:\n"+failed)
			} else {
				showMsg("INFORMATION", "Analyse terminée, vous pouvez maintenant traiter les fichiers\n\nDurée totale de l'analyse : "+duree)
			}
		})
	}()
}

func fmtDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d min %02d sec", m, s)
}

func startTraitement() {
	if S.busy {
		showError("ERREUR", "Vous ne pouvez pas interrompre ce processus")
		return
	}
	if len(S.rows) == 0 || !hasResults() {
		showError("ERREUR", "AUCUNE SELECTION A TRAITER \n VOUS DEVEZ D'ABORD ANALYSER LES FICHIERS")
		return
	}

	go func() {
		start := time.Now()
		var total int64
		for _, r := range S.rows {
			if fi, err := os.Stat(r.path); err == nil {
				r.size = fi.Size()
				total += r.size
			}
		}
		frameUpdate(func() {
			S.busy = true
			S.busyKnd = "traitement"
			S.progress = 0
		})

		var cum int64
		var toFix []*row
		var failed string

		for i, r := range S.rows {
			frameUpdate(func() {
				S.status = fmt.Sprintf("Traitement du fichier (%d/%d) : %s   %d Ko", i+1, len(S.rows), r.name, r.size/1024)
			})
			corr := r.corr
			var lv audio.Level
			var err error
			switch {
			case mathAbs(corr) < 0.5:
				frameUpdate(func() {
					r.action = "Aucune correction"
					r.level2 = r.level
					r.done = true
				})
			case mathAbs(corr) <= 5:
				lv, err = audio.ApplyGainFile(r.path, corr+0.4, progressFnFor(r, &cum, total))
				if err == nil {
					clip := lv.Clipping
					frameUpdate(func() {
						r.action = "Ampl.simple"
						r.level2 = lv.Loudness
						r.clip2 = clip
						r.done = true
					})
					if clip {
						toFix = append(toFix, r)
					}
				}
			default:
				gain := (S.target - 5) - r.level
				lv, err = audio.LoudNormFile(r.path, gain, progressFnFor(r, &cum, total))
				if err == nil {
					frameUpdate(func() {
						r.action = "Normalisation"
						r.level2 = lv.Loudness
						r.clip2 = lv.Clipping
						r.done = true
					})
				}
			}
			if err != nil {
				failed = r.name + ": " + err.Error()
				break
			}
			frameUpdate(func() {
				cum += r.size
			})
		}

		if failed == "" && len(toFix) > 0 {
			frameUpdate(func() {
				S.status = fmt.Sprintf("Normalisation des fichiers pouvant provoquer un écrêtage (%d fichier(s))", len(toFix))
				S.progress = 0
			})
			var fcum int64
			var ftotal int64
			for _, r := range toFix {
				ftotal += r.size
			}
			for _, r := range toFix {
				gain := (S.target - 5) - r.level2
				lv, err := audio.LoudNormFile(r.path, gain, func(f float64) {
					frameUpdate(func() {
						S.progress = float32((float64(fcum) + f*float64(r.size)) / float64(ftotal))
					})
				})
				if err != nil {
					failed = r.name + ": " + err.Error()
					break
				}
				frameUpdate(func() {
					r.level2 = lv.Loudness
					r.clip2 = lv.Clipping
					r.fixed = true
					fcum += r.size
				})
			}
		}

		duree := fmtDuration(time.Since(start))
		frameUpdate(func() {
			S.busy = false
			S.progress = 1
			S.status = ""
			if failed != "" {
				showError("ERREUR", "Traitement interrompu:\n"+failed)
			} else {
				showMsg("INFORMATION", "Traitement terminé.\n\nDurée totale du traitement : "+duree+
					"\n\nVous pouvez utiliser les fichiers maintenant normalisés\nqui se trouvent dans le dossier :\n\n"+strings.ToUpper(S.outDir))
			}
		})
	}()
}

func progressFnFor(r *row, cum *int64, total int64) audio.ProgressFn {
	size := r.size
	return func(f float64) {
		frameUpdate(func() {
			S.progress = float32((float64(*cum) + f*float64(size)) / float64(total))
		})
	}
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
