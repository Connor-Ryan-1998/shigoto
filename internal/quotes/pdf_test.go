package quotes

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/property"
)

func samplePDFInput() PDFInput {
	return PDFInput{
		Quote: Quote{
			Number: 7, Revision: 2, Title: "Switchboard upgrade", Terms: "50% deposit on acceptance.",
			IssueDate: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), ValidUntil: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			Subtotal: 48000, GSTTotal: 4800, Total: 52800,
			Lines: []Line{
				{Kind: KindLabour, Description: "Rough-in", QuantityMilli: 2500, UnitAmount: 12000, GSTCode: core.GST, GSTAmount: 3000, Amount: 30000},
				{Kind: KindMaterial, Description: "Switchboard", QuantityMilli: 1000, UnitAmount: 18000, GSTCode: core.GST, GSTAmount: 1800, Amount: 18000},
			},
		},
		Org: core.OrgSettings{
			Name: "Acme Electrical", ABN: "51 824 753 556", Address: "1 Main St\nBrisbane QLD 4000",
			Phone: "07 3000 1234", Email: "hi@acme.test",
			Licences: []core.Licence{{Type: "QBCC", Number: "1234567"}, {Type: "Electrical", Number: "98765"}},
		},
		Customer:    core.Contact{Name: "Priya Nair", CompanyName: "Nair Holdings", Email: "priya@example.com"},
		ProjectName: "Kitchen reno",
	}
}

func TestQuotePDFContent(t *testing.T) {
	in := samplePDFInput()
	text := buildPDFModel(in).text()
	for _, want := range []string{
		"Quote Q-0007-R2", "Switchboard upgrade", "Acme Electrical", "ABN 51 824 753 556", "1 Main St", "Brisbane QLD 4000",
		"07 3000 1234", "hi@acme.test", "QBCC 1234567", "Electrical 98765",
		"Priya Nair", "Nair Holdings", "priya@example.com", "Kitchen reno",
		"Issued 1 Dec 2026", "Valid until 31 Dec 2026",
		"Labour", "Rough-in", "2.5", "$120.00", "$300.00",
		"Materials", "Switchboard", "$180.00",
		"Subtotal $480.00", "GST $48.00", "Total $528.00",
		"Terms", "50% deposit on acceptance.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("quote PDF text missing %q:\n%s", want, text)
		}
	}
}

func TestQuotePDFNoProfile(t *testing.T) {
	in := samplePDFInput()
	in.Org = core.OrgSettings{Name: "Acme"}
	in.Quote.GSTTotal, in.Quote.Total = 0, 48000
	in.Quote.Lines = in.Quote.Lines[:1]
	text := buildPDFModel(in).text()
	for _, unwanted := range []string{"ABN", "GST $", "Materials"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("text has %q:\n%s", unwanted, text)
		}
	}
	var buf bytes.Buffer
	if err := WriteQuotePDF(&buf, in); err != nil || !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("WriteQuotePDF: %v", err)
	}
}

func TestQuotePDFLongAndLogo(t *testing.T) {
	in := samplePDFInput()
	var logo bytes.Buffer
	if err := png.Encode(&logo, image.NewRGBA(image.Rect(0, 0, 400, 100))); err != nil {
		t.Fatal(err)
	}
	in.Logo = logo.Bytes()
	in.Customer.Name = "Zoë Đặng 王小明"
	in.Quote.Lines = nil
	for i := range 60 {
		in.Quote.Lines = append(in.Quote.Lines, Line{Kind: KindMaterial, Description: fmt.Sprintf("Item %d %s", i, strings.Repeat("long ", 30)),
			QuantityMilli: 1000, UnitAmount: 100, GSTCode: core.FRE, Amount: 100})
	}
	var buf bytes.Buffer
	if err := WriteQuotePDF(&buf, in); err != nil {
		t.Fatal(err)
	}
	pages, err := property.PDFPageCount(buf.Bytes())
	if err != nil || pages < 2 {
		t.Fatalf("pages = %d, %v; want at least 2", pages, err)
	}
}
