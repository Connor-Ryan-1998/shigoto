package property

import (
	"errors"
	"strings"
	"testing"
	"time"

	"shigoto/internal/core"
	"shigoto/internal/testkit/table"
)

type extractCase struct {
	Name  string `yaml:"name"`
	Type  string `yaml:"type"`
	Batch [2]int `yaml:"batch"`
	Raw   string `yaml:"raw"`
	Want  *struct {
		Pages   []int    `yaml:"pages"`
		Dropped int      `yaml:"dropped"`
		Texts   []string `yaml:"texts"`
		Motions []struct {
			MotionNo    string `yaml:"motion_no"`
			Outcome     string `yaml:"outcome"`
			PageNo      int    `yaml:"page_no"`
			MeetingDate string `yaml:"meeting_date"`
			MeetingKind string `yaml:"meeting_kind"`
		} `yaml:"motions"`
		Figures []struct {
			Label       string `yaml:"label"`
			AmountCents int64  `yaml:"amount_cents"`
			PeriodStart string `yaml:"period_start"`
			PeriodEnd   string `yaml:"period_end"`
			PageNo      int    `yaml:"page_no"`
		} `yaml:"figures"`
	} `yaml:"want"`
	Err string `yaml:"err"`
}

func dateString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

func TestParseExtractionTable(t *testing.T) {
	const path = "testdata/strata_extract.yaml"
	for _, c := range table.Load[extractCase](t, path) {
		t.Run(c.Name, func(t *testing.T) {
			if (c.Want == nil) == (c.Err == "") {
				t.Fatalf("%s: %q: needs exactly one of want or err", path, c.Name)
			}
			got, err := ParseExtraction([]byte(c.Raw), StrataDocType(c.Type), PageBatch{From: c.Batch[0], To: c.Batch[1]})
			if c.Err != "" {
				if err == nil || !strings.Contains(err.Error(), c.Err) || !errors.Is(err, core.ErrInvalidInput) {
					t.Fatalf("err = %v, want ErrInvalidInput mentioning %q", err, c.Err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Dropped != c.Want.Dropped {
				t.Errorf("dropped = %d, want %d", got.Dropped, c.Want.Dropped)
			}
			if len(got.Pages) != len(c.Want.Pages) {
				t.Fatalf("pages = %+v, want %v", got.Pages, c.Want.Pages)
			}
			for i, p := range got.Pages {
				if p.PageNo != c.Want.Pages[i] {
					t.Errorf("page %d = %d, want %d", i, p.PageNo, c.Want.Pages[i])
				}
				if c.Want.Texts != nil && p.Text != c.Want.Texts[i] {
					t.Errorf("page %d text = %q, want %q", i, p.Text, c.Want.Texts[i])
				}
			}
			if len(got.Motions) != len(c.Want.Motions) {
				t.Fatalf("motions = %+v", got.Motions)
			}
			for i, w := range c.Want.Motions {
				m := got.Motions[i]
				if m.MotionNo != w.MotionNo || m.Outcome != w.Outcome || m.PageNo != w.PageNo ||
					m.MeetingDate.Format(time.DateOnly) != w.MeetingDate || m.MeetingKind != w.MeetingKind {
					t.Errorf("motion %d = %+v, want %+v", i, m, w)
				}
			}
			if len(got.Figures) != len(c.Want.Figures) {
				t.Fatalf("figures = %+v", got.Figures)
			}
			for i, w := range c.Want.Figures {
				f := got.Figures[i]
				if f.Label != w.Label || int64(f.AmountCents) != w.AmountCents || dateString(f.PeriodStart) != w.PeriodStart ||
					f.PeriodEnd.Format(time.DateOnly) != w.PeriodEnd || f.PageNo != w.PageNo {
					t.Errorf("figure %d = %+v, want %+v", i, f, w)
				}
			}
		})
	}
}

func TestExtractInstructionNamesBatch(t *testing.T) {
	got := extractInstruction(DocMinutes, PageBatch{From: 6, To: 10})
	if !strings.Contains(got, "pages 6-10") || !strings.Contains(got, "motions") {
		t.Fatalf("instruction = %q", got)
	}
	if got := extractInstruction(DocBudget, PageBatch{From: 1, To: 5}); !strings.Contains(got, "figures") || strings.Contains(got, `"motions"`) {
		t.Fatalf("budget instruction = %q", got)
	}
}

type chunkCase struct {
	Name  string `yaml:"name"`
	Pages []struct {
		PageNo int    `yaml:"page_no"`
		Text   string `yaml:"text"`
		Word   string `yaml:"word"`
		Times  int    `yaml:"times"`
	} `yaml:"pages"`
	Want []struct {
		PageFrom int `yaml:"page_from"`
		PageTo   int `yaml:"page_to"`
		Len      int `yaml:"len"`
	} `yaml:"want"`
}

func TestChunkPagesTable(t *testing.T) {
	const path = "testdata/strata_chunk.yaml"
	for _, c := range table.Load[chunkCase](t, path) {
		t.Run(c.Name, func(t *testing.T) {
			var pages []ExtractedPage
			for _, p := range c.Pages {
				text := p.Text
				if p.Word != "" {
					text = strings.Repeat(p.Word, p.Times)
				}
				pages = append(pages, ExtractedPage{PageNo: p.PageNo, Text: text})
			}
			got := ChunkPages(pages, 1200, 200)
			if len(got) != len(c.Want) {
				t.Fatalf("got %d chunks %+v, want %d", len(got), got, len(c.Want))
			}
			for i, w := range c.Want {
				g := got[i]
				if g.PageFrom != w.PageFrom || g.PageTo != w.PageTo || len(g.Text) != w.Len {
					t.Errorf("chunk %d = pages %d-%d len %d, want %d-%d len %d", i, g.PageFrom, g.PageTo, len(g.Text), w.PageFrom, w.PageTo, w.Len)
				}
				if len(g.Text) > 1200 {
					t.Errorf("chunk %d is %d bytes, over 1200", i, len(g.Text))
				}
				if i > 0 && got[i-1].PageFrom == g.PageFrom {
					prev := got[i-1].Text
					tail := prev[max(0, len(prev)-50):]
					if !strings.Contains(g.Text[:min(len(g.Text), 400)], strings.TrimSpace(tail)) {
						t.Errorf("chunk %d does not overlap chunk %d", i, i-1)
					}
				}
			}
		})
	}
}
