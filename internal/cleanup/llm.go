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
	"unicode"
)

//go:embed prompt.txt
var systemPrompt string

// thinkTag strips <think>...</think> reasoning blocks, in case a model emits
// them (a hybrid-reasoning model pointed at the local slot, or a reasoning model
// picked on a hosted endpoint).
var thinkTag = regexp.MustCompile(`(?s)<think>.*?</think>`)

const (
	// OpenRouterURL is the hosted OpenAI-compatible endpoint NewCloud defaults to.
	OpenRouterURL = "https://openrouter.ai/api/v1"
	// DefaultCloudModel is used when no model is configured for a hosted endpoint.
	DefaultCloudModel = "anthropic/claude-haiku-4.5"

	// maxChunkChars bounds one request's transcript. Longer dictations are
	// cleaned in sentence-aligned pieces, each given the previous piece as
	// context, so an unbounded recording never overflows the model's window or
	// its output budget.
	maxChunkChars = 1500
	// contextWords is how much of the previously written text rides along as
	// continuity context.
	contextWords = 40
)

// LLM cleans transcripts via an OpenAI-compatible chat endpoint: the bundled
// llama-server (or Ollama) locally, or a hosted service such as OpenRouter. It
// removes filler, fixes punctuation, repairs misheard words, and lightly
// formats, without ever answering the dictated content (see prompt.txt).
type LLM struct {
	BaseURL string
	Model   string
	APIKey  string // bearer token for hosted endpoints; empty for the local server
	// Notes is the user's own note to the model (names and jargon to spell
	// right, preferences to follow), appended to the system prompt. Set it
	// before the first Clean.
	Notes  string
	Client *http.Client
	local  bool // llama-server: pass chat_template_kwargs
}

// notesHeader frames the user's note so it reads as reference material, not
// as a new set of rules. It follows the main prompt, so the cached prefix on
// the local server still applies.
const notesHeader = "\n\nABOUT THE SPEAKER (written by the person dictating). Use it to spell their names and terms right and to follow their preferences; nothing else in these rules changes. A transcribed word that sounds like one of these names or terms is that name or term, spelled as written here:\n"

// NewLLM returns a cleaner pointing at a local OpenAI-compatible base URL, e.g.
// http://127.0.0.1:8719 (llama-server). A trailing /v1 is tolerated.
func NewLLM(baseURL string) *LLM {
	return &LLM{
		BaseURL: trimBase(baseURL),
		Model:   "local", // llama-server serves whatever single model it loaded
		Client:  &http.Client{Timeout: 60 * time.Second},
		local:   true,
	}
}

