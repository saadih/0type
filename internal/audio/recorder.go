// Package audio captures microphone input during a dictation.
//
// The real implementation uses miniaudio (malgo) to capture 16 kHz mono 16-bit
// PCM — the format Parakeet expects.
package audio

// Recorder captures microphone audio between Start and Stop.
type Recorder interface {
	Start() error
	// Stop ends capture and returns a complete WAV file (16 kHz mono 16-bit PCM
	// in a RIFF container) — the format transcribers accept.
	Stop() ([]byte, error)
}

// DeviceSelector is an optional Recorder capability: picking the input device by
// name ("" = system default). Backends without it are simply left on default.
type DeviceSelector interface {
	SetInputDevice(name string)
}

// Stub returns empty audio — enough to exercise the pipeline.
type Stub struct{}

// NewStub returns a no-op recorder.
func NewStub() *Stub { return &Stub{} }

// Start begins the (stub) capture.
func (s *Stub) Start() error { return nil }

// Stop returns no audio.
func (s *Stub) Stop() ([]byte, error) { return nil, nil }
