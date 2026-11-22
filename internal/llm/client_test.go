package llm_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"shigoto/internal/llm"
	"shigoto/internal/llm/llmtest"
)

func newClient(t *testing.T, embedModel string) (*llm.Client, *llmtest.Fake) {
	t.Helper()
	f := llmtest.New(t)
	var ec = f.EmbedConfig(embedModel)
	if embedModel == "" {
		ec = nil
	}
	return llm.New(f.LLMConfig(), ec, http.DefaultClient), f
}

func userText(s string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: []llm.ContentPart{{Type: "text", Text: s}}}
}

func TestChatSendsToolsAndParsesToolCalls(t *testing.T) {
	c, f := newClient(t, "")
	f.QueueChat(llmtest.ChatScript{ToolCalls: []llmtest.ScriptCall{{ID: "c1", Name: "find_motions", Args: `{"query":"roof"}`}}, PromptTokens: 10, CompletionTokens: 3})

	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Model:    c.Model(),
		Messages: []llm.Message{{Role: llm.RoleSystem, Content: []llm.ContentPart{{Type: "text", Text: "sys"}}}, userText("roof?")},
		Tools:    []llm.Tool{{Name: "find_motions", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "c1" || resp.ToolCalls[0].Name != "find_motions" || string(resp.ToolCalls[0].Arguments) != `{"query":"roof"}` {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 3 {
		t.Fatalf("usage = %+v", resp.Usage)
	}

	body := f.Requests("/chat/completions")[0].JSON(t)
	if body["model"] != "fake-chat" {
		t.Fatalf("model = %v", body["model"])
	}
	tools := body["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["function"].(map[string]any)["name"] != "find_motions" {
		t.Fatalf("tools = %v", tools)
	}
	if _, ok := body["response_format"]; ok {
		t.Fatal("response_format sent without JSONObject")
	}
}

func TestChatRoundTripsToolMessages(t *testing.T) {
	c, f := newClient(t, "")
	f.QueueChat(llmtest.ChatScript{Content: "done"})
	_, err := c.Chat(context.Background(), llm.ChatRequest{Model: "m", Messages: []llm.Message{
		userText("q"),
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "t", Arguments: json.RawMessage(`{"a":1}`)}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: []llm.ContentPart{{Type: "text", Text: "result"}}},
	}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	msgs := f.Requests("/chat/completions")[0].JSON(t)["messages"].([]any)
	asst := msgs[1].(map[string]any)
	call := asst["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "c1" || call["type"] != "function" || call["function"].(map[string]any)["arguments"] != `{"a":1}` {
		t.Fatalf("assistant tool call = %v", call)
	}
	tool := msgs[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "c1" || tool["content"] != "result" {
		t.Fatalf("tool message = %v", tool)
	}
}

func TestChatJSONObject(t *testing.T) {
	c, f := newClient(t, "")
	f.QueueChat(llmtest.ChatScript{Content: `{"pages":[]}`})
	resp, err := c.Chat(context.Background(), llm.ChatRequest{Model: "m", Messages: []llm.Message{userText("x")}, JSONObject: true})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != `{"pages":[]}` {
		t.Fatalf("content = %q", resp.Content)
	}
	rf := f.Requests("/chat/completions")[0].JSON(t)["response_format"].(map[string]any)
	if rf["type"] != "json_object" {
		t.Fatalf("response_format = %v", rf)
	}
}

func TestChatFilePartShape(t *testing.T) {
	c, f := newClient(t, "")
	f.QueueChat(llmtest.ChatScript{Content: "ok"})
	pdf := []byte("%PDF-1.4 fake")
	parts, err := llm.PDFParts("file", "m.pdf", pdf)
	if err != nil {
		t.Fatalf("PDFParts: %v", err)
	}
	parts = append(parts, llm.ContentPart{Type: "text", Text: "read it"})
	if _, err := c.Chat(context.Background(), llm.ChatRequest{Model: "m", Messages: []llm.Message{{Role: llm.RoleUser, Content: parts}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	content := f.Requests("/chat/completions")[0].JSON(t)["messages"].([]any)[0].(map[string]any)["content"].([]any)
	file := content[0].(map[string]any)
	if file["type"] != "file" {
		t.Fatalf("part type = %v", file["type"])
	}
	inner := file["file"].(map[string]any)
	want := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(pdf)
	if inner["filename"] != "m.pdf" || inner["file_data"] != want {
		t.Fatalf("file part = %v", inner)
	}
	if text := content[1].(map[string]any); text["type"] != "text" || text["text"] != "read it" {
		t.Fatalf("text part = %v", text)
	}
}

func TestPDFPartsImageReserved(t *testing.T) {
	if _, err := llm.PDFParts("image", "m.pdf", []byte("%PDF")); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("err = %v, want reserved", err)
	}
}

func TestRetryable(t *testing.T) {
	c, f := newClient(t, "")
	long := strings.Repeat("x", 1000)
	f.QueueChat(
		llmtest.ChatScript{Status: 429, RetryAfter: "7", Body: long},
		llmtest.ChatScript{Status: 503, Body: "down"},
		llmtest.ChatScript{Status: 400, Body: long},
	)
	req := llm.ChatRequest{Model: "m", Messages: []llm.Message{userText("x")}}

	_, err := c.Chat(context.Background(), req)
	var re *llm.RetryableError
	if !errors.As(err, &re) || re.Status != 429 || re.RetryAfter != 7*time.Second {
		t.Fatalf("429 err = %#v", err)
	}
	_, err = c.Chat(context.Background(), req)
	if !errors.As(err, &re) || re.Status != 503 {
		t.Fatalf("503 err = %#v", err)
	}
	_, err = c.Chat(context.Background(), req)
	if !errors.Is(err, llm.ErrPermanent) {
		t.Fatalf("400 err = %v, want ErrPermanent", err)
	}
	if strings.Contains(err.Error(), "test-key") || strings.Count(err.Error(), "x") > 200 {
		t.Fatalf("error leaks key or full body: %q", err)
	}
}

func TestNoChoicesIsRetryable(t *testing.T) {
	c, f := newClient(t, "")
	f.ChatFunc(nil)
	// An unscripted fake replies 500; the client must classify it as
	// retryable, not panic on the missing choices.
	_, err := c.Chat(context.Background(), llm.ChatRequest{Model: "m", Messages: []llm.Message{userText("x")}})
	var re *llm.RetryableError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want retryable", err)
	}
}

func TestAuthHeader(t *testing.T) {
	c, f := newClient(t, "e")
	f.QueueChat(llmtest.ChatScript{Content: "ok"})
	if _, err := c.Chat(context.Background(), llm.ChatRequest{Model: "m", Messages: []llm.Message{userText("x")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Embed(context.Background(), []string{"a"}, llm.Query); err != nil {
		t.Fatal(err)
	}
	if got := f.Requests("/chat/completions")[0].Auth; got != "Bearer test-key" {
		t.Fatalf("chat auth = %q", got)
	}
	if got := f.Requests("/embeddings")[0].Auth; got != "Bearer embed-key" {
		t.Fatalf("embed auth = %q", got)
	}
}

func TestEmbedSendsDimensionsAndPrefix(t *testing.T) {
	f := llmtest.New(t)
	ec := f.EmbedConfig("gem")
	ec.QueryPrefix, ec.DocumentPrefix = "q: ", "d: "
	c := llm.New(f.LLMConfig(), ec, http.DefaultClient)

	got, err := c.Embed(context.Background(), []string{"roof", "lift"}, llm.Query)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	body := f.Requests("/embeddings")[0].JSON(t)
	if body["dimensions"] != float64(1536) || body["model"] != "gem" {
		t.Fatalf("body = %v", body)
	}
	if in := body["input"].([]any); in[0] != "q: roof" || in[1] != "q: lift" {
		t.Fatalf("input = %v", in)
	}
	// The fake replies in reverse order; results must follow index.
	if len(got) != 2 || got[0][0] != llmtest.HashVector("q: roof")[0] || got[1][0] != llmtest.HashVector("q: lift")[0] {
		t.Fatal("embeddings not ordered by response index")
	}

	if _, err := c.Embed(context.Background(), []string{"page"}, llm.Document); err != nil {
		t.Fatal(err)
	}
	if in := f.Requests("/embeddings")[1].JSON(t)["input"].([]any); in[0] != "d: page" {
		t.Fatalf("document input = %v", in)
	}
	if c.EmbedModel() != "gem" {
		t.Fatalf("EmbedModel = %q", c.EmbedModel())
	}
}

func TestEmbedRejectsWrongDimension(t *testing.T) {
	c, f := newClient(t, "e")
	f.EmbedFunc(func(string) []float32 { return make([]float32, 768) })
	if _, err := c.Embed(context.Background(), []string{"a"}, llm.Document); err == nil || !strings.Contains(err.Error(), "1536") {
		t.Fatalf("err = %v, want dimension error", err)
	}
}

func TestEmbedUnconfigured(t *testing.T) {
	c, _ := newClient(t, "")
	if c.EmbedModel() != "" {
		t.Fatalf("EmbedModel = %q, want empty", c.EmbedModel())
	}
	if _, err := c.Embed(context.Background(), []string{"a"}, llm.Query); !errors.Is(err, llm.ErrNoEmbeddings) {
		t.Fatalf("err = %v, want ErrNoEmbeddings", err)
	}
}

func TestModelNames(t *testing.T) {
	c, _ := newClient(t, "")
	if c.Model() != "fake-chat" || c.ExtractModel() != "fake-extract" || c.PDFMode() != "file" {
		t.Fatalf("models = %q %q %q", c.Model(), c.ExtractModel(), c.PDFMode())
	}
}
