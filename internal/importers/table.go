package importers

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Problem is one import problem. Row is the 1-based file line the CSV row
// starts on (the header is line 1), or 0 for a problem with the whole file
// or its mapping. Field
// is the import field key the problem concerns, when there is one.
type Problem struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Field is one field an import reads.
type Field struct {
	Key      string
	Required bool
}

// Mapping maps import field keys to the CSV header names holding them,
// plus the date layout (one of DateFormats) the file's dates use. An empty
// header name leaves the field unmapped.
type Mapping struct {
	Columns    map[string]string `json:"columns"`
	DateFormat string            `json:"date_format"`
}

// Row is one non-blank data row: the 1-based file line it starts on and the
// trimmed cell for each mapped field (absent when the field is unmapped,
// "" when the row is shorter than the header).
type Row struct {
	Num    int
	Values map[string]string
}

// ReadTable reads a CSV whose first row is a header, resolves m against
// that header for fields, and returns the data rows. Header names match
// trimmed and case-insensitively. Blank rows (every cell empty) are
// skipped. A leading UTF-8 BOM is ignored.
//
// When the file or mapping is unusable - not CSV, no header row, an
// unsupported date format, a required field unmapped, a mapping key that
// isn't a field, or a mapped header that is missing or appears more than
// once - it returns nil rows and those problems (all with Row 0).
func ReadTable(r io.Reader, m Mapping, fields []Field) ([]Row, []Problem) {
	cr := csv.NewReader(stripBOM(r))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	// Each record's Row is the file line it starts on (what a spreadsheet
	// or editor shows), not its record index: encoding/csv skips empty
	// lines and a quoted cell may span several.
	var records [][]string
	var lines []int
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, []Problem{{Message: "file is not valid CSV"}}
		}
		line, _ := cr.FieldPos(0)
		records = append(records, rec)
		lines = append(lines, line)
	}
	if len(records) == 0 {
		return nil, []Problem{{Message: "file has no header row"}}
	}

	var problems []Problem
	if !knownDateFormats[m.DateFormat] {
		problems = append(problems, Problem{Field: "date_format", Message: "unsupported date format"})
	}

	headerIndex := map[string][]int{}
	for i, h := range records[0] {
		key := strings.ToLower(strings.TrimSpace(h))
		headerIndex[key] = append(headerIndex[key], i)
	}

	known := map[string]bool{}
	for _, f := range fields {
		known[f.Key] = true
	}
	var unknown []string
	for key := range m.Columns {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		problems = append(problems, Problem{Field: key, Message: "unknown field"})
	}

	columns := map[string]int{}
	for _, f := range fields {
		header := strings.TrimSpace(m.Columns[f.Key])
		if header == "" {
			if f.Required {
				problems = append(problems, Problem{Field: f.Key, Message: fmt.Sprintf("%s must be mapped", f.Key)})
			}
			continue
		}
		idx := headerIndex[strings.ToLower(header)]
		switch len(idx) {
		case 0:
			problems = append(problems, Problem{Field: f.Key, Message: fmt.Sprintf("header %q not found", header)})
		case 1:
			columns[f.Key] = idx[0]
		default:
			problems = append(problems, Problem{Field: f.Key, Message: fmt.Sprintf("header %q appears more than once", header)})
		}
	}
	if len(problems) > 0 {
		return nil, problems
	}

	rows := []Row{}
	for i, rec := range records[1:] {
		if blank(rec) {
			continue
		}
		values := make(map[string]string, len(columns))
		for key, col := range columns {
			if col < len(rec) {
				values[key] = strings.TrimSpace(rec[col])
			} else {
				values[key] = ""
			}
		}
		rows = append(rows, Row{Num: lines[i+1], Values: values})
	}
	return rows, nil
}

func blank(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
