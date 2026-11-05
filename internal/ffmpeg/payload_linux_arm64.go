//go:build linux && arm64

package ffmpeg

import _ "embed"

//go:embed bin/ffmpeg-linux-arm64.xz
var payload []byte
