//go:build windows

package hotkey

// New returns the Windows push-to-talk controller for the given binding.
func New(b Binding) Controller { return newController(b) }
