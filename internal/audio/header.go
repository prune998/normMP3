package audio

import (
	"errors"
	"fmt"
	"os"
)

// Channels returns the number of channels (1 or 2) of the MP3 file by
// reading the channel mode of its first frame header. go-mp3 always decodes
// to stereo (duplicating mono sources), so the header is the only way to
// know the true layout and keep mono files mono on re-encode.
func Channels(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	header := make([]byte, 10)
	// skip an ID3v2 tag if present (10-byte header + synchsafe size)
	if _, err := f.ReadAt(header[:3], 0); err != nil {
		return 0, err
	}
	off := int64(0)
	if string(header[:3]) == "ID3" {
		if _, err := f.ReadAt(header[:10], 0); err != nil {
			return 0, err
		}
		size := int64(header[6]&0x7F)<<21 | int64(header[7]&0x7F)<<14 |
			int64(header[8]&0x7F)<<7 | int64(header[9]&0x7F)
		off = 10 + size
	}

	buf := make([]byte, 64*1024)
	n, err := f.ReadAt(buf, off)
	if n < 4 {
		if err != nil && off > 0 {
			return 0, errors.New("audio: no mp3 frame found after ID3 tag")
		}
	}
	for i := 0; i+3 < n; i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		version := (buf[i+1] >> 3) & 3
		layer := (buf[i+1] >> 1) & 3
		bitrateIdx := (buf[i+2] >> 4) & 0xF
		srIdx := (buf[i+2] >> 2) & 3
		if version == 1 || layer == 0 || bitrateIdx == 0 || bitrateIdx == 15 || srIdx == 3 {
			continue
		}
		mode := (buf[i+3] >> 6) & 3
		if mode == 3 {
			return 1, nil
		}
		return 2, nil
	}
	return 0, fmt.Errorf("audio: %s: no valid mp3 frame header found", path)
}
