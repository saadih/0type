// Package inject places the final text at the cursor in the focused app.
//
// The real implementation writes the text to the clipboard and sends the paste
// shortcut (Ctrl/Cmd+V), then restores the previous clipboard — faster and far
// more reliable with Unicode (e.g. Swedish characters) than simulating
// individual keystrokes.
package inject

import "fmt"

// Injector delivers text into whatever app currently has focus.
type Injector interface {
	Inject(text string) error
}

// Stub prints the text instead of injecting it, so you can watch pipeline output
// without touching the OS.
type Stub struct{}

// NewStub returns a print-only injector.
func NewStub() *Stub { return &Stub{} }

// Inject writes the text to stdout.
func (s *Stub) Inject(text string) error {
	fmt.Printf("→ %s\n", text)
	return nil
}
