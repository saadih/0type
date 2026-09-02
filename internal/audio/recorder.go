// Package audio captures microphone input during a dictation.
//
// The Windows implementation uses winmm (waveIn) to capture 16 kHz mono 16-bit
// PCM (the format Parakeet expects) into a ring of small buffers, so a
// recording can run for as long as the speaker likes and, through Streaming,
// be handed out while it is still in progress.
package audio

// Recorder captures microphone audio between Start and Stop.
type Recorder interface {
	Start() error
	// Stop ends capture and returns a complete WAV file (16 kHz mono 16-bit PCM
	// in a RIFF container), the format transcribers accept.
	Stop() ([]byte, error)
}

// DeviceSelector is an optional Recorder capability: picking the input device by
// name ("" = system default). Backends without it are simply left on default.
type DeviceSelector interface {
	SetInputDevice(name string)
}

// Streaming is an optional Recorder capability: delivering audio while it is
// being captured instead of at Stop. fn receives each new chunk of raw 16 kHz
// mono 16-bit PCM (no WAV header) in order, from the recorder's own goroutine;
// Stop flushes the last chunks through fn before returning and then returns an
// empty WAV, since the caller already has every byte. fn must return promptly.
// Set it before Start; nil disables streaming.
type Streaming interface {
	OnAudio(fn func(pcm []byte))
}

// Stub returns empty audio, enough to exercise the pipeline.
type Stub struct{}

// NewStub returns a no-op recorder.
func NewStub() *Stub { return &Stub{} }

// Start begins the (stub) capture.
func (s *Stub) Start() error { return nil }

// Stop returns no audio.
func (s *Stub) Stop() ([]byte, error) { return nil, nil }
