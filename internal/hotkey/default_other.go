//go:build !windows

package hotkey

// Default returns the platform push-to-talk trigger. Off Windows the real global
// hook isn't wired up yet, so this falls back to the terminal stub (press Enter
// to simulate a dictation).
func Default() Trigger { return NewStdinStub() }
