package clients

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"shigoto/internal/property"
)

// ConsentText is the exact consent wording shown to, and recorded for, a
// client signing on behalf of clientName.
func ConsentText(clientName string) string {
	return "I agree this is my electronic signature and I am authorised to sign for " + clientName + "."
}

// CertificateData is the evidence printed on a signing certificate.
type CertificateData struct {
	DocumentTitle string
	FileName      string
	FileSHA256    []byte
	SignerName    string
	SignerEmail   string
	ClientName    string
	OrgName       string
	IP            string
	UserAgent     string
	ConsentText   string
	SignedAt      time.Time
	SignatureID   int64
}

const certTimeLayout = "2 Jan 2006 15:04:05 MST"

// certificateRows are the certificate's label/value rows, in print order.
func certificateRows(c CertificateData, loc *time.Location) [][2]string {
	return [][2]string{
		{"Document", c.DocumentTitle},
		{"File name", c.FileName},
		{"File SHA-256", hex.EncodeToString(c.FileSHA256)},
		{"Signed by", c.SignerName},
		{"Signer email", c.SignerEmail},
		{"On behalf of", c.ClientName},
		{"Issued by", c.OrgName},
		{"Signed at (UTC)", c.SignedAt.UTC().Format(certTimeLayout)},
		{"Signed at (local)", c.SignedAt.In(loc).Format(certTimeLayout)},
		{"IP address", c.IP},
		{"Browser", c.UserAgent},
		{"Consent", c.ConsentText},
		{"Signature ID", fmt.Sprintf("%d", c.SignatureID)},
	}
}

// certificateLines is the certificate's text content, one row per line.
func certificateLines(c CertificateData, loc *time.Location) []string {
	rows := certificateRows(c, loc)
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = r[0] + ": " + r[1]
	}
	return lines
}

// WriteCertificatePDF writes c as a one-page A4 signing certificate, with
// local times in loc.
func WriteCertificatePDF(w io.Writer, c CertificateData, loc *time.Location) error {
	const (
		pageW  = 595.28
		margin = 40.0
		labelW = 110.0
		rowH   = 14.0
	)
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	opt := gopdf.TtfOption{OnGlyphNotFoundSubstitute: property.BaseLetter}
	if err := pdf.AddTTFFontDataWithOption("go", goregular.TTF, opt); err != nil {
		return err
	}
	if err := pdf.AddTTFFontDataWithOption("go-bold", gobold.TTF, opt); err != nil {
		return err
	}
	pdf.AddPage()
	y := 50.0
	write := func(x float64, font string, size float64, s string) error {
		if err := pdf.SetFont(font, "", size); err != nil {
			return err
		}
		pdf.SetXY(x, y)
		return pdf.Cell(nil, s)
	}
	if err := write(margin, "go-bold", 16, "Signing certificate"); err != nil {
		return err
	}
	y += 22
	if err := write(margin, "go", 9, "This certificate records the electronic signature of the document below. The document file is unchanged; its SHA-256 fingerprint identifies the exact bytes signed."); err != nil {
		return err
	}
	y += 26
	valueW := pageW - 2*margin - labelW
	for _, r := range certificateRows(c, loc) {
		if err := write(margin, "go-bold", 9, r[0]); err != nil {
			return err
		}
		if err := pdf.SetFont("go", "", 9); err != nil {
			return err
		}
		lines, err := pdf.SplitTextWithWordWrap(r[1], valueW)
		if err != nil || len(lines) == 0 {
			lines = []string{r[1]}
		}
		for _, line := range lines {
			pdf.SetXY(margin+labelW, y)
			if err := pdf.Cell(nil, strings.TrimSpace(line)); err != nil {
				return err
			}
			y += rowH
		}
		y += 4
	}
	return pdf.Write(w)
}
