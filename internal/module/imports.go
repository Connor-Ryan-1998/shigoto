package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shigoto/internal/core"
	"shigoto/internal/importers"
)

// MaxImportBytes caps a CSV import upload (5 MiB, as for bank imports).
const MaxImportBytes = 5 << 20

// ImportRunner checks an import's mapped rows against q and, when apply is
// set and there are no problems, writes them. It returns the response body
// and every problem, both from parsing rows and from the database, in row
// order. It must not write when it reports problems.
type ImportRunner func(ctx context.Context, q core.Querier, rows []importers.Row, dateFormat string, apply bool) (body any, problems []importers.Problem, err error)

// importProblemsResponse is the body when an import can't proceed: whole-
// file problems on a preview (200) or any problems on apply (422).
type importProblemsResponse struct {
	Error    string              `json:"error,omitempty"`
	New      int                 `json:"new"`
	Existing int                 `json:"existing"`
	Problems []importers.Problem `json:"problems"`
}

// ServeImport handles one CSV import request: multipart "file" plus
// "mapping" (JSON importers.Mapping), capped at MaxImportBytes. A preview
// (apply false) runs on the pool and always answers 200 with problems
// listed. An apply runs in one transaction holding a per-org advisory
// lock, so two imports can't interleave; any problem rolls back and
// answers 422 {"error", "problems"}.
func ServeImport(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, orgID int64, fields []importers.Field, apply bool, run ImportRunner) {
	data, mapping, ok := readImportUpload(w, r)
	if !ok {
		return
	}

	rows, fileProblems := importers.ReadTable(bytes.NewReader(data), mapping, fields)
	if len(fileProblems) > 0 {
		writeImportProblems(w, apply, fileProblems)
		return
	}

	var body any
	var problems []importers.Problem
	ctx := r.Context()
	if !apply {
		var err error
		body, problems, err = run(ctx, pool, rows, mapping.DateFormat, false)
		if err != nil {
			WriteCoreError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, body)
		return
	}

	errProblems := errors.New("import has problems")
	err := RunInTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('import:' || $1::bigint::text, 0))`, orgID); err != nil {
			return err
		}
		var err error
		body, problems, err = run(ctx, tx, rows, mapping.DateFormat, true)
		if err != nil {
			return err
		}
		if len(problems) > 0 {
			return errProblems
		}
		return nil
	})
	switch {
	case errors.Is(err, errProblems):
		writeImportProblems(w, true, problems)
	case err != nil:
		WriteCoreError(w, err)
	default:
		WriteJSON(w, http.StatusOK, body)
	}
}

func writeImportProblems(w http.ResponseWriter, apply bool, problems []importers.Problem) {
	if !apply {
		WriteJSON(w, http.StatusOK, importProblemsResponse{Problems: problems})
		return
	}
	WriteJSON(w, http.StatusUnprocessableEntity, importProblemsResponse{
		Error: "import has problems; preview again", Problems: problems,
	})
}

// readImportUpload reads the capped multipart body, writing the error
// response itself and returning ok false when it is unusable.
func readImportUpload(w http.ResponseWriter, r *http.Request) ([]byte, importers.Mapping, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxImportBytes)
	if err := r.ParseMultipartForm(MaxImportBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteError(w, http.StatusRequestEntityTooLarge, "file too large")
			return nil, importers.Mapping{}, false
		}
		WriteError(w, http.StatusBadRequest, "invalid multipart body")
		return nil, importers.Mapping{}, false
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "file is required")
		return nil, importers.Mapping{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid multipart body")
		return nil, importers.Mapping{}, false
	}

	raw := r.FormValue("mapping")
	if raw == "" {
		WriteError(w, http.StatusUnprocessableEntity, "mapping is required")
		return nil, importers.Mapping{}, false
	}
	var m importers.Mapping
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "invalid mapping")
		return nil, importers.Mapping{}, false
	}
	return data, m, true
}

// MergeProblems returns a and b combined in row order (stable, so each
// row's problems keep their order, a's before b's).
func MergeProblems(a, b []importers.Problem) []importers.Problem {
	out := append(append([]importers.Problem{}, a...), b...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Row < out[j].Row })
	return out
}
