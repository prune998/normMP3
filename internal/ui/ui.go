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
	mTitle   string
	mMsg     string
	gainBuf  float32
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

// browserEntries lists the current directory the way the browser modal
// displays it: directories first, then files, each group sorted by name,
// hidden entries skipped.
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

func Run(version string) {
	S.version = version
	S.cwd = workDir()
	S.outDir = filepath.Join(S.cwd, "Fichiers_normalises")
	loadMemo()
	brw.cwd = homeDir()

	app.SetupWindow("APPLICATION : NORMALISATION GAINS FICHIERS MP3", 1280, 720)
	app.Run(RootView)
}

func RootView() {
	Container(Attrs(Viewport, Background(200, 55, 96, 1)), func() {
		menubar()
		Container(Attrs(Row, Grow(1), Expand, Gap(8), Pad(8), Clip), func() {
			tablePanel()
			selectionPanel()
		})
		statusBar()
	})
	renderModals()
}

func menubar() {
	Container(Attrs(Row, CrossMid, Pad2(4, 6), Gap(6), Background(180, 45, 75, 1)), func() {
		MenuButton(NoIcon, "Fichiers", func() {
			if MenuItem(NoIcon, "Choisir les fichiers") {
				openBrowser()
			}
			MenuSeparator()
			if MenuItem(NoIcon, "Effacer la sélection") {
				raz()
			}
			MenuSeparator()
			if MenuItem(NoIcon, "Quitter") {
				quitter()
			}
		})
		MenuButton(NoIcon, "Action", func() {
			if MenuItem(NoIcon, "Choix du gain cible") {
				openGainModal()
			}
			MenuSeparator()
			if MenuItem(NoIcon, "Analyse") {
				startAnalyse()
			}
			MenuSeparator()
			if MenuItem(NoIcon, "Traitement") {
				startTraitement()
			}
		})
		MenuButton(NoIcon, "A propos ...", func() {
			if MenuItem(NoIcon, "Infos") {
				showMsg("A propos de l'application",
					"Application de normalisation des fichiers MP3\nPortage Go (shirei) de l'application Tkinter originale\nAuteur original : Elie Couzinié\nVersion : "+S.version)
			}
			MenuSeparator()
			if MenuItem(NoIcon, "Lisez-moi ...") {
				openReadme()
			}
		})
		Filler(1)
		Label(fmt.Sprintf("Gain cible = %.1f dB", S.target),
			FontSize(14), TextColor(50, 60, 25, 1))
	})
}

func tablePanel() {
	Container(Attrs(Grow(1), Expand, Clip, Background(0, 0, 100, 1), Corners(6), BorderWidth(1), BorderColor(200, 30, 70, 1)), func() {
		header := func() {
			Container(Attrs(Row, FixHeight(26), CrossMid, Background(60, 85, 85, 1)), func() {
				col("Fichier", 250, AlignStart)
				col("Niveau", 70, AlignEnd)
				col("Cible", 70, AlignEnd)
				col("Corr.", 70, AlignEnd)
				col("Action", 150, AlignMiddle)
				col("Niveau final", 90, AlignEnd)
				col("Ecr.", 40, AlignMiddle)
			})
		}
		if len(S.rows) == 0 {
			Container(Attrs(Expand, Grow(1), CrossMid, MainAlign(AlignMiddle)), func() {
				header()
				Container(Attrs(FixHeight(300), CrossMid, MainAlign(AlignMiddle)), func() {
					Label("Utilisez Fichiers > Choisir les fichiers pour sélectionner des MP3", FontSize(14), TextColor(210, 15, 45, 1))
				})
			})
			return
		}
		Container(Attrs(Grow(1), Expand, Clip), func() {
			ScrollOnInput()
			header()
			for i, r := range S.rows {
				r := r
				ContainerWithKey(r.path, Attrs(Row, FixHeight(24), CrossMid), func() {
					bg := Vec4{0, 0, 100, 1}
					if i%2 == 1 {
						bg = Vec4{200, 30, 96, 1}
					}
					ModAttrs(BackgroundVec(bg))
					if IsHovered() {
						ModAttrs(Background(200, 45, 88, 1))
					}
					cell(r.name, 250, AlignStart)
					cell(fmt.Sprintf("%.2f", r.level), 70, AlignEnd)
					cell(fmt.Sprintf("%.2f", r.target), 70, AlignEnd)
					cell(fmt.Sprintf("%+.2f", r.corr), 70, AlignEnd)
					cell(r.action, 150, AlignMiddle, actionColor(r))
					if r.done {
						cell(fmt.Sprintf("%.2f", r.level2), 90, AlignEnd)
						ecr := " "
						if r.clip2 {
							ecr = "Y"
						}
						cell(ecr, 40, AlignMiddle, Vec4{0, 70, 45, 1})
					} else {
						cell("", 90, AlignEnd)
						cell("", 40, AlignMiddle)
					}
				})
			}
			ScrollBars()
		})
	})
}

