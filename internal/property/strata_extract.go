package property

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"shigoto/internal/core"
	"shigoto/internal/importers"
)

// ExtractedPage is one page's text as the extraction model transcribed it.
type ExtractedPage struct {
	PageNo int
	Text   string
}

// ExtractedMotion is a motion recorded in minutes.
type ExtractedMotion struct {
	MeetingDate time.Time
	MeetingKind string
	MotionNo    string
	Text        string
	Outcome     string
	PageNo      int
}

// ExtractedFigure is one financial figure from a levy notice, budget,
// financial statement or sinking fund forecast.
type ExtractedFigure struct {
	Fund        string
	Metric      string
	Label       string
	PeriodStart *time.Time
	PeriodEnd   time.Time
	AmountCents core.Cents
	PageNo      int
}

// Extraction is everything read from one page batch.
type Extraction struct {
	Pages   []ExtractedPage
	Motions []ExtractedMotion
	Figures []ExtractedFigure
}

var (
	meetingKinds   = []string{"agm", "egm", "committee", "other"}
	motionOutcomes = []string{"carried", "defeated", "withdrawn", "not_put", "unknown"}
	figureFunds    = []string{"admin", "sinking", "other"}
	figureMetrics  = []string{"levy", "budget_income", "budget_expense", "actual_income", "actual_expense", "balance", "arrears", "other"}
)

// extractInstruction is the prompt sent with page batch b of a document of
// type t. It always names the batch as "pages <from>-<to>".
func extractInstruction(t StrataDocType, b PageBatch) string {
	var s strings.Builder
	fmt.Fprintf(&s, `You are reading pages %d-%d of a Queensland body corporate (strata) document of type %q. The attached PDF contains exactly these pages, in order: its first page is page %d.

Reply with one JSON object and nothing else:
{"pages": [{"page_no": <int>, "text": <string>}]`, b.From, b.To, t, b.From)
	switch {
	case t == DocMinutes:
		fmt.Fprintf(&s, `, "motions": [{"meeting_date": "YYYY-MM-DD", "meeting_kind": one of %s, "motion_no": <string, "" if none>, "text": <the motion's wording>, "outcome": one of %s, "page_no": <int>}]`,
			quoteList(meetingKinds), quoteList(motionOutcomes))
	case t.Financial():
		fmt.Fprintf(&s, `, "figures": [{"fund": one of %s, "metric": one of %s, "label": <the line's label as printed>, "period_start": "YYYY-MM-DD" or null, "period_end": "YYYY-MM-DD", "amount": <string exactly as printed, e.g. "1,234.50" or "(1,200.00)">, "page_no": <int>}]`,
			quoteList(figureFunds), quoteList(figureMetrics))
	}
	s.WriteString(`}

Rules:
- "pages" has exactly one entry for every page from ` + fmt.Sprint(b.From) + ` to ` + fmt.Sprint(b.To) + `, numbered as in the original document.
- "text" is the page's full text as markdown. Keep tables as markdown tables with every row and column. Do not summarise, correct or omit anything. A blank page has "".
- Only report what is printed in the document. Never guess a value.`)
	switch {
	case t == DocMinutes:
		s.WriteString("\n- Report every motion put to the meeting, with the page it appears on.")
	case t.Financial():
		s.WriteString("\n- Report fund totals, levies, balances and arrears; amounts are strings exactly as printed, never JSON numbers.")
	}
	return s.String()
}

func quoteList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = `"` + x + `"`
	}
	return strings.Join(q, ", ")
}

type rawExtraction struct {
	Pages []struct {
		PageNo int    `json:"page_no"`
		Text   string `json:"text"`
	} `json:"pages"`
	Motions []struct {
		MeetingDate string `json:"meeting_date"`
		MeetingKind string `json:"meeting_kind"`
		MotionNo    string `json:"motion_no"`
		Text        string `json:"text"`
		Outcome     string `json:"outcome"`
		PageNo      int    `json:"page_no"`
	} `json:"motions"`
	Figures []struct {
		Fund        string          `json:"fund"`
		Metric      string          `json:"metric"`
		Label       string          `json:"label"`
		PeriodStart *string         `json:"period_start"`
		PeriodEnd   string          `json:"period_end"`
		Amount      json.RawMessage `json:"amount"`
		PageNo      int             `json:"page_no"`
	} `json:"figures"`
}

// clean removes NUL characters (which Postgres text rejects) and trims s.
func clean(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "\x00", "")) }

