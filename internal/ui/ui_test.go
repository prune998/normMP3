package ui

import (
	"os"
	"path/filepath"
	"testing"

	. "go.hasen.dev/shirei"
)

func TestHeadlessRender(t *testing.T) {
	dir := t.TempDir()
	S.cwd = dir
	S.outDir = filepath.Join(dir, "Fichiers_normalises")
	S.rows = nil
	S.busy = false
	S.modal = ""

	if err := RenderToPNG(filepath.Join(dir, "empty.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("empty view: %v", err)
	}

	target := 89.0
	S.rows = []*row{
		{path: "/x/a.mp3", name: "a.mp3", target: target, level: 84.21, corr: 4.79},
		{path: "/x/b.mp3", name: "b.mp3", target: target, level: 89.10, corr: -0.10, action: "Aucune correction", level2: 89.10, done: true},
		{path: "/x/c.mp3", name: "c.mp3", target: target, level: 95.30, corr: -6.30, action: "Normalisation", level2: 84.02, done: true, clip2: true},
	}
	S.busy = true
	S.progress = 0.42
	S.status = "Analyse fichier (2/3) : b.mp3   4096 Ko"
	if err := RenderToPNG(filepath.Join(dir, "busy.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("busy view: %v", err)
	}

	S.busy = false
	S.modal = "gain"
	S.gainBuf = 91.5
	if err := RenderToPNG(filepath.Join(dir, "gain.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("gain modal: %v", err)
	}

	S.modal = "browser"
	if err := RenderToPNG(filepath.Join(dir, "browser.png"), 1280, 720, RootView); err != nil {
		t.Fatalf("browser modal: %v", err)
	}

	for _, f := range []string{"empty.png", "busy.png", "gain.png", "browser.png"} {
		if fi, err := os.Stat(filepath.Join(dir, f)); err != nil || fi.Size() < 1000 {
			t.Errorf("%s missing or suspiciously small", f)
		}
	}
}
