package timesheets

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"shigoto/internal/importers"
	"shigoto/internal/testkit/table"
)

type entryRowsCase struct {
	Name string `yaml:"name"`
	Rows []struct {
		Num    int               `yaml:"num"`
		Values map[string]string `yaml:"values"`
	} `yaml:"rows"`
	Want []struct {
		Row     int    `yaml:"row"`
		Date    string `yaml:"date"`
		Minutes int    `yaml:"minutes"`
		Project string `yaml:"project"`
		Task    string `yaml:"task"`
		Person  string `yaml:"person"`
		Invoice string `yaml:"invoice"`
		Note    string `yaml:"note"`
	} `yaml:"want"`
	Problems []importers.Problem `yaml:"problems"`
}

var importToday = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

func toRows(in []struct {
	Num    int               `yaml:"num"`
	Values map[string]string `yaml:"values"`
}) []importers.Row {
	out := make([]importers.Row, len(in))
	for i, r := range in {
		if r.Values["note"] == "LONGNOTE" {
			r.Values["note"] = strings.Repeat("é", 1001)
		}
		out[i] = importers.Row{Num: r.Num, Values: r.Values}
	}
	return out
}

func TestParseEntryRows(t *testing.T) {
	for _, c := range table.Load[entryRowsCase](t, "testdata/entry_rows.yaml") {
		t.Run(c.Name, func(t *testing.T) {
			got, problems := ParseEntryRows(toRows(c.Rows), "2006-01-02", importToday)
			if c.Problems == nil {
				c.Problems = []importers.Problem{}
			}
			if !reflect.DeepEqual(problems, c.Problems) {
				t.Fatalf("problems = %+v\nwant %+v", problems, c.Problems)
			}
			if len(got) != len(c.Want) {
				t.Fatalf("rows = %+v, want %d", got, len(c.Want))
			}
			for i, w := range c.Want {
				g := got[i]
				if g.Row != w.Row || g.WorkDate.Format("2006-01-02") != w.Date || g.Minutes != w.Minutes ||
					g.Project != w.Project || g.Task != w.Task || g.Person != w.Person || g.Invoice != w.Invoice || g.Note != w.Note {
					t.Errorf("row %d = %+v, want %+v", i, g, w)
				}
				if !strings.HasPrefix(g.Ref, "import:") || len(g.Ref) != len("import:")+64 {
					t.Errorf("ref = %q, want import:<sha256 hex>", g.Ref)
				}
			}
		})
	}
}

func TestEntryRefsStableAndDistinct(t *testing.T) {
	row := func(num int) importers.Row {
		return importers.Row{Num: num, Values: map[string]string{
			"date": "2025-03-03", "hours": "1", "project": "P", "task": "T", "person": "a@b.c", "invoice": "I", "note": "n",
		}}
	}
	first, _ := ParseEntryRows([]importers.Row{row(2), row(3)}, "2006-01-02", importToday)
	again, _ := ParseEntryRows([]importers.Row{row(5), row(9)}, "2006-01-02", importToday)
	if first[0].Ref == first[1].Ref {
		t.Error("two identical rows in one file share a ref")
	}
	if first[0].Ref != again[0].Ref || first[1].Ref != again[1].Ref {
		t.Error("refs depend on row position, want content plus occurrence only")
	}
	lower := row(2)
	lower.Values["project"], lower.Values["person"] = "p", "A@B.C"
	cased, _ := ParseEntryRows([]importers.Row{lower}, "2006-01-02", importToday)
	if cased[0].Ref != first[0].Ref {
		t.Error("ref changes with project/person case")
	}
}
