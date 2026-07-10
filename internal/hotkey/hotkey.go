// Package hotkey provides the global push-to-talk trigger — the key or mouse
// button the user holds (or taps, in toggle mode) to dictate.
//
// The real implementation uses a global input hook (robotgo / libuiohook) so it
// captures mouse side buttons (MB4/MB5) and any key even when 0type is not
// focused. That mouse-button capability is the whole point of the project.
package hotkey

import (
	"bufio"
	"os"
)

// Mode is how the trigger behaves.
type Mode int

const (
	// Hold dictates while the button is held down (default).
	Hold Mode = iota
	// Toggle taps once to start and again to stop — for long-form dictation.
	Toggle
)

// Binding is a bound key or mouse button plus its interaction mode.
type Binding struct {
	Key  string // human-readable, e.g. "MouseBack", "RightCtrl"
	Mode Mode
}

// DefaultBinding is the origin story: the mouse back button, hold-to-talk.
var DefaultBinding = Binding{Key: "MouseBack", Mode: Hold}

// Trigger listens globally for the bound key/button.
type Trigger interface {
	// Start begins listening. onPress fires when the trigger goes down and
	// onRelease when it comes up. It blocks until Stop is called.
	Start(onPress, onRelease func()) error
	Stop() error
}

// StdinStub simulates a trigger from the terminal: each line (Enter) is one full
// press+release cycle. Lets the pipeline run with no native dependencies.
type StdinStub struct{}

// NewStdinStub returns a terminal-driven stub trigger.
func NewStdinStub() *StdinStub { return &StdinStub{} }

// Start treats every line read from stdin as a complete hold-and-release.
func (s *StdinStub) Start(onPress, onRelease func()) error {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		onPress()
		onRelease()
	}
	return sc.Err()
}

// Stop is a no-op for the stub.
func (s *StdinStub) Stop() error { return nil }
