package property

import (
	"bytes"
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"shigoto/internal/core"
)

// strataBatchPages is how many pages the extraction model reads per call,
// small enough that its transcription fits a flash model's output limit.
const strataBatchPages = 5

func init() {
	// pdfcpu would otherwise create a config directory under the user's
	// config dir on first use; the scratch image has none.
	model.ConfigPath = "disable"
}

func pdfConfig() *model.Configuration {
	conf := model.NewStatelessConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return conf
}

// readPDF parses pdf, mapping any parse failure (not a PDF, damaged,
// encrypted) to core.ErrInvalidInput.
func readPDF(pdf []byte) (ctx *model.Context, err error) {
	defer func() {
		// pdfcpu can panic on hostile input; treat that as unreadable.
		if r := recover(); r != nil {
			ctx, err = nil, fmt.Errorf("%w: the PDF could not be read", core.ErrInvalidInput)
		}
	}()
	if len(pdf) == 0 {
		return nil, fmt.Errorf("%w: the PDF is empty", core.ErrInvalidInput)
	}
	ctx, err = api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(pdf), pdfConfig(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: the PDF could not be read (it may be damaged or password protected)", core.ErrInvalidInput)
	}
	if ctx.Encrypt != nil {
		return nil, fmt.Errorf("%w: password-protected PDFs are not supported", core.ErrInvalidInput)
	}
	return ctx, nil
}

// PDFPageCount returns pdf's page count, or core.ErrInvalidInput when it
// is not a readable, unencrypted PDF.
func PDFPageCount(pdf []byte) (int, error) {
	ctx, err := readPDF(pdf)
	if err != nil {
		return 0, err
	}
	return ctx.PageCount, nil
}

// PageBatch is pages From..To (1-based, inclusive) of a document as a PDF
// of their own.
type PageBatch struct {
	From, To int
	PDF      []byte
}

// SplitPDF splits pdf into consecutive batches of at most size pages.
func SplitPDF(pdf []byte, size int) ([]PageBatch, error) {
	if size < 1 {
		return nil, fmt.Errorf("SplitPDF: size %d < 1", size)
	}
	src, err := readPDF(pdf)
	if err != nil {
		return nil, err
	}
	var batches []PageBatch
	for from := 1; from <= src.PageCount; from += size {
		to := min(from+size-1, src.PageCount)
		pages := make([]int, 0, to-from+1)
		for p := from; p <= to; p++ {
			pages = append(pages, p)
		}
		b, err := extractPages(src, pages)
		if err != nil {
			return nil, fmt.Errorf("splitting pages %d-%d: %w", from, to, err)
		}
		batches = append(batches, PageBatch{From: from, To: to, PDF: b})
	}
	return batches, nil
}

func extractPages(src *model.Context, pages []int) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("%w: the PDF could not be split", core.ErrInvalidInput)
		}
	}()
	dest, err := pdfcpu.ExtractPages(context.Background(), src, pages, false)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := api.Write(context.Background(), dest, &buf, pdfConfig()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