func col(label string, w float32, al Alignment) {
	Container(Attrs(FixWidth(w), CrossMid, MainAlign(al), Pad2(0, 6)), func() {
		Label(label, FontSize(12.5), TextColor(210, 40, 20, 1))
	})
}

func cell(text string, w float32, al Alignment, color ...Vec4) {
	c := Vec4{0, 0, 15, 1}
	if len(color) == 1 {
		c = color[0]
	}
	Container(Attrs(FixWidth(w), CrossMid, MainAlign(al), Pad2(0, 6)), func() {
		Label(text, FontSize(12.5), TextColorVec(c))
	})
}

func actionColor(r *row) Vec4 {
	switch {
	case r.action == "Aucune correction":
		return Vec4{120, 60, 30, 1}
	case r.action == "Ampl.simple":
		return Vec4{210, 70, 35, 1}
	case r.action == "Normalisation":
		return Vec4{0, 70, 45, 1}
	}
	return Vec4{0, 0, 15, 1}
}

func selectionPanel() {
	Container(Attrs(FixWidth(320), Expand, Clip, Background(0, 0, 100, 1), Corners(6), BorderWidth(1), BorderColor(200, 30, 70, 1)), func() {
		Container(Attrs(FixHeight(26), CrossMid, Pad2(0, 6), Background(60, 85, 85, 1)), func() {
			Label("Liste des fichiers sélectionnés", FontSize(12.5), TextColor(210, 40, 20, 1))
		})
		if len(S.rows) == 0 {
			return
		}
		Container(Attrs(Grow(1), Expand, Clip), func() {
			ScrollOnInput()
			for _, r := range S.rows {
				r := r
				ContainerWithKey("sel-"+r.path, Attrs(Row, FixHeight(24), CrossMid, Pad2(0, 6)), func() {
					c := Vec4{0, 0, 15, 1}
					if r.done {
						if r.fixed {
							c = Vec4{120, 60, 30, 1}
						} else {
							c = Vec4{0, 70, 45, 1}
						}
					}
					if IsHovered() {
						ModAttrs(Background(200, 80, 85, 1))
					}
					if IsDoubleClicked() {
						openWithDefault(r.path)
					}
					Label(r.name, FontSize(12.5), TextColorVec(c))
				})
			}
			ScrollBars()
		})
	})
}

func statusBar() {
	Container(Attrs(Row, CrossMid, FixHeight(40), Pad2(6, 8), Gap(12), Background(180, 45, 92, 1)), func() {
		if S.busy {
			Container(Attrs(FixWidth(220), CrossMid), func() {
				ProgressBar(S.progress)
			})
			Label(S.status, FontSize(13), TextColor(0, 0, 15, 1))
		} else if len(S.rows) > 0 {
			Label(fmt.Sprintf("%d fichier(s) prêt(s) — gain cible %.1f dB", len(S.rows), S.target),
				FontSize(13), TextColor(210, 15, 35, 1))
		} else {
			Label("Prêt", FontSize(13), TextColor(210, 15, 35, 1))
		}
	})
}

