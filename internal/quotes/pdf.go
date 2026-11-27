package quotes

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // logo formats for image.DecodeConfig
	_ "image/png"
	"io"
	"strconv"
	"strings"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"shigoto/internal/core"
	"shigoto/internal/property"
)

// PDFInput is everything a quote PDF shows. Attachments are deliberately
// absent: they are internal records, never sent to the customer.
type PDFInput struct {
	Quote       Quote
	Org         core.OrgSettings
	Logo        []byte // PNG or JPEG; nil for none
	Customer    core.Contact
	ProjectName string
}

// A4 portrait in points.
const (
	pageW   = 595.28
	pageH   = 841.89
	margin  = 40.0
	rowH    = 16.0
	bottomY = pageH - 60
	logoW   = 160.0
	logoH   = 60.0
)

// Both tables share x positions: Description, quantity, unit, GST, amount.
var tableX = []struct {
	x, w  float64
	right bool
}{
	{margin, 255, false},
	{margin + 255, 50, true},
	{margin + 305, 80, true},
	{margin + 385, 50, true},
	{margin + 435, 80, true},
}

type pdfSection struct {
	Title  string
	Header []string
	Rows   [][]string
}

// pdfModel is the quote's content as text, laid out by WriteQuotePDF.
// Tests assert on it because gopdf's embedded fonts don't leave
// extractable text in the PDF.
type pdfModel struct {
	OrgLines  []string
	Heading   string
	Title     string
	Customer  []string
	Meta      []string
	Sections  []pdfSection
	Totals    [][2]string
	TermsHead string
	Terms     []string
}

func (m pdfModel) text() string {
	var b strings.Builder
	write := func(s ...string) {
		for _, x := range s {
			b.WriteString(x)
			b.WriteByte('\n')
		}
	}
	write(m.OrgLines...)
	write(m.Heading, m.Title)
	write(m.Customer...)
	write(m.Meta...)
	for _, s := range m.Sections {
		write(s.Title, strings.Join(s.Header, " | "))
		for _, r := range s.Rows {
			write(strings.Join(r, " | "))
		}
	}
	for _, t := range m.Totals {
		write(t[0] + " " + t[1])
	}
	if m.TermsHead != "" {
		write(m.TermsHead)
		write(m.Terms...)
	}
	return b.String()
}

// money formats cents as $1,234.56.
func money(c core.Cents) string {
	v := int64(c)
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	whole := strconv.FormatInt(v/100, 10)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	return fmt.Sprintf("%s$%s.%02d", sign, whole, v%100)
}

