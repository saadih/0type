//go:build windows

package inject

import (
	"syscall"
	"time"
	"unsafe"

	"github.com/atotto/clipboard"
)

var (
	user32        = syscall.NewLazyDLL("user32.dll")
	procSendInput = user32.NewProc("SendInput")
)

const (
	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
	vkControl      = 0x11
	vkV            = 0x56
)

type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
	_           uint64 // pad KEYBDINPUT up to the INPUT union size on amd64
}

type input struct {
	inputType uint32
	_         uint32 // align the union on an 8-byte boundary
	ki        keybdInput
}

// INPUT must be 40 bytes on amd64 (DWORD type + padding + the 32-byte union).
// SendInput silently rejects the call if cbSize is wrong, so assert the layout
// at compile time — either line fails to build if the size drifts.
const _ = uint(unsafe.Sizeof(input{}) - 40)
const _ = uint(40 - unsafe.Sizeof(input{}))

// ClipboardPaster injects text by writing it to the clipboard and sending
// Ctrl+V, then restoring the previous clipboard. This is faster and far more
// Unicode-safe (Swedish å/ä/ö, emoji) than simulating each keystroke.
type ClipboardPaster struct{}

// NewClipboardPaster returns a clipboard-paste injector.
func NewClipboardPaster() *ClipboardPaster { return &ClipboardPaster{} }

// Inject sets the clipboard to text, pastes it into the focused app, then
// restores whatever was on the clipboard before.
func (c *ClipboardPaster) Inject(text string) error {
	prev, _ := clipboard.ReadAll() // best effort; empty if the clipboard isn't text
	if err := clipboard.WriteAll(text); err != nil {
		return err
	}
	if err := sendCtrlV(); err != nil {
		return err
	}
	// Let the target app consume the paste before restoring: put the old text
	// back too early and it pastes the previous contents instead.
	time.Sleep(120 * time.Millisecond)
	if prev != "" {
		_ = clipboard.WriteAll(prev)
	}
	return nil
}

func keyEvent(vk uint16, keyUp bool) input {
	in := input{inputType: inputKeyboard, ki: keybdInput{wVk: vk}}
	if keyUp {
		in.ki.dwFlags = keyeventfKeyUp
	}
	return in
}

// sendCtrlV synthesizes a Ctrl+V keystroke via SendInput.
func sendCtrlV() error {
	events := []input{
		keyEvent(vkControl, false),
		keyEvent(vkV, false),
		keyEvent(vkV, true),
		keyEvent(vkControl, true),
	}
	n, _, err := procSendInput.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		unsafe.Sizeof(events[0]),
	)
	if int(n) != len(events) {
		return err
	}
	return nil
}

// Default returns the platform injector: clipboard-paste on Windows.
func Default() Injector { return NewClipboardPaster() }