func renderModals() {
	switch S.modal {
	case "msg":
		Modal(520, closeModal, func() {
			Container(Attrs(Pad(18), Gap(14)), func() {
				Label(S.mTitle, FontSize(16), FontWeight(WeightBold))
				for _, line := range strings.Split(S.mMsg, "\n") {
					Label(line, FontSize(13.5))
				}
				Container(Attrs(Row, MainAlign(AlignMiddle), Pad2(6, 0)), func() {
					if Button(NoIcon, "OK") {
						closeModal()
					}
				})
			})
		})
	case "gain":
		Modal(460, closeModal, func() {
			Container(Attrs(Pad(18), Gap(14)), func() {
				Label("Choisissez le gain cible", FontSize(16), FontWeight(WeightBold))
				Container(Attrs(Row, CrossMid, Gap(12)), func() {
					Slider(&S.gainBuf, SliderAttrs{Min: 85, Max: 93, Step: 0.5, Width: 280})
					Label(fmt.Sprintf("%.1f dB", S.gainBuf), FontSize(15), FontWeight(WeightBold))
				})
				Container(Attrs(Row, Gap(10), MainAlign(AlignMiddle), Pad2(8, 0)), func() {
					if Button(NoIcon, "Valider") {
						validerGain(float64(S.gainBuf))
					}
					if Button(NoIcon, "Sortie sans valider") {
						closeModal()
					}
				})
			})
		})
	case "browser":
		renderBrowser()
	case "quit":
		Modal(560, closeModal, func() {
			Container(Attrs(Pad(18), Gap(14)), func() {
				Label("ARRÊT PRÉMATURÉ ...", FontSize(16), FontWeight(WeightBold), TextColor(0, 70, 40, 1))
				Label("Le traitement est en cours d'exécution.", FontSize(13.5))
				Label("Certains fichiers pourraient être corrompus si vous arrêtez maintenant.", FontSize(13.5))
				Label("Voulez-vous vraiment arrêter ?", FontSize(13.5))
				Container(Attrs(Row, Gap(10), MainAlign(AlignMiddle), Pad2(8, 0)), func() {
					if Button(NoIcon, "Oui, arrêter le traitement") {
						app.Quit()
					}
					if Button(NoIcon, "Non, continuer le traitement") {
						closeModal()
					}
				})
			})
		})
	}
}

func openGainModal() {
	S.gainBuf = float32(S.target)
	S.modal = "gain"
}

func validerGain(v float64) {
	S.target = v
	saveMemo()
	closeModal()
	startAnalyse()
}

func closeModal() { S.modal = "" }

