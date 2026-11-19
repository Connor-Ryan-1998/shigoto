package core

import (
	"bytes"
	"net/http"
)

// SniffMIME returns the MIME type of a file from its leading bytes (up to
// 512). It recognises the phone media formats http.DetectContentType misses
// or misreports - ISO-BMFF containers by ftyp brand (MOV, HEIC/HEIF, M4A,
// MP4), Matroska/WebM, Ogg and ID3-tagged MP3 - and otherwise defers to
// http.DetectContentType.
func SniffMIME(head []byte) string {
	if len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp")) {
		switch string(head[8:12]) {
		case "qt  ":
			return "video/quicktime"
		case "heic", "heix", "hevc", "hevx", "heim", "heis":
			return "image/heic"
		case "mif1", "msf1":
			return "image/heic"
		case "M4A ", "M4B ":
			return "audio/mp4"
		default:
			return "video/mp4"
		}
	}
	switch {
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "video/webm"
	case bytes.HasPrefix(head, []byte("OggS")):
		return "audio/ogg"
	case bytes.HasPrefix(head, []byte("ID3")):
		return "audio/mpeg"
	}
	return http.DetectContentType(head)
}
