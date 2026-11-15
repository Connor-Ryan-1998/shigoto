package billing

import (
	"reflect"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/importers"
	"shigoto/internal/testkit/table"
)

type invoiceRowWant struct {
	Row         int    `yaml:"row"`
	Number      string `yaml:"number"`
	Customer    string `yaml:"customer"`
	IssueDate   string `yaml:"issue_date"`
	DueDate     string `yaml:"due_date"`
	PaidDate    string `yaml:"paid_date"`
	Subtotal    int64  `yaml:"subtotal"`
	GST         int64  `yaml:"gst"`
	Total       int64  `yaml:"total"`
	Outstanding int64  `yaml:"outstanding"`
}

type invoiceRowsCase struct {
	Name string `yaml:"name"`
	Rows []struct {
		Num    int               `yaml:"num"`
		Values map[string]string `yaml:"values"`
	} `yaml:"rows"`
	Want     []invoiceRowWant    `yaml:"want"`
	Problems []importers.Problem `yaml:"problems"`
}

func TestParseInvoiceRows(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("bad test date %q", s)
		}
		return d
	}
	for _, c := range table.Load[invoiceRowsCase](t, "testdata/invoice_rows.yaml") {
		t.Run(c.Name, func(t *testing.T) {
			in := make([]importers.Row, len(c.Rows))
			for i, r := range c.Rows {
				in[i] = importers.Row{Num: r.Num, Values: r.Values}
			}
			got, problems := ParseInvoiceRows(in, "2006-01-02", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))

			want := []InvoiceRow{}
			for _, w := range c.Want {
				r := InvoiceRow{
					Row: w.Row, Number: w.Number, Customer: w.Customer,
					IssueDate: day(w.IssueDate), DueDate: day(w.DueDate),
					Subtotal: core.Cents(w.Subtotal), GST: core.Cents(w.GST), Total: core.Cents(w.Total), Outstanding: core.Cents(w.Outstanding),
				}
				if w.PaidDate != "" {
					p := day(w.PaidDate)
					r.PaidDate = &p
				}
				want = append(want, r)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("rows = %+v\nwant %+v", got, want)
			}
			if c.Problems == nil {
				c.Problems = []importers.Problem{}
			}
			if !reflect.DeepEqual(problems, c.Problems) {
				t.Errorf("problems = %+v\nwant %+v", problems, c.Problems)
			}
		})
	}
}
