//go:build !windows

package inject

// Default returns the platform injector. Off Windows the clipboard-paste path
// isn't wired up yet, so this falls back to the print-only stub.
func Default() Injector { return NewStub() }
