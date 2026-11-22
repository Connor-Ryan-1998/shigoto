// Package llm is shigoto's provider-agnostic model client. It speaks the
// OpenAI-compatible /chat/completions and /embeddings JSON APIs over plain
// net/http, so any compatible endpoint (OpenRouter, Google's OpenAI
// endpoint, vLLM, Ollama, Cloudflare Workers AI) is selected by
// configuration alone.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"shigoto/internal/config"
)

// Dimensions is the fixed embedding length shigoto stores
// (vector(1536)). Every embeddings request asks for it and any other
// length is an error.
const Dimensions = 1536

// Role is a chat message role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentPart is one part of a message: Type "text" (Text) or "file"
// (File).
type ContentPart struct {
	Type string
	Text string
	File *FilePart
}

// FilePart is a file sent inline as a data URL (OpenRouter's file input
// shape).
type FilePart struct {
	Filename string
	DataURL  string
}

// Message is one chat message.
type Message struct {
	Role       Role
	Content    []ContentPart
	ToolCalls  []ToolCall // assistant messages that called tools
	ToolCallID string     // tool messages
}

// Tool is a function the model may call; Parameters is its JSON schema.
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is a model's request to call a tool with raw JSON arguments.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// ChatRequest is one /chat/completions call. A zero Timeout means
// DefaultChatTimeout.
type ChatRequest struct {
	Model       string
	Messages    []Message
	Tools       []Tool
	ToolChoice  string // "", "auto" or "none"
	JSONObject  bool
	Temperature float64
	Timeout     time.Duration
}

// Usage is the token usage a provider reports.
type Usage struct {
	PromptTokens, CompletionTokens int
}

// ChatResponse is the first choice of a completion.
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
}

// EmbedKind says whether texts are search queries or stored passages.
type EmbedKind int

const (
	Query EmbedKind = iota
	Document
)

const (
	DefaultChatTimeout  = 60 * time.Second
	DefaultEmbedTimeout = 30 * time.Second
	maxResponseBytes    = 8 << 20
	maxErrorBody        = 200
)

// ErrPermanent marks a provider error that retrying will not fix (4xx
// other than 408/429, or an unusable response).
var ErrPermanent = errors.New("llm: permanent error")

// ErrNoEmbeddings is returned by Embed when embeddings are not configured.
var ErrNoEmbeddings = errors.New("llm: embeddings are not configured")

// RetryableError is a transient failure: 408, 429, 5xx or a transport
// error. RetryAfter is the provider's Retry-After hint, zero if absent.
type RetryableError struct {
	Status     int
	RetryAfter time.Duration
	Err        error
}

func (e *RetryableError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("llm: retryable status %d: %v", e.Status, e.Err)
	}
	return fmt.Sprintf("llm: retryable: %v", e.Err)
}

func (e *RetryableError) Unwrap() error { return e.Err }

// Client calls one chat endpoint and, optionally, one embeddings endpoint.
type Client struct {
	chat  config.LLM
	embed *config.Embed
	hc    *http.Client
}

// New returns a Client. e may be nil (no embeddings); hc nil means
// http.DefaultClient.
func New(c config.LLM, e *config.Embed, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{chat: c, embed: e, hc: hc}
}

// Model is the question-answering model.
func (c *Client) Model() string { return c.chat.Model }

// ExtractModel is the document-reading model.
func (c *Client) ExtractModel() string { return c.chat.ExtractModel }

// PDFMode is how PDFs are sent to ExtractModel.
func (c *Client) PDFMode() string { return c.chat.PDFMode }

// EmbedModel is the embeddings model, "" when embeddings are unconfigured.
func (c *Client) EmbedModel() string {
	if c.embed == nil {
		return ""
	}
	return c.embed.Model
}

type wireFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   *string         `json:"arguments,omitempty"`
}

type wireTool struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wirePart struct {
	Type string        `json:"type"`
	Text string        `json:"text,omitempty"`
	File *wireFilePart `json:"file,omitempty"`
}

type wireFilePart struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

