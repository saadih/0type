package cleanup

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

//go:embed prompt.txt
var systemPrompt string

// thinkTag strips Qwen's <think>...</think> reasoning blocks, in case the model
// emits them despite enable_thinking:false.
var thinkTag = regexp.MustCompile(`(?s)<think>.*?</think>`)

// LLM cleans transcripts via a local OpenAI-compatible chat endpoint (llama.cpp's
// llama-server or Ollama). It removes filler, fixes punctuation, and lightly
// formats — without ever answering the dictated content (see prompt.txt).
type LLM struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

// NewLLM returns a cleaner pointing at an OpenAI-compatible base URL, e.g.
// http://127.0.0.1:8719 (llama-server). A trailing /v1 is tolerated.
func NewLLM(baseURL string) *LLM {
	base := strings.TrimRight(baseURL, "/")
	base = strings.TrimSuffix(base, "/v1") // tolerate a trailing /v1 in the configured URL
	base = strings.TrimRight(base, "/")
	return &LLM{
		BaseURL: base,
		Model:   "local", // llama-server serves whatever single model it loaded
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Clean sends the raw transcript through the model and returns the cleaned text.
func (l *LLM) Clean(raw string) (string, error) {
	// Defense in depth: a spoken "</transcript>" must not let dictated text
	// break out of the delimiter and pose as instructions.
	raw = strings.ReplaceAll(raw, "</transcript>", "")

	payload := map[string]any{
		"model": l.Model,
		"messages": []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "<transcript>" + raw + "</transcript>"},
		},
		"temperature": 0.2,
		"max_tokens":  512,
		"stream":      false,
		// The default cleanup model (Qwen3-4B-Instruct) has no thinking mode, so
		// this is a no-op there. It stays as a guard for anyone pointing the
		// cleaner at a hybrid-reasoning model, which would otherwise spend the
		// whole budget on a <think> trace. Needs the server launched with --jinja.
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, l.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("cleanup: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}

	var out struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("cleanup: empty response")
	}

	text := thinkTag.ReplaceAllString(out.Choices[0].Message.Content, "")
	// A truncated (max_tokens) or non-jinja response can leave an unclosed
	// <think> the regex can't match; drop from any opener so raw reasoning is
	// never pasted.
	if i := strings.Index(text, "<think>"); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text), nil
}
