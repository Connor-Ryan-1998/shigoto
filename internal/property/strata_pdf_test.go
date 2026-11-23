package property_test

import (
	"bytes"
	"errors"
	"testing"

	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/testkit"
)

func TestPDFPageCount(t *testing.T) {
	n, err := property.PDFPageCount(testkit.PDFPages(3))
	if err != nil || n != 3 {
		t.Fatalf("PDFPageCount = %d, %v; want 3", n, err)
	}
}

func TestPDFPageCountRejectsNonPDF(t *testing.T) {
	for name, b := range map[string][]byte{
		"text":      []byte("hello, not a pdf"),
		"truncated": testkit.PDFPages(2)[:100],
		"empty":     nil,
	} {
		if _, err := property.PDFPageCount(b); !errors.Is(err, core.ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestPDFPageCountRejectsEncrypted(t *testing.T) {
	enc := testkit.EncryptedPDF(t, testkit.PDFPages(1))
	if _, err := property.PDFPageCount(enc); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestSplitPDF(t *testing.T) {
	batches, err := property.SplitPDF(testkit.PDFPages(12), 5)
	if err != nil {
		t.Fatalf("SplitPDF: %v", err)
	}
	want := [][2]int{{1, 5}, {6, 10}, {11, 12}}
	if len(batches) != len(want) {
		t.Fatalf("got %d batches, want %d", len(batches), len(want))
	}
	for i, b := range batches {
		if b.From != want[i][0] || b.To != want[i][1] {
			t.Errorf("batch %d = %d-%d, want %d-%d", i, b.From, b.To, want[i][0], want[i][1])
		}
		n, err := property.PDFPageCount(b.PDF)
		if err != nil || n != b.To-b.From+1 {
			t.Errorf("batch %d page count = %d, %v; want %d", i, n, err, b.To-b.From+1)
		}
	}
	// The first page of the second batch is the source's page 6.
	if !bytes.Contains(batches[1].PDF, []byte("Page 6")) {
		t.Error("batch 2 does not contain the source's page 6")
	}
}

func TestSplitPDFSmallerThanBatch(t *testing.T) {
	batches, err := property.SplitPDF(testkit.PDFPages(2), 5)
	if err != nil || len(batches) != 1 || batches[0].From != 1 || batches[0].To != 2 {
		t.Fatalf("batches = %+v, %v", batches, err)
	}
}
