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
// emits them despite the /no_think directive.
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
// http://127.0.0.1:8719 (llama-server) — no trailing /v1.
func NewLLM(baseURL string) *LLM {
	return &LLM{
		BaseURL: strings.TrimRight(baseURL, "/"),
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
	payload := map[string]any{
		"model": l.Model,
		"messages": []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "<transcript>" + raw + "</transcript>"},
		},
		"temperature": 0.2,
		"max_tokens":  512,
		"stream":      false,
		// Qwen is a hybrid-reasoning model; without this it spends the whole
		// token budget on a <think> trace and returns no answer. Needs the
		// server launched with --jinja so the chat template honors the flag.
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
	return strings.TrimSpace(text), nil
}
