// Package cleanup runs a small local LLM over the raw transcript to remove
// filler, fix punctuation, and apply light formatting — without ever answering
// or executing the dictated content. The system prompt lives in
// prompts/cleanup.txt.
//
// The real implementation calls a local model (Qwen3.5 4B Instruct via Ollama)
// at low temperature with thinking disabled.
package cleanup

// Cleaner rewrites a raw transcript into clean text.
type Cleaner interface {
	Clean(raw string) (string, error)
}

// Noop passes the transcript through unchanged — the "raw / fast" mode that
// skips the LLM entirely.
type Noop struct{}

// NewNoop returns a pass-through cleaner.
func NewNoop() *Noop { return &Noop{} }

// Clean returns the transcript unchanged.
func (n *Noop) Clean(raw string) (string, error) { return raw, nil }
