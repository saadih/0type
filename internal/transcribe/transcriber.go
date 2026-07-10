// Package transcribe turns captured audio into a raw text transcript.
//
// The real implementation uses sherpa-onnx (Go bindings) running NVIDIA
// Parakeet TDT 0.6B v3 locally. A cloud implementation (e.g. Groq
// whisper-large-v3-turbo) is a good v0 stand-in to prove the loop before the
// local model is wired up.
package transcribe

// Transcriber converts 16 kHz mono PCM into a raw transcript.
type Transcriber interface {
	Transcribe(pcm []byte) (string, error)
}

// Stub returns a fixed placeholder transcript so downstream stages have
// something realistic (filler + spoken punctuation) to work on.
type Stub struct{}

// NewStub returns a placeholder transcriber.
func NewStub() *Stub { return &Stub{} }

// Transcribe ignores its input and returns a canned raw transcript.
func (s *Stub) Transcribe(pcm []byte) (string, error) {
	return "um so this is a uh test of the zero typing pipeline period", nil
}
