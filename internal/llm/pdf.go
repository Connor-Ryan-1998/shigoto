package llm

import (
	"encoding/base64"
	"fmt"
)

// PDFParts turns a PDF (typically a few pages split from a larger
// document) into message content for the extraction model. mode is
// config.LLM.PDFMode. "file" sends the PDF itself, keeping its text layer
// so figures are read from real characters. "image" (page rasters) is
// reserved: it would need a PDF renderer in the scratch image.
func PDFParts(mode, filename string, pdf []byte) ([]ContentPart, error) {
	switch mode {
	case "file":
		return []ContentPart{{Type: "file", File: &FilePart{
			Filename: filename,
			DataURL:  "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(pdf),
		}}}, nil
	case "image":
		return nil, fmt.Errorf("llm: PDF mode %q is reserved and not yet supported", mode)
	default:
		return nil, fmt.Errorf("llm: unknown PDF mode %q", mode)
	}
}
