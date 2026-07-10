//go:build windows

package hotkey

// Default returns the platform push-to-talk trigger: the mouse back button (MB4)
// on Windows.
func Default() Trigger { return NewMouseHook() }