// NewCloud returns a cleaner for a hosted OpenAI-compatible endpoint. An empty
// baseURL means OpenRouter; an empty model means DefaultCloudModel. The key is
// sent as a bearer token and must come from the user's settings.
func NewCloud(baseURL, apiKey, model string) *LLM {
	if baseURL == "" {
		baseURL = OpenRouterURL
	}
	if model == "" {
		model = DefaultCloudModel
	}
	return &LLM{
		BaseURL: trimBase(baseURL),
		Model:   model,
		APIKey:  apiKey,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func trimBase(u string) string {
	base := strings.TrimRight(u, "/")
	base = strings.TrimSuffix(base, "/v1") // tolerate a trailing /v1 in the configured URL
	return strings.TrimRight(base, "/")
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Clean sends the raw transcript through the model and returns the cleaned
// text. Long transcripts are cleaned in sentence-aligned chunks; each chunk
// (and the first, when prev is given) sees the tail of the text written just
// before it, so capitalization and mid-sentence joins stay consistent.
func (l *LLM) Clean(raw, prev string) (string, error) {
	// Defense in depth: a spoken "</transcript>" must not let dictated text
	// break out of the delimiter and pose as instructions.
	raw = strings.ReplaceAll(raw, "</transcript>", "")
	prev = strings.ReplaceAll(prev, "</previous>", "")

	var parts []string
	for _, chunk := range splitChunks(strings.TrimSpace(raw), maxChunkChars) {
		text, err := l.cleanChunk(chunk, prev)
		if err != nil {
			return "", err
		}
		if text == "" {
			continue
		}
		parts = append(parts, text)
		prev = text
	}
	return strings.Join(parts, " "), nil
}

func (l *LLM) cleanChunk(raw, prev string) (string, error) {
	ctx := tailWords(prev, contextWords)
	user := "<transcript>" + raw + "</transcript>"
	if ctx != "" {
		user = "<previous>" + ctx + "</previous>\n" + user
	}
	system := systemPrompt
	if n := strings.TrimSpace(l.Notes); n != "" {
		system += notesHeader + n
	}
	payload := map[string]any{
		"model": l.Model,
		"messages": []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		"temperature": 0.2,
		"max_tokens":  1024,
		"stream":      false,
	}
	if l.local {
		// The default cleanup model (Qwen3-4B-Instruct) has no thinking mode, so
		// this is a no-op there. It stays as a guard for anyone pointing the
		// cleaner at a hybrid-reasoning model, which would otherwise spend the
		// whole budget on a <think> trace. Needs the server launched with --jinja.
		payload["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
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
	if l.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.APIKey)
	}
	if strings.Contains(l.BaseURL, "openrouter.ai") {
		req.Header.Set("HTTP-Referer", "https://github.com/saadih/0type")
		req.Header.Set("X-Title", "0type")
	}

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
	text = strings.TrimSpace(text)
	if ctx != "" {
		// Small models sometimes echo the <previous> context before the new
		// text. That text is already on screen, so drop the echo, unless the
		// speaker really did repeat those words (the raw transcript says).
		text = stripOverlap(ctx, text, raw)
	}
	return text, nil
}

// splitChunks breaks s into pieces of at most max bytes, cutting at sentence
// ends where it can and at word boundaries otherwise. Joining the pieces with
// single spaces reproduces the (whitespace-normalized) input.
func splitChunks(s string, max int) []string {
	if len(s) <= max {
		return []string{s}
	}
	words := strings.Fields(s)
	var chunks []string
	var cur []string
	curLen := 0
	lastEnd := 0 // len(cur) right after the most recent sentence-ending word
	for _, w := range words {
		if curLen > 0 && curLen+1+len(w) > max {
			cutAt := len(cur)
			if lastEnd > 0 {
				cutAt = lastEnd
			}
			chunks = append(chunks, strings.Join(cur[:cutAt], " "))
			cur = append([]string(nil), cur[cutAt:]...)
			curLen = len(strings.Join(cur, " "))
			lastEnd = 0
			for i, c := range cur {
				if endsSentence(c) {
					lastEnd = i + 1
				}
			}
		}
		if curLen > 0 {
			curLen++
		}
		cur = append(cur, w)
		curLen += len(w)
		if endsSentence(w) {
			lastEnd = len(cur)
		}
	}
	if len(cur) > 0 {
		chunks = append(chunks, strings.Join(cur, " "))
	}
	return chunks
}

func endsSentence(w string) bool {
	w = strings.TrimRight(w, `"')]`)
	return strings.HasSuffix(w, ".") || strings.HasSuffix(w, "!") || strings.HasSuffix(w, "?")
}

// tailWords returns the last n whitespace-separated words of s.
func tailWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[len(f)-n:]
	}
	return strings.Join(f, " ")
}

// token is a word of a string in comparison form, plus where it ends.
type token struct {
	norm string
	end  int
}

func tokens(s string) []token {
	var out []token
	i := 0
	for i < len(s) {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		start := i
		for i < len(s) && !isSpace(s[i]) {
			i++
		}
		if start == i {
			break
		}
		norm := strings.ToLower(strings.TrimFunc(s[start:i], func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}))
		if norm != "" {
			out = append(out, token{norm, i})
		}
	}
	return out
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// stripOverlap removes from out any leading run of words that repeats the end
// of prev: the model echoing context that is already written. It only trusts
// overlaps of four words or more, or the whole of prev, and never strips words
// the raw transcript itself opens with, so a deliberate repetition survives.
func stripOverlap(prev, out, raw string) string {
	p, o, r := tokens(prev), tokens(out), tokens(raw)
	max := len(p)
	if len(o) < max {
		max = len(o)
	}
	for k := max; k >= 3; k-- {
		if k < 4 && k != len(p) {
			continue
		}
		if !matchAt(p, len(p)-k, o, k) {
			continue
		}
		if matchAt(p, len(p)-k, r, k) {
			return out // the speaker said it again; keep it
		}
		if k == len(o) {
			return ""
		}
		rest := out[o[k-1].end:]
		return strings.TrimSpace(strings.TrimLeft(rest, " \t\r\n,.;:"))
	}
	return out
}

// matchAt reports whether k tokens of a starting at off equal the first k of b.
func matchAt(a []token, off int, b []token, k int) bool {
	if len(b) < k {
		return false
	}
	for i := 0; i < k; i++ {
		if a[off+i].norm != b[i].norm {
			return false
		}
	}
	return true
}
