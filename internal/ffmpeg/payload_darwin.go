//go:build darwin

package ffmpeg

import _ "embed"

//go:embed bin/ffmpeg-darwin-universal.xz
var payload []byte
