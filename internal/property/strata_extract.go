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

// Extraction is everything read from one page batch. Dropped counts
// motions and figures discarded as unusable.
type Extraction struct {
	Pages   []ExtractedPage
	Motions []ExtractedMotion
	Figures []ExtractedFigure
	Dropped int
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
		s.WriteString("\n- Report fund totals, levies, balances and arrears; amounts are strings exactly as printed (keep any CR/DR suffix; a dash means nil), never JSON numbers.")
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

	// A motion or figure the model got wrong is dropped, not fatal: the
	// page text (which search and citations rely on) is still good.
	for _, m := range r.Motions {
		date, err := time.Parse(time.DateOnly, m.MeetingDate)
		if t != DocMinutes || err != nil || !slices.Contains(meetingKinds, m.MeetingKind) ||
			!slices.Contains(motionOutcomes, m.Outcome) || clean(m.Text) == "" || !inBatch(m.PageNo) {
			out.Dropped++
			continue
		}
		out.Motions = append(out.Motions, ExtractedMotion{MeetingDate: date, MeetingKind: m.MeetingKind,
			MotionNo: clean(m.MotionNo), Text: clean(m.Text), Outcome: m.Outcome, PageNo: m.PageNo})
	}
	for _, f := range r.Figures {
		fig, ok := parseFigure(f.Fund, f.Metric, f.Label, f.PeriodStart, f.PeriodEnd, f.Amount, f.PageNo)
		if !t.Financial() || !ok || !inBatch(f.PageNo) {
			out.Dropped++
			continue
		}
		out.Figures = append(out.Figures, fig)
	}
	return out, nil
}

func parseFigure(fund, metric, label string, periodStart *string, periodEnd string, rawAmount json.RawMessage, page int) (ExtractedFigure, bool) {
	if !slices.Contains(figureFunds, fund) || !slices.Contains(figureMetrics, metric) || clean(label) == "" {
		return ExtractedFigure{}, false
	}
	end, err := time.Parse(time.DateOnly, periodEnd)
	if err != nil {
		return ExtractedFigure{}, false
	}
	var start *time.Time
	if periodStart != nil {
		s, err := time.Parse(time.DateOnly, *periodStart)
		if err != nil {
			return ExtractedFigure{}, false
		}
		start = &s
	}
	// Amounts must be JSON strings: a JSON number has already been through
	// a float in the model's output and may be rounded.
	var amount string
	if err := json.Unmarshal(rawAmount, &amount); err != nil {
		return ExtractedFigure{}, false
	}
	cents, ok := parseStrataAmount(amount)
	if !ok {
		return ExtractedFigure{}, false
	}
	return ExtractedFigure{Fund: fund, Metric: metric, Label: clean(label), PeriodStart: start, PeriodEnd: end,
		AmountCents: cents, PageNo: page}, true
}

// parseStrataAmount reads an amount as printed in a StrataMax statement:
// "1,234.50", "(1,200.00)", "$(1,200.00)", a dash or "nil" for zero, and a
// CR (credit, negative) or DR (debit, positive) suffix.
func parseStrataAmount(raw string) (core.Cents, bool) {
	s := clean(raw)
	switch strings.ToLower(s) {
	case "-", "–", "—", "nil":
		return 0, true
	case "":
		return 0, false
	}
	neg := false
	upper := strings.ToUpper(s)
	switch {
	case strings.HasSuffix(upper, "CR"):
		neg, s = true, strings.TrimSpace(s[:len(s)-2])
	case strings.HasSuffix(upper, "DR"):
		s = strings.TrimSpace(s[:len(s)-2])
	}
	if strings.HasPrefix(s, "$(") {
		s = "(" + s[2:]
	}
	if s == "" {
		return 0, false
	}
	cents, err := importers.ParseAmountCents(s)
	if err != nil {
		return 0, false
	}
	if neg {
		cents = -cents
	}
	return cents, true
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
