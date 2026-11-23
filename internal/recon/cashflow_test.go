package recon

import (
	"testing"
	"time"

	"shigoto/internal/testkit/table"
)

func TestCashFlowPeriod(t *testing.T) {
	type testCase struct {
		Name   string `yaml:"name"`
		Today  string `yaml:"today"`
		Month  int    `yaml:"month"`
		Period string `yaml:"period"`
		From   string `yaml:"from"`
		To     string `yaml:"to"`
		Error  bool   `yaml:"error"`
	}
	for _, c := range table.Load[testCase](t, "testdata/cashflow_periods.yaml") {
		t.Run(c.Name, func(t *testing.T) {
			today, err := time.Parse(dateLayout, c.Today)
			if err != nil {
				t.Fatal(err)
			}
			from, to, err := cashFlowPeriod(today, c.Month, c.Period)
			if (err != nil) != c.Error {
				t.Fatalf("error = %v, want error %v", err, c.Error)
			}
			if !c.Error && (from.Format(dateLayout) != c.From || to.Format(dateLayout) != c.To) {
				t.Fatalf("got %s..%s, want %s..%s", from, to, c.From, c.To)
			}
		})
	}
}
