package clients

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func sampleCertificate() CertificateData {
	return CertificateData{
		DocumentTitle: "Master Services Agreement", FileName: "msa.pdf",
		FileSHA256: []byte{0xde, 0xad, 0xbe, 0xef},
		SignerName: "Priya Nair", SignerEmail: "priya@acme.example", ClientName: "Acme Pty Ltd",
		OrgName: "Studio", IP: "203.0.113.7", UserAgent: "Mozilla/5.0 Test",
		ConsentText: ConsentText("Acme Pty Ltd"),
		SignedAt:    time.Date(2026, 11, 23, 4, 30, 0, 0, time.UTC), SignatureID: 42,
	}
}

func TestCertificateContainsEvidence(t *testing.T) {
	brisbane, err := time.LoadLocation("Australia/Brisbane")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(certificateLines(sampleCertificate(), brisbane), "\n")
	for _, want := range []string{
		"Master Services Agreement", "msa.pdf", "deadbeef", "Priya Nair", "priya@acme.example",
		"Acme Pty Ltd", "Studio", "203.0.113.7", "Mozilla/5.0 Test",
		"I agree this is my electronic signature and I am authorised to sign for Acme Pty Ltd.",
		"23 Nov 2026 04:30:00 UTC", "23 Nov 2026 14:30:00 AEST", "42",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("certificate text missing %q:\n%s", want, text)
		}
	}
}

func TestCertificatePDF(t *testing.T) {
	var buf bytes.Buffer
	c := sampleCertificate()
	c.SignerName = "Zoë Đặng 王小明"
	c.UserAgent = strings.Repeat("VeryLongUserAgentToken/1.0 ", 20)
	if err := WriteCertificatePDF(&buf, c, time.UTC); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
}

func TestConsentText(t *testing.T) {
	if got := ConsentText("Acme"); got != "I agree this is my electronic signature and I am authorised to sign for Acme." {
		t.Fatalf("ConsentText = %q", got)
	}
}
