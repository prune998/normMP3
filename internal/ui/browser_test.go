package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func setupBrowserDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	names := []string{"a.mp3", "b.mp3", "c.mp3", "d.MP3", "notes.txt", "z-sub", ".hidden.mp3"}
	for _, n := range names {
		p := filepath.Join(dir, n)
		if n == "z-sub" {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	brw.cwd = dir
	brw.sel = map[string]bool{}
	brw.anchor = ""
	t.Cleanup(func() {
		brw.sel = map[string]bool{}
		brw.anchor = ""
	})
}

func TestBrowserFilesOrderAndFilter(t *testing.T) {
	setupBrowserDir(t)
	files := browserFiles()
	want := []string{
		filepath.Join(brw.cwd, "a.mp3"),
		filepath.Join(brw.cwd, "b.mp3"),
		filepath.Join(brw.cwd, "c.mp3"),
		filepath.Join(brw.cwd, "d.MP3"),
	}
	if !slices.Equal(files, want) {
		t.Errorf("browserFiles = %v, want %v", files, want)
	}
}

func TestSelectRangeForward(t *testing.T) {
	setupBrowserDir(t)
	files := browserFiles()
	brw.anchor = files[0]

	n := selectRange(files[2])
	if n != 3 {
		t.Fatalf("selectRange returned %d, want 3", n)
	}
	for _, p := range files[:3] {
		if !brw.sel[p] {
			t.Errorf("%s should be selected", p)
		}
	}
	if brw.sel[files[3]] {
		t.Errorf("%s should NOT be selected", files[3])
	}
}

func TestSelectRangeBackward(t *testing.T) {
	setupBrowserDir(t)
	files := browserFiles()
	brw.anchor = files[3]

	selectRange(files[1])
	for _, p := range files[1:4] {
		if !brw.sel[p] {
			t.Errorf("%s should be selected", p)
		}
	}
	if brw.sel[files[0]] {
		t.Errorf("%s should NOT be selected", files[0])
	}
}

func TestSelectRangeKeepsAnchorForExtension(t *testing.T) {
	setupBrowserDir(t)
	files := browserFiles()
	brw.anchor = files[0]

	selectRange(files[1])
	if brw.anchor != files[0] {
		t.Errorf("anchor moved to %s, want %s", brw.anchor, files[0])
	}
	selectRange(files[3])
	for _, p := range files[:4] {
		if !brw.sel[p] {
			t.Errorf("%s should be selected after two ranges", p)
		}
	}
}

func TestSelectRangeWithoutAnchor(t *testing.T) {
	setupBrowserDir(t)
	files := browserFiles()

	if n := selectRange(files[2]); n != 0 {
		t.Fatalf("selectRange without anchor returned %d, want 0", n)
	}
	if !brw.sel[files[2]] {
		t.Error("clicked file should be selected as fallback")
	}
	if brw.anchor != files[2] {
		t.Errorf("anchor = %s, want %s", brw.anchor, files[2])
	}
}
