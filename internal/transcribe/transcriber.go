// Package transcribe turns captured audio into a raw text transcript.
//
// Transcribers accept a complete WAV file (16 kHz mono 16-bit PCM in a RIFF
// container), the format the Recorder produces. Local Parakeet (sherpa-onnx) is
// the only real backend: audio never leaves the machine. The rest are
// placeholders reporting why no transcript is coming yet.
package transcribe

import "errors"

// Transcriber converts WAV audio into a raw transcript.
type Transcriber interface {
	Transcribe(wav []byte) (string, error)
}

// ErrNoModel means no transcription backend is available: the local model has
// not been downloaded. The engine turns it into a "download a model" notice
// instead of pasting anything.
var ErrNoModel = errors.New("no transcription model installed")

// NeedModel is the GUI's placeholder when nothing is ready. It transcribes
// nothing and reports ErrNoModel, so a fresh install prompts for a download
// rather than pasting the stub's canned text.
type NeedModel struct{}

// NewNeedModel returns a transcriber that always reports ErrNoModel.
func NewNeedModel() *NeedModel { return &NeedModel{} }

// Transcribe always returns ErrNoModel.
func (n *NeedModel) Transcribe(wav []byte) (string, error) { return "", ErrNoModel }

// ErrLoading means a local model is installed but still being read into
// memory. Loading it takes seconds, so the engine reports this as "starting
// up" rather than the "download a model" prompt ErrNoModel triggers, which
// would be both wrong and alarming.
var ErrLoading = errors.New("transcription model still loading")

// Loading is the placeholder while a downloaded model is being loaded in the
// background. It transcribes nothing and reports ErrLoading.
type Loading struct{}

// NewLoading returns a transcriber that always reports ErrLoading.
func NewLoading() *Loading { return &Loading{} }

// Transcribe always returns ErrLoading.
func (l *Loading) Transcribe(wav []byte) (string, error) { return "", ErrLoading }

// Default returns the canned stub, for console and dev builds. Real
// transcription is local Parakeet, wired up by the engine.
func Default() Transcriber { return NewStub() }

// Stub returns a fixed placeholder transcript so downstream stages have
// something realistic (filler + spoken punctuation) to work on.
type Stub struct{}

// NewStub returns a placeholder transcriber.
func NewStub() *Stub { return &Stub{} }

// Transcribe ignores its input and returns a canned raw transcript.
func (s *Stub) Transcribe(wav []byte) (string, error) {
	return "um so this is a uh test of the zero typing pipeline period", nil
}
