package property

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"shigoto/internal/core"
)

// formatCents formats c as dollars with two decimals and no symbol:
// 62000 -> "620.00", -1005 -> "-10.05".
func formatCents(c core.Cents) string {
	sign := ""
	v := int64(c)
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// WriteStatementCSV writes s as CSV: header rows (organisation, tenants,
// property, period, opening and closing balance, generation time in loc),
// a blank row, then Date, Description, Debit, Credit, Balance per entry.
func WriteStatementCSV(w io.Writer, s Statement, loc *time.Location) error {
	cw := csv.NewWriter(w)
	rows := [][]string{
		{"Organisation", s.OrgName},
		{"Tenant(s)", strings.Join(s.TenantNames, "; ")},
		{"Property", s.PropertyAddress},
		{"Period", s.From.Format("2006-01-02") + " to " + s.To.Format("2006-01-02")},
		{"Opening balance", formatCents(s.Opening)},
		{"Closing balance", formatCents(s.Closing)},
		{"Generated", s.GeneratedAt.In(loc).Format("2006-01-02 15:04")},
		{""},
		{"Date", "Description", "Debit", "Credit", "Balance"},
	}
	for _, e := range s.Entries {
		rows = append(rows, []string{
			e.Date.Format("2006-01-02"), e.Description,
			amountOrBlank(e.Debit), amountOrBlank(e.Credit), formatCents(e.Balance),
		})
	}
	if err := cw.WriteAll(rows); err != nil {
		return err
	}
	return cw.Error()
}

func amountOrBlank(c core.Cents) string {
	if c == 0 {
		return ""
	}
	return formatCents(c)
}

// StatementFilename is "rent-statement-<address slug>-<from>-to-<to>.<ext>",
// with an ASCII-only slug of the property address (accents stripped). An
// address with no ASCII letters or digits falls back to "lease-<id>".
func StatementFilename(s Statement, ext string) string {
	slug := asciiSlug(s.PropertyAddress)
	if slug == "" {
		slug = fmt.Sprintf("lease-%d", s.Lease.ID)
	}
	return fmt.Sprintf("rent-statement-%s-%s-to-%s.%s", slug, s.From.Format("2006-01-02"), s.To.Format("2006-01-02"), ext)
}

func asciiSlug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