type wireMessage struct {
	Role       Role       `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []wireTool `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// wireContent sends text-only content as a plain string (accepted by every
// provider) and mixed content as an array of parts.
func wireContent(parts []ContentPart) any {
	if len(parts) == 0 {
		return ""
	}
	textOnly := true
	for _, p := range parts {
		if p.Type != "text" {
			textOnly = false
		}
	}
	if textOnly {
		var b strings.Builder
		for i, p := range parts {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(p.Text)
		}
		return b.String()
	}
	out := make([]wirePart, len(parts))
	for i, p := range parts {
		out[i] = wirePart{Type: p.Type, Text: p.Text}
		if p.File != nil {
			out[i].File = &wireFilePart{Filename: p.File.Filename, FileData: p.File.DataURL}
		}
	}
	return out
}

// Chat runs one completion and returns its first choice.
func (c *Client) Chat(ctx context.Context, r ChatRequest) (ChatResponse, error) {
	msgs := make([]wireMessage, len(r.Messages))
	for i, m := range r.Messages {
		wm := wireMessage{Role: m.Role, ToolCallID: m.ToolCallID, Content: wireContent(m.Content)}
		for _, tc := range m.ToolCalls {
			args := string(tc.Arguments)
			wm.ToolCalls = append(wm.ToolCalls, wireTool{ID: tc.ID, Type: "function", Function: wireFunction{Name: tc.Name, Arguments: &args}})
		}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 && len(m.Content) == 0 {
			wm.Content = nil
		}
		msgs[i] = wm
	}
	body := map[string]any{"model": r.Model, "messages": msgs, "temperature": r.Temperature}
	if len(r.Tools) > 0 {
		tools := make([]wireTool, len(r.Tools))
		for i, t := range r.Tools {
			tools[i] = wireTool{Type: "function", Function: wireFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters}}
		}
		body["tools"] = tools
		if r.ToolChoice != "" {
			body["tool_choice"] = r.ToolChoice
		}
	}
	if r.JSONObject {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = DefaultChatTimeout
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content   *string    `json:"content"`
				ToolCalls []wireTool `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := c.post(ctx, c.chat.BaseURL, c.chat.APIKey, "/chat/completions", body, timeout, &resp); err != nil {
		return ChatResponse{}, fmt.Errorf("chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return ChatResponse{}, &RetryableError{Err: errors.New("chat: response has no choices")}
	}
	m := resp.Choices[0].Message
	out := ChatResponse{Usage: Usage{resp.Usage.PromptTokens, resp.Usage.CompletionTokens}}
	if m.Content != nil {
		out.Content = *m.Content
	}
	for _, tc := range m.ToolCalls {
		args := ""
		if tc.Function.Arguments != nil {
			args = *tc.Function.Arguments
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: json.RawMessage(args)})
	}
	return out, nil
}

// Embed embeds inputs, applying the configured query or document prefix.
// Results are in input order and each has length Dimensions.
func (c *Client) Embed(ctx context.Context, inputs []string, kind EmbedKind) ([][]float32, error) {
	if c.embed == nil {
		return nil, ErrNoEmbeddings
	}
	prefix := c.embed.QueryPrefix
	if kind == Document {
		prefix = c.embed.DocumentPrefix
	}
	in := make([]string, len(inputs))
	for i, s := range inputs {
		in[i] = prefix + s
	}
	body := map[string]any{"model": c.embed.Model, "input": in, "dimensions": Dimensions, "encoding_format": "float"}
	var resp struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := c.post(ctx, c.embed.BaseURL, c.embed.APIKey, "/embeddings", body, DefaultEmbedTimeout, &resp); err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(resp.Data) != len(inputs) {
		return nil, fmt.Errorf("embed: %w: got %d embeddings for %d inputs", ErrPermanent, len(resp.Data), len(inputs))
	}
	sort.Slice(resp.Data, func(i, j int) bool { return resp.Data[i].Index < resp.Data[j].Index })
	out := make([][]float32, len(inputs))
	for i, d := range resp.Data {
		if d.Index != i {
			return nil, fmt.Errorf("embed: %w: missing embedding index %d", ErrPermanent, i)
		}
		if len(d.Embedding) != Dimensions {
			return nil, fmt.Errorf("embed: %w: embedding has %d dimensions, want %d (the provider must honour the dimensions parameter)", ErrPermanent, len(d.Embedding), Dimensions)
		}
		out[i] = d.Embedding
	}
	return out, nil
}

func (c *Client) post(parent context.Context, base, key, path string, body any, timeout time.Duration, out any) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPermanent, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := c.hc.Do(req)
	if err != nil {
		// The caller giving up (cancel or its own deadline) is not a
		// provider failure; only this call's own timeout is retryable.
		if perr := parent.Err(); perr != nil {
			return perr
		}
		// Transport errors are retryable but must not echo the request
		// (whose URL may carry provider account ids).
		return &RetryableError{Err: errors.New("request failed: " + transportReason(err))}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return &RetryableError{Status: resp.StatusCode, Err: errors.New("reading response failed")}
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("%w: response larger than %d bytes", ErrPermanent, maxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(data)
		if len(snippet) > maxErrorBody {
			snippet = snippet[:maxErrorBody]
		}
		snippet = strings.ReplaceAll(snippet, key, "[redacted]")
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500 {
			return &RetryableError{Status: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After")), Err: errors.New(snippet)}
		}
		return fmt.Errorf("%w: status %d: %s", ErrPermanent, resp.StatusCode, snippet)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: decoding response: %v", ErrPermanent, err)
	}
	return nil
}

func transportReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	default:
		return "connection error"
	}
}

// retryAfter parses a Retry-After header given in seconds or as an HTTP
// date; unparseable or absent values are zero.
func retryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
