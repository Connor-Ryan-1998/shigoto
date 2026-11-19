package core

import "testing"

// ftyp builds an ISO-BMFF header: 4-byte box size, "ftyp", 4-byte brand.
func ftyp(brand string) []byte {
	b := []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'}
	b = append(b, brand...)
	return append(b, make([]byte, 16)...)
}

func TestSniffMIME(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want string
	}{
		{"quicktime", ftyp("qt  "), "video/quicktime"},
		{"heic", ftyp("heic"), "image/heic"},
		{"heix", ftyp("heix"), "image/heic"},
		{"mif1", ftyp("mif1"), "image/heic"},
		{"m4a", ftyp("M4A "), "audio/mp4"},
		{"isom", ftyp("isom"), "video/mp4"},
		{"mp42", ftyp("mp42"), "video/mp4"},
		{"avc1", ftyp("avc1"), "video/mp4"},
		{"webm", append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 20)...), "video/webm"},
		{"ogg", append([]byte("OggS"), make([]byte, 20)...), "audio/ogg"},
		{"mp3 id3", append([]byte("ID3"), make([]byte, 20)...), "audio/mpeg"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F'}, "image/jpeg"},
		{"png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), "image/png"},
		{"pdf", []byte("%PDF-1.7\n"), "application/pdf"},
		{"text", []byte("hello world"), "text/plain; charset=utf-8"},
		{"short", []byte{0, 0}, "application/octet-stream"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SniffMIME(c.head); got != c.want {
				t.Fatalf("SniffMIME = %q, want %q", got, c.want)
			}
		})
	}
}

func TestMediaPolicyAllowlist(t *testing.T) {
	p := MediaPolicy(123)
	if p.MaxBytes != 123 {
		t.Fatalf("MaxBytes = %d", p.MaxBytes)
	}
	for _, m := range []string{"image/jpeg", "image/png", "image/heic", "image/heif", "video/mp4", "video/quicktime", "video/webm", "audio/mp4", "audio/webm", "audio/ogg", "audio/mpeg"} {
		if !p.Allowed[m] {
			t.Errorf("%s not allowed", m)
		}
	}
	if p.Allowed["application/pdf"] {
		t.Error("pdf allowed in media policy")
	}
	if DocumentPolicy.MaxBytes != 10<<20 || !DocumentPolicy.Allowed["application/pdf"] || DocumentPolicy.Allowed["video/mp4"] {
		t.Errorf("DocumentPolicy = %+v", DocumentPolicy)
	}
}
