//go:build !(linux && amd64) && !(linux && arm64) && !(windows && amd64) && !darwin

package ffmpeg

// No static ffmpeg build is available for this platform (e.g. windows/arm64):
// the application falls back to an ffmpeg found in PATH.
var payload []byte
