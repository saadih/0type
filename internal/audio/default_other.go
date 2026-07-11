//go:build !windows

package audio

// Default returns the platform recorder. Off Windows real capture isn't wired up
// yet, so this falls back to the silent stub.
func Default() Recorder { return NewStub() }

// InputDevices lists microphones. Off Windows there's no capture yet, so none.
func InputDevices() []string { return nil }