// ParseExtraction validates the extraction model's reply for page batch b
// of a document of type t. Any violation is core.ErrInvalidInput.
func ParseExtraction(raw []byte, t StrataDocType, b PageBatch) (Extraction, error) {
	bad := func(format string, args ...any) (Extraction, error) {
		return Extraction{}, fmt.Errorf("%w: extraction: %s", core.ErrInvalidInput, fmt.Sprintf(format, args...))
	}
	var r rawExtraction
	if err := json.Unmarshal(bytes.TrimSpace(raw), &r); err != nil {
		return bad("reply is not a JSON object: %v", err)
	}
	inBatch := func(p int) bool { return p >= b.From && p <= b.To }

	seen := map[int]bool{}
	var out Extraction
	for _, p := range r.Pages {
		if !inBatch(p.PageNo) {
			return bad("page %d is outside pages %d-%d", p.PageNo, b.From, b.To)
		}
		if seen[p.PageNo] {
			return bad("page %d appears twice", p.PageNo)
		}
		seen[p.PageNo] = true
		out.Pages = append(out.Pages, ExtractedPage{PageNo: p.PageNo, Text: clean(p.Text)})
	}
	for p := b.From; p <= b.To; p++ {
		if !seen[p] {
			return bad("page %d is missing", p)
		}
	}
	slices.SortFunc(out.Pages, func(a, b ExtractedPage) int { return a.PageNo - b.PageNo })

	if len(r.Motions) > 0 && t != DocMinutes {
		return bad("motions are only extracted from minutes")
	}
	for i, m := range r.Motions {
		date, err := time.Parse(time.DateOnly, m.MeetingDate)
		switch {
		case err != nil:
			return bad("motion %d: meeting_date %q is not YYYY-MM-DD", i, m.MeetingDate)
		case !slices.Contains(meetingKinds, m.MeetingKind):
			return bad("motion %d: unknown meeting_kind %q", i, m.MeetingKind)
		case !slices.Contains(motionOutcomes, m.Outcome):
			return bad("motion %d: unknown outcome %q", i, m.Outcome)
		case clean(m.Text) == "":
			return bad("motion %d: motion text is blank", i)
		case !inBatch(m.PageNo):
			return bad("motion %d: page_no %d is outside pages %d-%d", i, m.PageNo, b.From, b.To)
		}
		out.Motions = append(out.Motions, ExtractedMotion{MeetingDate: date, MeetingKind: m.MeetingKind,
			MotionNo: clean(m.MotionNo), Text: clean(m.Text), Outcome: m.Outcome, PageNo: m.PageNo})
	}

	if len(r.Figures) > 0 && !t.Financial() {
		return bad("figures are only extracted from financial documents")
	}
	for i, f := range r.Figures {
		if !slices.Contains(figureFunds, f.Fund) {
			return bad("figure %d: unknown fund %q", i, f.Fund)
		}
		if !slices.Contains(figureMetrics, f.Metric) {
			return bad("figure %d: unknown metric %q", i, f.Metric)
		}
		if clean(f.Label) == "" {
			return bad("figure %d: label is blank", i)
		}
		end, err := time.Parse(time.DateOnly, f.PeriodEnd)
		if err != nil {
			return bad("figure %d: period_end %q is not YYYY-MM-DD", i, f.PeriodEnd)
		}
		var start *time.Time
		if f.PeriodStart != nil {
			s, err := time.Parse(time.DateOnly, *f.PeriodStart)
			if err != nil {
				return bad("figure %d: period_start %q is not YYYY-MM-DD", i, *f.PeriodStart)
			}
			start = &s
		}
		// Amounts must be JSON strings: a JSON number has already been
		// through a float in the model's output and may be rounded.
		var amount string
		if err := json.Unmarshal(f.Amount, &amount); err != nil {
			return bad("figure %d: amount must be a string as printed, got %s", i, string(f.Amount))
		}
		amount = strings.TrimSpace(amount)
		if strings.HasPrefix(amount, "$(") {
			amount = "(" + amount[2:]
		}
		if amount == "" {
			return bad("figure %d: amount is blank", i)
		}
		cents, err := importers.ParseAmountCents(amount)
		if err != nil {
			return bad("figure %d: amount %q is not a money amount", i, amount)
		}
		if !inBatch(f.PageNo) {
			return bad("figure %d: page_no %d is outside pages %d-%d", i, f.PageNo, b.From, b.To)
		}
		out.Figures = append(out.Figures, ExtractedFigure{Fund: f.Fund, Metric: f.Metric, Label: clean(f.Label),
			PeriodStart: start, PeriodEnd: end, AmountCents: cents, PageNo: f.PageNo})
	}
	return out, nil
}

// Chunk is a searchable span of one page's text.
type Chunk struct {
	PageFrom, PageTo int
	Text             string
}

// ChunkPages splits each page into chunks of at most size bytes, each
// overlapping the previous one on the same page by about overlap bytes.
// Splits prefer whitespace; chunks never span pages; blank pages yield
// none.
func ChunkPages(pages []ExtractedPage, size, overlap int) []Chunk {
	var out []Chunk
	for _, p := range pages {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		add := func(s string) {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, Chunk{PageFrom: p.PageNo, PageTo: p.PageNo, Text: s})
			}
		}
		if len(text) <= size {
			add(text)
			continue
		}
		start := 0
		for start < len(text) {
			end := start + size
			if end >= len(text) {
				add(text[start:])
				break
			}
			// Prefer to end just after the last whitespace in the second
			// half of the window.
			if cut := strings.LastIndexFunc(text[start+size/2:end], unicode.IsSpace); cut >= 0 {
				end = start + size/2 + cut + 1
			}
			end = runeFloor(text, end)
			add(text[start:end])
			next := runeFloor(text, end-overlap)
			// Start the overlap at a word boundary when there is one.
			if sp := strings.IndexFunc(text[next:end], unicode.IsSpace); sp >= 0 && next > 0 && !unicode.IsSpace(rune(text[next-1])) {
				next += sp + 1
			}
			if next <= start {
				next = end
			}
			start = next
		}
	}
	return out
}

// runeFloor moves i back to the start of the UTF-8 rune containing it.
func runeFloor(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}
