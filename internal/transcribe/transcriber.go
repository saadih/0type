// Package transcribe turns captured audio into a raw text transcript.
//
// Transcribers accept a complete WAV file (16 kHz mono 16-bit PCM in a RIFF
// container), the format the Recorder produces. Local Parakeet (sherpa-onnx)
// is the default backend; OpenRouter's hosted speech-to-text models are the
// cloud alternative, behind the same interface.
package transcribe

import (
	"errors"
	"os"
)

// Transcriber converts WAV audio into a raw transcript.
type Transcriber interface {
	Transcribe(wav []byte) (string, error)
}

// ErrNoModel means no transcription backend is available: no local model
// downloaded and no cloud key. The engine turns it into a "download a model"
// notice instead of pasting anything.
var ErrNoModel = errors.New("no transcription model installed")

// NeedModel is the GUI's placeholder when nothing is ready. It transcribes
// nothing and reports ErrNoModel, so a fresh install prompts for a download
// rather than pasting the stub's canned text.
type NeedModel struct{}

// NewNeedModel returns a transcriber that always reports ErrNoModel.
func NewNeedModel() *NeedModel { return &NeedModel{} }

// Transcribe always returns ErrNoModel.
func (n *NeedModel) Transcribe(wav []byte) (string, error) { return "", ErrNoModel }

// Default returns OpenRouter when OPENROUTER_API_KEY is set, otherwise the
// stub. The key is only ever read from the environment, never hardcoded or
// committed.
func Default() Transcriber {
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		return NewOpenRouter(key, os.Getenv("OPENROUTER_STT_MODEL"))
	}
	return NewStub()
}

// Stub returns a fixed placeholder transcript so downstream stages have
// something realistic (filler + spoken punctuation) to work on.
type Stub struct{}

// NewStub returns a placeholder transcriber.
func NewStub() *Stub { return &Stub{} }

// Transcribe ignores its input and returns a canned raw transcript.
func (s *Stub) Transcribe(wav []byte) (string, error) {
	return "um so this is a uh test of the zero typing pipeline period", nil
}
