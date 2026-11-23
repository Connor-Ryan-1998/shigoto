package property

import (
	"fmt"
	"testing"

	"shigoto/internal/testkit/table"
)

type citationCase struct {
	Name string     `yaml:"name"`
	Seen [][2]int64 `yaml:"seen"`
	MD   string     `yaml:"md"`
	Want struct {
		MD        string     `yaml:"md"`
		Citations [][2]int64 `yaml:"citations"`
		Grounded  bool       `yaml:"grounded"`
	} `yaml:"want"`
}

func TestResolveCitationsTable(t *testing.T) {
	const path = "testdata/strata_citations.yaml"
	for _, c := range table.Load[citationCase](t, path) {
		t.Run(c.Name, func(t *testing.T) {
			seen := map[[2]int64]Citation{}
			for _, s := range c.Seen {
				seen[s] = Citation{DocumentID: s[0], PageNo: int(s[1]), Title: fmt.Sprintf("Doc %d", s[0])}
			}
			md, cites := resolveCitations(c.MD, seen)
			if md != c.Want.MD {
				t.Errorf("md = %q, want %q", md, c.Want.MD)
			}
			if len(cites) != len(c.Want.Citations) {
				t.Fatalf("citations = %+v, want %v", cites, c.Want.Citations)
			}
			for i, w := range c.Want.Citations {
				if cites[i].DocumentID != w[0] || int64(cites[i].PageNo) != w[1] || cites[i].Title != fmt.Sprintf("Doc %d", w[0]) {
					t.Errorf("citation %d = %+v, want %v", i, cites[i], w)
				}
			}
			if (len(cites) > 0) != c.Want.Grounded {
				t.Errorf("grounded = %v, want %v", len(cites) > 0, c.Want.Grounded)
			}
		})
	}
}
