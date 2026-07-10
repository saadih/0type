// Package cleanup runs a small local LLM over the raw transcript to remove
// filler, fix punctuation, and apply light formatting — without ever answering
// or executing the dictated content. The system prompt is embedded from
// internal/cleanup/prompt.txt.
//
// The default cleaner talks to a local OpenAI-compatible endpoint (llama.cpp's
// llama-server or Ollama) running Qwen3.5 4B at low temperature with thinking
// disabled; without one configured it falls back to a pass-through.
package cleanup

import "os"

// Cleaner rewrites a raw transcript into clean text.
type Cleaner interface {
	Clean(raw string) (string, error)
}

// Default returns the LLM cleaner when ZEROTYPE_CLEANUP_URL points at an
// OpenAI-compatible endpoint, otherwise a pass-through. The URL is only read
// from the environment.
func Default() Cleaner {
	if url := os.Getenv("ZEROTYPE_CLEANUP_URL"); url != "" {
		return NewLLM(url)
	}
	return NewNoop()
}

// Noop passes the transcript through unchanged — the "raw / fast" mode that
// skips the LLM entirely.
type Noop struct{}

// NewNoop returns a pass-through cleaner.
func NewNoop() *Noop { return &Noop{} }

// Clean returns the transcript unchanged.
func (n *Noop) Clean(raw string) (string, error) { return raw, nil }
