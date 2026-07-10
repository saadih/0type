// Package hotkey installs the global push-to-talk trigger and supports live
// rebinding to any key or mouse side/middle button, plus one-shot capture for
// the settings UI.
package hotkey

import (
	"encoding/json"
	"fmt"
)

// Binding identifies the push-to-talk input.
type Binding struct {
	Kind string `json:"kind"` // "mouse" | "key"
	Code int    `json:"code"` // mouse: 4=back, 5=forward, 3=middle; key: Windows virtual-key code
	Name string `json:"name"` // human-readable, e.g. "Mouse Back", "Right Ctrl"
}

// DefaultBinding is the mouse back button (MB4) — the project's origin story.
func DefaultBinding() Binding { return Binding{Kind: "mouse", Code: 4, Name: "Mouse Back"} }

// Valid reports whether b names a real trigger.
func (b Binding) Valid() bool { return b.Kind == "mouse" || b.Kind == "key" }

// UnmarshalJSON tolerates the legacy string trigger form ("MouseBack") by
// falling back to the default, so old configs still load their other fields.
func (b *Binding) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		*b = DefaultBinding()
		return nil
	}
	type raw Binding
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*b = Binding(r)
	return nil
}

// Controller installs the global trigger, supports live rebinding, and can
// capture the next input for the rebind UI.
type Controller interface {
	// Start installs the global hook(s); onPress/onRelease fire when the current
	// binding is held/released. It returns once the hooks are installed.
	Start(onPress, onRelease func()) error
	// SetBinding changes the active trigger live (no reinstall).
	SetBinding(b Binding)
	// Capture blocks until the user presses any key or mouse side/middle button
	// and returns it as a Binding.
	Capture() (Binding, error)
}

func mouseName(code int) string {
	switch code {
	case 4:
		return "Mouse Back"
	case 5:
		return "Mouse Forward"
	case 3:
		return "Mouse Middle"
	}
	return "Mouse Button"
}

// keyName maps a Windows virtual-key code to a display name; unknown keys fall
// back to a hex code.
func keyName(vk int) string {
	if n, ok := keyNames[vk]; ok {
		return n
	}
	if (vk >= 'A' && vk <= 'Z') || (vk >= '0' && vk <= '9') {
		return string(rune(vk))
	}
	return fmt.Sprintf("Key 0x%02X", vk)
}

var keyNames = map[int]string{
	0x08: "Backspace", 0x09: "Tab", 0x0D: "Enter", 0x1B: "Esc", 0x20: "Space",
	0x14: "Caps Lock", 0x2D: "Insert", 0x2E: "Delete", 0x24: "Home", 0x23: "End",
	0x21: "Page Up", 0x22: "Page Down", 0x25: "Left", 0x26: "Up", 0x27: "Right", 0x28: "Down",
	0xA0: "Left Shift", 0xA1: "Right Shift", 0xA2: "Left Ctrl", 0xA3: "Right Ctrl",
	0xA4: "Left Alt", 0xA5: "Right Alt", 0x5B: "Left Win", 0x5C: "Right Win",
	0x70: "F1", 0x71: "F2", 0x72: "F3", 0x73: "F4", 0x74: "F5", 0x75: "F6",
	0x76: "F7", 0x77: "F8", 0x78: "F9", 0x79: "F10", 0x7A: "F11", 0x7B: "F12",
}
