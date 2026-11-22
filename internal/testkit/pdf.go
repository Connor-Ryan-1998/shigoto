package testkit

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// PDF builds a minimal valid PDF with one page per entry of pages, each
// showing that entry's text in Helvetica. It lets tests create documents
// of any page count without binary fixtures.
func PDF(pages ...string) []byte {
	var objs []string
	// 1: catalog, 2: page tree, 3: font, then a page and content object
	// per page.
	n := len(pages)
	kids := make([]string, n)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	esc := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	for i, text := range pages {
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", esc.Replace(text))
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		)
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// PDFPages is PDF with n pages reading "Page 1" … "Page n".
func PDFPages(n int) []byte {
	pages := make([]string, n)
	for i := range pages {
		pages[i] = fmt.Sprintf("Page %d", i+1)
	}
	return PDF(pages...)
}

// EncryptedPDF returns pdf encrypted with AES and a user password, so it
// cannot be opened without one.
func EncryptedPDF(t testing.TB, pdf []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	conf := model.NewAESConfiguration("user-secret", "owner-secret", 256)
	if err := api.Encrypt(context.Background(), bytes.NewReader(pdf), &out, conf); err != nil {
		t.Fatalf("encrypting fixture PDF: %v", err)
	}
	return out.Bytes()
}
