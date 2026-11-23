package llm_test

import (
	"context"
	"os"
	"testing"

	"shigoto/internal/config"
	"shigoto/internal/llm"
)

// TestLiveProvider calls a real provider. It runs only when
// SHIGOTO_TEST_LLM_BASE_URL, _API_KEY and _MODEL are set; embeddings are
// checked too when SHIGOTO_TEST_EMBED_BASE_URL, _API_KEY and _MODEL are set.
func TestLiveProvider(t *testing.T) {
	base, key, model := os.Getenv("SHIGOTO_TEST_LLM_BASE_URL"), os.Getenv("SHIGOTO_TEST_LLM_API_KEY"), os.Getenv("SHIGOTO_TEST_LLM_MODEL")
	if base == "" || key == "" || model == "" {
		t.Skip("SHIGOTO_TEST_LLM_* not set")
	}
	var ec *config.Embed
	if eb, ek, em := os.Getenv("SHIGOTO_TEST_EMBED_BASE_URL"), os.Getenv("SHIGOTO_TEST_EMBED_API_KEY"), os.Getenv("SHIGOTO_TEST_EMBED_MODEL"); eb != "" && ek != "" && em != "" {
		ec = &config.Embed{BaseURL: eb, APIKey: ek, Model: em}
	}
	c := llm.New(config.LLM{BaseURL: base, APIKey: key, Model: model, ExtractModel: model, PDFMode: "file"}, ec, nil)

	resp, err := c.Chat(context.Background(), llm.ChatRequest{Model: model, Messages: []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentPart{{Type: "text", Text: "Reply with the single word OK."}}},
	}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("chat returned empty content")
	}

	if ec == nil {
		return
	}
	vecs, err := c.Embed(context.Background(), []string{"sinking fund balance"}, llm.Query)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != llm.Dimensions {
		t.Fatalf("embed returned %d vectors", len(vecs))
	}
}
