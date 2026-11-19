package property

import (
	"bytes"
	"encoding/csv"
	"regexp"
	"strings"
	"testing"
	"time"

	"shigoto/internal/core"
)

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func sampleStatement(n int) Statement {
	s := Statement{
		Lease:           Lease{ID: 7, StartDate: date("2026-07-01"), Frequency: Weekly},
		PropertyAddress: "12 Banksia St, Fitzroy",
		TenantNames:     []string{"Tina Tenant", "Co, Tenant"},
		OrgName:         "Ryan Properties",
		OrgABN:          "51 824 753 556",
		From:            date("2026-07-01"),
		To:              date("2027-06-30"),
		Opening:         0,
		GeneratedAt:     time.Date(2026, 11, 19, 9, 30, 0, 0, time.UTC),
	}
	var bal core.Cents
	for i := range n {
		d := s.From.AddDate(0, 0, i)
		if i%2 == 0 {
			bal += 62000
			s.Entries = append(s.Entries, AccountEntry{Date: d, Kind: EntryCharge, Description: "Rent " + DateRangeLabel(d, d.AddDate(0, 0, 6)), Debit: 62000, Balance: bal})
			s.TotalDebits += 62000
		} else {
			bal -= 62000
			s.Entries = append(s.Entries, AccountEntry{Date: d, Kind: EntryPayment, Description: "Payment · Bank transfer", Credit: 62000, Balance: bal})
			s.TotalCredits += 62000
		}
	}
	s.Closing = bal
	return s
}

func TestStatementCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteStatementCSV(&buf, sampleStatement(2), time.UTC); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "09:30\n\nDate,") && !strings.Contains(buf.String(), "09:30\n\"\"\nDate,") {
		t.Errorf("want a blank row between header and table: %q", buf.String())
	}
	r := csv.NewReader(strings.NewReader(buf.String()))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"Organisation", "Ryan Properties"},
		{"Tenant(s)", "Tina Tenant; Co, Tenant"},
		{"Property", "12 Banksia St, Fitzroy"},
		{"Period", "2026-07-01 to 2027-06-30"},
		{"Opening balance", "0.00"},
		{"Closing balance", "0.00"},
		{"Generated", "2026-11-19 09:30"},
		{"Date", "Description", "Debit", "Credit", "Balance"},
		{"2026-07-01", "Rent 1–7 Jul 2026", "620.00", "", "620.00"},
		{"2026-07-02", "Payment · Bank transfer", "", "620.00", "0.00"},
	}
	if len(recs) != len(want) {
		t.Fatalf("records = %q", recs)
	}
	for i := range want {
		if strings.Join(recs[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d = %q, want %q", i, recs[i], want[i])
		}
	}
}

func TestFormatCentsNegative(t *testing.T) {
	if got := formatCents(-1005); got != "-10.05" {
		t.Fatalf("formatCents(-1005) = %q", got)
	}
}

func TestStatementPDF(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteStatementPDF(&buf, sampleStatement(6), time.UTC); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", buf.Bytes()[:16])
	}
}

func TestStatementPDFEmptyRange(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteStatementPDF(&buf, sampleStatement(0), time.UTC); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
}

func TestStatementPDFUnicode(t *testing.T) {
	s := sampleStatement(2)
	s.TenantNames = []string{"Zoë Müller", "Đặng Thị Hồng", "王小明"}
	s.PropertyAddress = "3/14 Côte d’Azur Rd — Unit ½"
	var buf bytes.Buffer
	if err := WriteStatementPDF(&buf, s, time.UTC); err != nil {
		t.Fatal(err)
	}
}

func TestStatementPDFManyPages(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteStatementPDF(&buf, sampleStatement(400), time.UTC); err != nil {
		t.Fatal(err)
	}
	pages := len(regexp.MustCompile(`/Type\s*/Page[^s]`).FindAll(buf.Bytes(), -1))
	if pages < 2 {
		t.Fatalf("pages = %d, want several", pages)
	}
}

func TestStatementFilename(t *testing.T) {
	s := sampleStatement(0)
	s.PropertyAddress = "12 Banksia St, Fitzroy — Côte"
	got := StatementFilename(s, "pdf")
	if got != "rent-statement-12-banksia-st-fitzroy-cote-2026-07-01-to-2027-06-30.pdf" {
		t.Fatalf("filename = %q", got)
	}
	s.PropertyAddress = "王小明"
	if got := StatementFilename(s, "csv"); got != "rent-statement-lease-7-2026-07-01-to-2027-06-30.csv" {
		t.Fatalf("filename = %q", got)
	}
}

func TestBaseLetter(t *testing.T) {
	if got := baseLetter('ặ'); got != 'a' {
		t.Fatalf("baseLetter('ặ') = %q, want 'a'", got)
	}
	if got := baseLetter('王'); got == '王' {
		t.Fatalf("baseLetter('王') returned the missing glyph")
	}
}