func showMsg(title, msg string) {
	S.modal = "msg"
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

func openBrowser() {
	brw.sel = map[string]bool{}
	S.modal = "browser"
}

func renderBrowser() {
	Modal(640, closeModal, func() {
		Container(Attrs(Pad(14), Gap(10)), func() {
			Label("SELECTIONNER UN OU PLUSIEURS FICHIERS MP3", FontSize(15), FontWeight(WeightBold))
			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				if Button(NoIcon, "Dossier parent") {
					brw.cwd = filepath.Dir(brw.cwd)
				}
				if Button(NoIcon, "Home") {
					brw.cwd = homeDir()
				}
				Label(truncateMiddle(brw.cwd, 52), FontSize(12.5), TextColor(210, 15, 35, 1))
			})
			Label("Clic : cocher — Maj+clic : plage — Cmd/Ctrl+clic : basculer",
				FontSize(12), TextColor(210, 10, 40, 1))
			Container(Attrs(Grow(1), Expand, Clip, FixHeight(380), Background(0, 0, 100, 1), Corners(6), BorderWidth(1)), func() {
				ScrollOnInput()
				entries := browserEntries()
				if entries == nil {
					Label("dossier illisible: "+brw.cwd, TextColor(0, 70, 45, 1))
				}
				for _, e := range entries {
					name := e.Name()
					if strings.HasPrefix(name, ".") {
						continue
					}
					if e.IsDir() {
						d := name
						ContainerWithKey("d-"+d, Attrs(Row, FixHeight(24), CrossMid, Pad2(0, 8)), func() {
							if IsHovered() {
								ModAttrs(Background(200, 45, 92, 1))
							}
							if IsClicked() {
								brw.cwd = filepath.Join(brw.cwd, d)
							}
							Label("[dossier] "+d, FontSize(12.5), TextColor(210, 60, 30, 1))
						})
						continue
					}
					if !strings.HasSuffix(name, ".mp3") && !strings.HasSuffix(name, ".MP3") {
						continue
					}
					full := filepath.Join(brw.cwd, name)
					ContainerWithKey("f-"+full, Attrs(Row, FixHeight(24), CrossMid, Pad2(0, 8)), func() {
						if IsHovered() {
							ModAttrs(Background(200, 45, 92, 1))
						}
						if IsClicked() {
							browserClick(full)
						}
						mark := "[  ]"
						c := Vec4{0, 0, 30, 1}
						if brw.sel[full] {
							mark = "[x]"
							c = Vec4{150, 65, 28, 1}
						}
						Label(mark, FontSize(12.5), TextColorVec(c))
						Label(name, FontSize(12.5), TextColorVec(c))
					})
				}
				ScrollBars()
			})
			Container(Attrs(Row, CrossMid, Gap(10)), func() {
				Label(fmt.Sprintf("%d fichier(s) sélectionné(s)", len(brw.sel)), FontSize(13))
				Filler(1)
				if Button(NoIcon, "Annuler") {
					closeModal()
				}
				NextButtonType(ButtonPrimary)
				if Button(NoIcon, "Choisir") {
					choisirFichiers()
				}
			})
		})
	})
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
		showMsg("ERREUR", "Un traitement est en cours, patientez.")
		return
	}
	if len(brw.sel) == 0 {
		showMsg("ERREUR", "VOUS DEVEZ CHOISIR UN OU PLUSIEURS FICHIERS")
		return
	}
	var picked []string
	for p := range brw.sel {
		picked = append(picked, p)
	}
	sort.Strings(picked)

	for _, p := range picked {
		if filepath.Dir(p) == S.outDir {
			showMsg("ERREUR", "VOUS NE POUVEZ PAS SELECTIONNER LE DOSSIER DES FICHIERS DEJA NORMALISES\nVEUILLEZ CHOISIR UN AUTRE DOSSIER OU FAIRE UNE COPIE DE VOS FICHIERS A NORMALISER")
			return
		}
	}
	closeModal()

	if err := os.MkdirAll(S.outDir, 0o755); err != nil {
		showMsg("ERREUR", err.Error())
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
			showMsg("ERREUR", "Copie impossible: "+err.Error())
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
		showMsg("ERREUR", "Vous ne pouvez pas interrompre ce processus")
		return
	}
	if len(S.rows) == 0 {
		showMsg("ERREUR", "AUCUNE SELECTION A EFFACER")
		return
	}
	S.rows = nil
	S.progress = 0
	S.status = ""
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
	showMsg("INFORMATION", "Norm_MP3.pdf introuvable dans le dossier de travail")
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
	cmd.Start()
}

func frameUpdate(fn func()) {
	WithFrameLock(func() {
		fn()
	})
	RequestNextFrame()
}

func startAnalyse() {
	if S.busy {
		showMsg("ERREUR", "Vous ne pouvez pas interrompre ce processus")
		return
	}
	if len(S.rows) == 0 {
		showMsg("ERREUR", "AUCUNE SELECTION DE FICHIERS")
		return
	}

	files := listMP3(S.outDir)
	if len(files) == 0 {
		showMsg("ERREUR", "AUCUNE SELECTION DE FICHIERS")
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
				showMsg("ERREUR", "Analyse interrompue:\n"+failed)
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
		showMsg("ERREUR", "Vous ne pouvez pas interrompre ce processus")
		return
	}
	if len(S.rows) == 0 {
		showMsg("ERREUR", "AUCUNE SELECTION A TRAITER \n VOUS DEVEZ D'ABORD ANALYSER LES FICHIERS")
		return
	}
	analyzed := true
	for _, r := range S.rows {
		if r.target == 0 {
			analyzed = false
		}
	}
	if !analyzed {
		showMsg("ERREUR", "AUCUNE SELECTION A TRAITER \n VOUS DEVEZ D'ABORD ANALYSER LES FICHIERS")
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
				showMsg("ERREUR", "Traitement interrompu:\n"+failed)
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