// quantity formats thousandths without trailing zeros: 2500 -> 2.5.
func quantity(milli int64) string {
	s := strconv.FormatFloat(float64(milli)/1000, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

func gstLabel(c core.GSTCode) string {
	switch c {
	case core.GST:
		return "GST"
	case core.FRE:
		return "Free"
	default:
		return "N-T"
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func buildPDFModel(in PDFInput) pdfModel {
	q, o := in.Quote, in.Org
	m := pdfModel{Heading: "Quote " + q.Label(), Title: q.Title}

	m.OrgLines = append(m.OrgLines, o.Name)
	if o.ABN != "" {
		m.OrgLines = append(m.OrgLines, "ABN "+o.ABN)
	}
	m.OrgLines = append(m.OrgLines, nonEmptyLines(o.Address)...)
	for _, s := range []string{o.Phone, o.Email} {
		if s != "" {
			m.OrgLines = append(m.OrgLines, s)
		}
	}
	for _, l := range o.Licences {
		m.OrgLines = append(m.OrgLines, l.Type+" "+l.Number)
	}

	for _, s := range []string{in.Customer.Name, in.Customer.CompanyName, in.Customer.Email, in.Customer.Phone} {
		if s != "" {
			m.Customer = append(m.Customer, s)
		}
	}
	m.Meta = []string{"Issued " + q.IssueDate.Format("2 Jan 2006"), "Valid until " + q.ValidUntil.Format("2 Jan 2006")}
	if in.ProjectName != "" {
		m.Meta = append(m.Meta, "Project "+in.ProjectName)
	}

	labour := pdfSection{Title: "Labour", Header: []string{"Description", "Hours", "Rate", "GST", "Amount"}}
	materials := pdfSection{Title: "Materials", Header: []string{"Description", "Qty", "Unit price", "GST", "Amount"}}
	for _, l := range q.Lines {
		row := []string{l.Description, quantity(l.QuantityMilli), money(l.UnitAmount), gstLabel(l.GSTCode), money(l.Amount)}
		if l.Kind == KindLabour {
			labour.Rows = append(labour.Rows, row)
		} else {
			materials.Rows = append(materials.Rows, row)
		}
	}
	for _, s := range []pdfSection{labour, materials} {
		if len(s.Rows) > 0 {
			m.Sections = append(m.Sections, s)
		}
	}

	m.Totals = [][2]string{{"Subtotal", money(q.Subtotal)}}
	if q.GSTTotal != 0 {
		m.Totals = append(m.Totals, [2]string{"GST", money(q.GSTTotal)})
	}
	m.Totals = append(m.Totals, [2]string{"Total", money(q.Total)})
	if terms := strings.TrimSpace(q.Terms); terms != "" {
		m.TermsHead = "Terms"
		m.Terms = strings.Split(terms, "\n")
	}
	return m
}

// WriteQuotePDF renders in as an A4 PDF to w.
func WriteQuotePDF(w io.Writer, in PDFInput) error {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	opt := gopdf.TtfOption{OnGlyphNotFoundSubstitute: property.BaseLetter}
	if err := pdf.AddTTFFontDataWithOption("go", goregular.TTF, opt); err != nil {
		return err
	}
	if err := pdf.AddTTFFontDataWithOption("go-bold", gobold.TTF, opt); err != nil {
		return err
	}
	r := &quotePDF{pdf: pdf, footer: "Quote " + in.Quote.Label()}
	r.render(buildPDFModel(in), in.Logo)
	if r.err != nil {
		return r.err
	}
	return pdf.Write(w)
}

type quotePDF struct {
	pdf    *gopdf.GoPdf
	footer string
	page   int
	err    error
}

func (r *quotePDF) render(m pdfModel, logo []byte) {
	r.newPage()
	top := r.pdf.GetY()

	// Org block on the left, logo on the right.
	if len(logo) > 0 {
		r.logo(logo, pageW-margin-logoW, top)
	}
	for i, l := range m.OrgLines {
		if i == 0 {
			r.font("go-bold", 14)
			r.fit(margin, 300, l, false)
			r.nl(20)
			continue
		}
		r.font("go", 9)
		r.fit(margin, 300, l, false)
		r.nl(12)
	}
	r.pdf.SetY(max(r.pdf.GetY(), top+logoH) + 14)

	r.font("go-bold", 16)
	r.fit(margin, pageW-2*margin, m.Heading, false)
	r.nl(20)
	r.font("go", 11)
	r.fit(margin, pageW-2*margin, m.Title, false)
	r.nl(22)

	// Customer on the left, dates on the right.
	y := r.pdf.GetY()
	r.font("go-bold", 9)
	r.text(margin, "Prepared for")
	r.nl(13)
	for _, l := range m.Customer {
		r.font("go", 9)
		r.fit(margin, 280, l, false)
		r.nl(12)
	}
	end := r.pdf.GetY()
	r.pdf.SetY(y)
	for _, l := range m.Meta {
		r.font("go", 9)
		r.fit(pageW-margin-200, 200, l, true)
		r.nl(12)
	}
	r.pdf.SetY(max(end, r.pdf.GetY()) + 14)

	for _, s := range m.Sections {
		r.ensure(3 * rowH)
		r.font("go-bold", 11)
		r.text(margin, s.Title)
		r.nl(16)
		r.tableHeader(s.Header)
		r.font("go", 9)
		for _, row := range s.Rows {
			if r.pdf.GetY()+rowH > bottomY {
				r.newPage()
				r.tableHeader(s.Header)
				r.font("go", 9)
			}
			for i, c := range tableX {
				r.fit(c.x, c.w, row[i], c.right)
			}
			r.nl(rowH)
		}
		r.nl(10)
	}

	r.ensure(float64(len(m.Totals)+1) * rowH)
	r.rule()
	r.nl(4)
	for i, t := range m.Totals {
		family := "go"
		if i == len(m.Totals)-1 {
			family = "go-bold"
		}
		r.font(family, 10)
		r.fit(margin+305, 130, t[0], true)
		r.fit(margin+435, 80, t[1], true)
		r.nl(rowH)
	}

	if m.TermsHead != "" {
		r.nl(10)
		r.ensure(2 * rowH)
		r.font("go-bold", 10)
		r.text(margin, m.TermsHead)
		r.nl(14)
		r.font("go", 9)
		for _, para := range m.Terms {
			for _, l := range r.wrap(para, pageW-2*margin) {
				r.ensure(12)
				r.text(margin, l)
				r.nl(12)
			}
		}
	}
}

// logo draws img scaled to fit logoW x logoH, keeping its aspect ratio. An
// unreadable logo is skipped rather than failing the quote.
func (r *quotePDF) logo(img []byte, x, y float64) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		return
	}
	scale := min(logoW/float64(cfg.Width), logoH/float64(cfg.Height))
	w, h := float64(cfg.Width)*scale, float64(cfg.Height)*scale
	holder, err := gopdf.ImageHolderByBytes(img)
	if err != nil {
		return
	}
	if err := r.pdf.ImageByHolder(holder, x+logoW-w, y, &gopdf.Rect{W: w, H: h}); err != nil {
		return
	}
}

func (r *quotePDF) ensure(h float64) {
	if r.pdf.GetY()+h > bottomY {
		r.newPage()
	}
}

func (r *quotePDF) newPage() {
	r.pdf.AddPage()
	r.page++
	r.font("go", 8)
	r.pdf.SetY(pageH - 30)
	r.text(margin, r.footer)
	r.fit(pageW-margin-100, 100, fmt.Sprintf("Page %d", r.page), true)
	r.pdf.SetY(margin)
}

func (r *quotePDF) tableHeader(titles []string) {
	r.font("go-bold", 9)
	for i, c := range tableX {
		r.fit(c.x, c.w, titles[i], c.right)
	}
	r.nl(rowH - 4)
	r.rule()
	r.nl(4)
}

func (r *quotePDF) rule() {
	y := r.pdf.GetY()
	r.pdf.SetLineWidth(0.5)
	r.pdf.SetStrokeColor(160, 160, 160)
	r.pdf.Line(margin, y, pageW-margin, y)
}

func (r *quotePDF) font(family string, size float64) {
	if r.err == nil {
		r.err = r.pdf.SetFont(family, "", size)
	}
}

func (r *quotePDF) nl(h float64) { r.pdf.SetY(r.pdf.GetY() + h) }

func (r *quotePDF) text(x float64, s string) {
	if r.err != nil {
		return
	}
	r.pdf.SetX(x)
	r.err = r.pdf.Cell(nil, s)
}

// fit writes s in a box of width w at x, truncating with "…" so it never
// overflows into the next column.
func (r *quotePDF) fit(x, w float64, s string, right bool) {
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
	r.err = r.pdf.CellWithOption(&gopdf.Rect{W: w, H: rowH}, s, gopdf.CellOption{Align: align})
	r.pdf.SetY(y)
}

func (r *quotePDF) truncate(s string, w float64) string {
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

// wrap splits s into lines no wider than w at word boundaries.
func (r *quotePDF) wrap(s string, w float64) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		candidate := line + " " + word
		if width, err := r.pdf.MeasureTextWidth(candidate); err == nil && width <= w {
			line = candidate
			continue
		}
		lines = append(lines, line)
		line = word
	}
	return append(lines, r.truncate(line, w))
}
