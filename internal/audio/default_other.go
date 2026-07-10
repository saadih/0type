//go:build !windows

package audio

// Default returns the platform recorder. Off Windows real capture isn't wired up
// yet, so this falls back to the silent stub.
func Default() Recorder { return NewStub() }
