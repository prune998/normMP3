// Package ffmpeg gives the application access to a static ffmpeg binary:
// one is embedded per platform (xz-compressed, see scripts/fetch-ffmpeg.sh)
// and extracted into the user cache directory on first use. Platforms with
// no embedded payload fall back to an ffmpeg found in PATH.
package ffmpeg

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/ulikunitz/xz"
)

// ErrNotEmbedded is returned when no binary is embedded for this platform
// and no ffmpeg was found in PATH.
var ErrNotEmbedded = errors.New("ffmpeg: no embedded binary for this platform and no ffmpeg in PATH")

// Available reports whether a binary is embedded for the current platform.
func Available() bool {
	return len(payload) > 0
}

var (
	once       sync.Once
	extracted  string
	extractErr error
)

// Executable returns a usable ffmpeg binary path: the embedded payload
// extracted into the user cache directory (once, atomically), or the
// ffmpeg found in PATH for platforms without an embedded payload.
func Executable() (string, error) {
	once.Do(func() {
		if len(payload) == 0 {
			p, err := exec.LookPath("ffmpeg")
			if err != nil {
				extractErr = ErrNotEmbedded
				return
			}
			extracted = p
			return
		}

		dir, err := cacheDir()
		if err != nil {
			extractErr = err
			return
		}
		sum := sha256.Sum256(payload)
		name := fmt.Sprintf("ffmpeg-%s-%x%s", versionTag(), sum[:4], exeExt())
		path := filepath.Join(dir, name)
		if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() && fi.Size() > 1024 {
			extracted = path
			return
		}

		xr, err := xz.NewReader(bytes.NewReader(payload))
		if err != nil {
			extractErr = fmt.Errorf("ffmpeg: payload: %w", err)
			return
		}
		tmp, err := os.CreateTemp(dir, ".ffmpeg-*"+exeExt())
		if err != nil {
			extractErr = err
			return
		}
		tmpName := tmp.Name()
		if _, err := io.Copy(tmp, xr); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			extractErr = err
			return
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmpName)
			extractErr = err
			return
		}
		if err := os.Chmod(tmpName, 0o755); err != nil {
			os.Remove(tmpName)
			extractErr = err
			return
		}
		if runtime.GOOS != "windows" {
			_ = os.Remove(path)
		}
		if err := os.Rename(tmpName, path); err != nil {
			os.Remove(tmpName)
			extractErr = err
			return
		}
		extracted = path
	})
	return extracted, extractErr
}

func versionTag() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		// no XDG cache / no HOME: fall back to the temp directory
		base = os.TempDir()
	}
	dir := filepath.Join(base, "normMP3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("ffmpeg: cache: %w", err)
	}
	return dir, nil
}

// Run executes ffmpeg with the given arguments, hiding its banner. On
// failure the last lines of stderr are included in the error.
func Run(args ...string) error {
	exe, err := Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg %s: %w: %s", args[0], err, lastLines(stderr.String(), 3))
	}
	return nil
}

// RunProgress runs ffmpeg with the given arguments, which must include
// "-progress pipe:1": the machine-readable progress written to stdout is
// parsed (out_time_us) to report the completion fraction against totalMicros.
func RunProgress(args []string, totalMicros int64, progress func(fraction float64)) error {
	exe, err := Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if v, ok := strings.CutPrefix(line, "out_time_us="); ok && totalMicros > 0 && progress != nil {
			var us int64
			if _, err := fmt.Sscanf(v, "%d", &us); err == nil {
				f := float64(us) / float64(totalMicros)
				progress(math.Min(1, math.Max(0, f)))
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg %s: %w: %s", args[0], err, lastLines(stderr.String(), 3))
	}
	if progress != nil {
		progress(1)
	}
	return nil
}

func lastLines(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no stderr output)"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
