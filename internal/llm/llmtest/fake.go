// Package llmtest is a scripted, in-process OpenAI-compatible provider for
// tests: chat completions reply from a FIFO queue (or a function), and
// embeddings are deterministic vectors derived from the input text.
package llmtest

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"shigoto/internal/config"
)

// Dimensions is the embedding length the fake returns by default.
const Dimensions = 1536

// ScriptCall is one tool call in a scripted reply.
type ScriptCall struct {
	ID   string
	Name string
	Args string // raw JSON arguments
}

// ChatScript is one scripted /chat/completions reply. A non-zero Status
// other than 200 replies with that status and Body (RetryAfter sets the
// Retry-After header).
type ChatScript struct {
	Content    string
	ToolCalls  []ScriptCall
	Status     int
	RetryAfter string
	Body       string
	// PromptTokens and CompletionTokens are reported as usage.
	PromptTokens, CompletionTokens int
}

// Recorded is one request the fake received.
type Recorded struct {
	Path string
	Auth string
	Body []byte
}

// JSON decodes the recorded body.
func (r Recorded) JSON(t testing.TB) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("decoding recorded %s body: %v", r.Path, err)
	}
	return m
}

// Fake is a scripted provider. Its zero value is not usable; call New.
type Fake struct {
	URL string // base URL, ending in /v1

	mu        sync.Mutex
	requests  []Recorded
	queue     []ChatScript
	chatFunc  func(body map[string]any) ChatScript
	embedFunc func(input string) []float32
	embedErr  *ChatScript
}

// New starts a fake provider, closed when t finishes.
func New(t testing.TB) *Fake {
	f := &Fake{embedFunc: HashVector}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL + "/v1"
	return f
}

// QueueChat appends scripted chat replies, served in order.
func (f *Fake) QueueChat(s ...ChatScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, s...)
}

// ChatFunc answers chat requests the queue does not cover, from the
// decoded request body.
func (f *Fake) ChatFunc(fn func(body map[string]any) ChatScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatFunc = fn
}

// EmbedFunc replaces the embedding function (default HashVector).
func (f *Fake) EmbedFunc(fn func(input string) []float32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.embedFunc = fn
}

// EmbedError makes every /embeddings request fail with s's status, body
// and Retry-After; nil restores success.
func (f *Fake) EmbedError(s *ChatScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.embedErr = s
}

// Requests returns the requests received so far, optionally only those
// whose path ends with suffix.
func (f *Fake) Requests(suffix string) []Recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Recorded
	for _, r := range f.requests {
		if suffix == "" || strings.HasSuffix(r.Path, suffix) {
			out = append(out, r)
		}
	}
	return out
}

// LLMConfig points a config.LLM at the fake with model "fake-chat".
func (f *Fake) LLMConfig() config.LLM {
	return config.LLM{BaseURL: f.URL, APIKey: "test-key", Model: "fake-chat", ExtractModel: "fake-extract", PDFMode: "file"}
}

// EmbedConfig points a config.Embed at the fake with the given model name.
func (f *Fake) EmbedConfig(model string) *config.Embed {
	return &config.Embed{BaseURL: f.URL, APIKey: "embed-key", Model: model}
}

// HashVector is a deterministic unit vector of length Dimensions for s:
// equal inputs embed identically; different inputs almost surely differ.
func HashVector(s string) []float32 {
	v := make([]float32, Dimensions)
	var norm float64
	for i := range v {
		h := fnv.New64a()
		fmt.Fprintf(h, "%d:%s", i, s)
		x := float64(h.Sum64()%2000)/1000 - 1
		v[i] = float32(x)
		norm += x * x
	}
	n := float32(math.Sqrt(norm))
	for i := range v {
		v[i] /= n
	}
	return v
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, Recorded{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
	f.mu.Unlock()

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	switch r.URL.Path {
	case "/v1/chat/completions":
		f.serveChat(w, req)
	case "/v1/embeddings":
		f.serveEmbed(w, req)
	default:
		http.NotFound(w, r)
	}
}

func writeStatus(w http.ResponseWriter, s ChatScript) bool {
	if s.Status == 0 || s.Status == http.StatusOK {
		return false
	}
	if s.RetryAfter != "" {
		w.Header().Set("Retry-After", s.RetryAfter)
	}
	w.WriteHeader(s.Status)
	_, _ = io.WriteString(w, s.Body)
	return true
}

func (f *Fake) serveChat(w http.ResponseWriter, req map[string]any) {
	f.mu.Lock()
	var s ChatScript
	switch {
	case len(f.queue) > 0:
		s = f.queue[0]
		f.queue = f.queue[1:]
	case f.chatFunc != nil:
		fn := f.chatFunc
		f.mu.Unlock()
		s = fn(req)
		f.mu.Lock()
	default:
		f.mu.Unlock()
		http.Error(w, `{"error":{"message":"llmtest: no scripted chat reply"}}`, http.StatusInternalServerError)
		return
	}
	f.mu.Unlock()
	if writeStatus(w, s) {
		return
	}

	msg := map[string]any{"role": "assistant", "content": s.Content}
	if len(s.ToolCalls) > 0 {
		var calls []map[string]any
		for i, c := range s.ToolCalls {
			id := c.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			calls = append(calls, map[string]any{"id": id, "type": "function",
				"function": map[string]any{"name": c.Name, "arguments": c.Args}})
		}
		msg["tool_calls"] = calls
		if s.Content == "" {
			msg["content"] = nil
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": s.PromptTokens, "completion_tokens": s.CompletionTokens},
	})
}

func (f *Fake) serveEmbed(w http.ResponseWriter, req map[string]any) {
	f.mu.Lock()
	errScript, fn := f.embedErr, f.embedFunc
	f.mu.Unlock()
	if errScript != nil && writeStatus(w, *errScript) {
		return
	}
	inputs, _ := req["input"].([]any)
	data := make([]map[string]any, len(inputs))
	// Reply in reverse order with explicit indexes: clients must order by
	// index, not by array position.
	for i, in := range inputs {
		s, _ := in.(string)
		data[len(inputs)-1-i] = map[string]any{"object": "embedding", "index": i, "embedding": fn(s)}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data, "model": req["model"]})
}
