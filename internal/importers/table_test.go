package importers

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"shigoto/internal/testkit/table"
)

type tableCase struct {
	Name    string `yaml:"name"`
	CSV     string `yaml:"csv"`
	Mapping struct {
		Columns    map[string]string `yaml:"columns"`
		DateFormat string            `yaml:"date_format"`
	} `yaml:"mapping"`
	Fields []struct {
		Key      string `yaml:"key"`
		Required bool   `yaml:"required"`
	} `yaml:"fields"`
	Rows []struct {
		Num    int               `yaml:"num"`
		Values map[string]string `yaml:"values"`
	} `yaml:"rows"`
	Problems []Problem `yaml:"problems"`
}

func TestReadTable(t *testing.T) {
	for _, c := range table.Load[tableCase](t, "testdata/table.yaml") {
		t.Run(c.Name, func(t *testing.T) {
			fields := make([]Field, len(c.Fields))
			for i, f := range c.Fields {
				fields[i] = Field{Key: f.Key, Required: f.Required}
			}
			m := Mapping{Columns: c.Mapping.Columns, DateFormat: c.Mapping.DateFormat}
			rows, problems := ReadTable(strings.NewReader(c.CSV), m, fields)

			if len(c.Problems) > 0 || len(problems) > 0 {
				if !reflect.DeepEqual(problems, c.Problems) {
					t.Fatalf("problems = %+v, want %+v", problems, c.Problems)
				}
				if rows != nil {
					t.Fatalf("rows = %+v, want nil with problems", rows)
				}
				return
			}
			want := make([]Row, len(c.Rows))
			for i, r := range c.Rows {
				want[i] = Row{Num: r.Num, Values: r.Values}
			}
			if !reflect.DeepEqual(rows, want) {
				t.Fatalf("rows = %+v, want %+v", rows, want)
			}
		})
	}
}

func TestParseDateEachFormat(t *testing.T) {
	want := time.Date(2025, 3, 7, 0, 0, 0, 0, time.UTC)
	for format, raw := range map[string]string{
		"02/01/2006": "07/03/2025",
		"2006-01-02": "2025-03-07",
		"2 Jan 2006": "7 Mar 2025",
	} {
		got, err := ParseDate(format, raw)
		if err != nil || !got.Equal(want) {
			t.Errorf("ParseDate(%q, %q) = %v, %v; want %v", format, raw, got, err, want)
		}
	}
	if len(DateFormats) != 3 {
		t.Errorf("DateFormats = %v, want the 3 bank formats", DateFormats)
	}
	if _, err := ParseDate("2006-01-02", "07/03/2025"); err == nil {
		t.Error("ParseDate accepted a value in the wrong format")
	}
	if _, err := ParseDate("01/02/2006", "03/07/2025"); err == nil {
		t.Error("ParseDate accepted an unsupported format")
	}
}

func TestParseAmountCentsExported(t *testing.T) {
	got, err := ParseAmountCents("$1,234.56")
	if err != nil || got != 123456 {
		t.Fatalf("ParseAmountCents = %d, %v; want 123456", got, err)
	}
}
