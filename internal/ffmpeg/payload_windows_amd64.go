//go:build windows && amd64

package ffmpeg

import _ "embed"

//go:embed bin/ffmpeg-windows-amd64.xz
var payload []byte
