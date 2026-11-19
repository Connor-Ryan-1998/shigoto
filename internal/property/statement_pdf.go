package property

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/text/unicode/norm"
)

// A4 portrait in points.
const (
	pdfPageW   = 595.28
	pdfPageH   = 841.89
	pdfMargin  = 40.0
	pdfRowH    = 16.0
	pdfBottomY = pdfPageH - 60
)

// statementColumns are the table's columns: x offset, width, right-aligned.
var statementColumns = []struct {
	title string
	x, w  float64
	right bool
}{
	{"Date", pdfMargin, 72, false},
	{"Description", pdfMargin + 72, 239, false},
	{"Debit", pdfMargin + 311, 68, true},
	{"Credit", pdfMargin + 379, 68, true},
	{"Balance", pdfMargin + 447, 68, true},
}

type statementPDF struct {
	pdf    *gopdf.GoPdf
	s      Statement
	loc    *time.Location
	page   int
	err    error
	footer string
}

// WriteStatementPDF writes s as an A4 PDF: organisation and statement
// header, the entry table (its header repeated on every page), totals and
// a "Generated … · Page n" footer, with times shown in loc. Glyphs missing
// from the embedded Go fonts are substituted rather than failing.
func WriteStatementPDF(w io.Writer, s Statement, loc *time.Location) error {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	opt := gopdf.TtfOption{OnGlyphNotFoundSubstitute: baseLetter}
	if err := pdf.AddTTFFontDataWithOption("go", goregular.TTF, opt); err != nil {
		return err
	}
	if err := pdf.AddTTFFontDataWithOption("go-bold", gobold.TTF, opt); err != nil {
		return err
	}
	r := &statementPDF{
		pdf: pdf, s: s, loc: loc,
		footer: "Generated " + s.GeneratedAt.In(loc).Format("2 Jan 2006 15:04"),
	}
	r.render()
	if r.err != nil {
		return r.err
	}
	return pdf.Write(w)
}

// baseLetter replaces a glyph the embedded Go fonts lack with its
// unaccented base letter (ặ -> a), so names stay readable; anything with no
// base letter falls back to gopdf's default substitute.
func baseLetter(r rune) rune {
	if d := []rune(norm.NFD.String(string(r))); len(d) > 0 && d[0] != r {
		return d[0]
	}
	return gopdf.DefaultOnGlyphNotFoundSubstitute(r)
}

func (r *statementPDF) render() {
	r.newPage()
	r.font("go-bold", 16)
	r.text(pdfMargin, r.s.OrgName)
	r.nl(20)
	if r.s.OrgABN != "" {
		r.font("go", 9)
		r.text(pdfMargin, "ABN "+r.s.OrgABN)
		r.nl(14)
	}
	r.nl(8)
	r.font("go-bold", 13)
	r.text(pdfMargin, "Rent statement")
	r.nl(20)

	lease := r.s.Lease.StartDate.Format("2 Jan 2006")
	if r.s.Lease.EndDate != nil {
		lease += " – " + r.s.Lease.EndDate.Format("2 Jan 2006")
	} else {
		lease += " onwards"
	}
	for _, kv := range [][2]string{
		{"Tenant(s)", strings.Join(r.s.TenantNames, ", ")},
		{"Property", r.s.PropertyAddress},
		{"Lease", lease + " (" + string(r.s.Lease.Frequency) + ")"},
		{"Period", r.s.From.Format("2 Jan 2006") + " – " + r.s.To.Format("2 Jan 2006")},
		{"Opening balance", "$" + formatCents(r.s.Opening)},
	} {
		r.font("go-bold", 9)
		r.text(pdfMargin, kv[0])
		r.font("go", 9)
		r.fit(pdfMargin+90, pdfPageW-2*pdfMargin-90, kv[1], false)
		r.nl(14)
	}
	r.nl(10)
	r.tableHeader()

	r.font("go", 9)
	if len(r.s.Entries) == 0 {
		r.text(pdfMargin, "No transactions in this period.")
		r.nl(pdfRowH)
	}
	for _, e := range r.s.Entries {
		if r.pdf.GetY()+pdfRowH > pdfBottomY {
			r.newPage()
			r.tableHeader()
			r.font("go", 9)
		}
		cells := []string{e.Date.Format("2 Jan 2006"), e.Description, amountOrBlank(e.Debit), amountOrBlank(e.Credit), formatCents(e.Balance)}
		for i, c := range statementColumns {
			r.fit(c.x, c.w, cells[i], c.right)
		}
		r.nl(pdfRowH)
	}

	if r.pdf.GetY()+3*pdfRowH > pdfBottomY {
		r.newPage()
	}
	r.rule()
	r.font("go-bold", 9)
	r.text(pdfMargin, "Totals")
	r.fit(statementColumns[2].x, statementColumns[2].w, formatCents(r.s.TotalDebits), true)
	r.fit(statementColumns[3].x, statementColumns[3].w, formatCents(r.s.TotalCredits), true)
	r.nl(pdfRowH)
	r.text(pdfMargin, "Closing balance")
	r.fit(statementColumns[4].x, statementColumns[4].w, "$"+formatCents(r.s.Closing), true)
	r.nl(pdfRowH)
	r.font("go", 8)
	r.text(pdfMargin, "Positive balances are owed by the tenant; negative balances are in credit.")
}

func (r *statementPDF) newPage() {
	r.pdf.AddPage()
	r.page++
	r.font("go", 8)
	r.pdf.SetY(pdfPageH - 30)
	r.text(pdfMargin, r.footer)
	r.fit(pdfPageW-pdfMargin-100, 100, fmt.Sprintf("Page %d", r.page), true)
	r.pdf.SetY(pdfMargin)
}

func (r *statementPDF) tableHeader() {
	r.font("go-bold", 9)
	for _, c := range statementColumns {
		r.fit(c.x, c.w, c.title, c.right)
	}
	r.nl(pdfRowH - 4)
	r.rule()
	r.nl(4)
}

func (r *statementPDF) rule() {
	y := r.pdf.GetY()
	r.pdf.SetLineWidth(0.5)
	r.pdf.SetStrokeColor(160, 160, 160)
	r.pdf.Line(pdfMargin, y, pdfPageW-pdfMargin, y)
}

func (r *statementPDF) font(family string, size float64) {
	if r.err == nil {
		r.err = r.pdf.SetFont(family, "", size)
	}
}

func (r *statementPDF) nl(h float64) { r.pdf.SetY(r.pdf.GetY() + h) }

func (r *statementPDF) text(x float64, s string) {
	if r.err != nil {
		return
	}
	r.pdf.SetX(x)
	r.err = r.pdf.Cell(nil, s)
}

// fit writes s in a box of width w at x, truncating with "…" so it never
// overflows into the next column, right-aligned when right is set.
func (r *statementPDF) fit(x, w float64, s string, right bool) {
	if r.err != nil {
		return
	}
	s = r.truncate(s, w)
	align := gopdf.Left | gopdf.Top
	if right {
		align = gopdf.Right | gopdf.Top
	}
	r.pdf.SetX(x)
	y := r.pdf.GetY()
	r.err = r.pdf.CellWithOption(&gopdf.Rect{W: w, H: pdfRowH}, s, gopdf.CellOption{Align: align})
	r.pdf.SetY(y)
}

func (r *statementPDF) truncate(s string, w float64) string {
	width, err := r.pdf.MeasureTextWidth(s)
	if err != nil || width <= w {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if width, err := r.pdf.MeasureTextWidth(candidate); err == nil && width <= w {
			return candidate
		}
	}
	return ""
}
