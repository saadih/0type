// Package cleanup runs an LLM over the raw transcript to remove filler, fix
// punctuation, repair words the speech recognizer misheard, and apply light
// formatting, without ever answering or executing the dictated content. The
// system prompt is embedded from internal/cleanup/prompt.txt.
//
// The default cleaner talks to the bundled llama-server (Qwen3-4B-Instruct) at
// low temperature; the same client also drives a hosted OpenAI-compatible
// endpoint such as OpenRouter when the user prefers a bigger model. Without
// either, cleanup falls back to a pass-through.
package cleanup

import (
	"errors"
	"os"
)

// Cleaner rewrites a raw transcript into clean text.
type Cleaner interface {
	// Clean rewrites raw. prev (may be empty) is the text already written
	// immediately before raw in the same dictation; it is context for
	// continuity only and never part of the result.
	Clean(raw, prev string) (string, error)
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

// Noop passes the transcript through unchanged: the "raw / fast" mode that
// skips the LLM entirely.
type Noop struct{}

// NewNoop returns a pass-through cleaner.
func NewNoop() *Noop { return &Noop{} }

// Clean returns the transcript unchanged.
func (n *Noop) Clean(raw, prev string) (string, error) { return raw, nil }

// ErrNotReady means the bundled cleanup server is still starting. Clean
// returns the transcript unchanged alongside it, so the engine pastes the raw
// text and can say why it was not cleaned instead of degrading in silence.
var ErrNotReady = errors.New("cleanup model still starting")

// Starting stands in while the bundled server loads its model, which takes
// longer than the first dictation usually does. It passes the transcript
// through and reports ErrNotReady.
type Starting struct{}

// NewStarting returns a pass-through cleaner that reports ErrNotReady.
func NewStarting() *Starting { return &Starting{} }

// Clean returns the transcript unchanged, with ErrNotReady.
func (s *Starting) Clean(raw, prev string) (string, error) { return raw, ErrNotReady }
