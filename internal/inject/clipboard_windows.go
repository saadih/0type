//go:build windows

package inject

import (
	"fmt"
	"sync"
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

// restoreDelay is how long the target app gets to consume the paste before the
// user's own clipboard goes back. It is waited out in the background, after the
// last paste of a burst, so it costs the output worker nothing.
const restoreDelay = 200 * time.Millisecond

// ClipboardPaster injects text by writing it to the clipboard and sending
// Ctrl+V, then restoring the previous clipboard. This is faster and far more
// Unicode-safe (Swedish å/ä/ö, emoji) than simulating each keystroke.
//
// A dictation arrives as a burst of segments, so the restore is deferred rather
// than awaited: Inject returns as soon as the keystroke is sent, and the user's
// clipboard comes back once the pastes stop. Waiting inline cost 120 ms of
// sleep per segment on the output worker, delaying the next segment's cleanup;
// the clipboard calls themselves take well under a millisecond.
//
// Inject must be driven from a single goroutine (0type's output worker): the
// clipboard is a shared global and set-then-paste is not atomic, so concurrent
// callers would clobber each other. mu additionally keeps a pending restore
// from landing in the middle of one. Non-text clipboard contents (images,
// files) can't be preserved and are best-effort only.
type ClipboardPaster struct {
	mu    sync.Mutex
	saved string // what the user had on the clipboard before this burst
	held  bool   // saved is valid and still owed back
	gen   uint64 // paste counter, so only the newest paste's restore runs
}

// NewClipboardPaster returns a clipboard-paste injector.
func NewClipboardPaster() *ClipboardPaster { return &ClipboardPaster{} }

// Inject sets the clipboard to text and pastes it into the focused app. It
// returns without waiting; the user's clipboard is restored in the background
// once restoreDelay passes with no further paste.
func (c *ClipboardPaster) Inject(text string) error {
	c.mu.Lock()
	if !c.held {
		// First paste of this burst: remember what the user had. Best effort,
		// empty if the clipboard holds something that isn't text.
		c.saved, _ = clipboard.ReadAll()
		c.held = true
	}
	c.gen++
	gen := c.gen
	err := clipboard.WriteAll(text)
	if err == nil {
		err = sendCtrlV()
	}
	c.mu.Unlock()
	// Schedule the restore either way: once WriteAll has run the clipboard may
	// already hold the dictated text, and the user's contents are still owed.
	time.AfterFunc(restoreDelay, func() { c.restore(gen) })
	return err
}

// restore puts the user's clipboard back, unless a later paste superseded this
// one — that paste schedules its own restore.
func (c *ClipboardPaster) restore(gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.held || gen != c.gen {
		return
	}
	if c.saved != "" {
		_ = clipboard.WriteAll(c.saved)
	}
	c.saved, c.held = "", false
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
		// A partial batch can leave Ctrl held; force it up so the modifier
		// doesn't stick for the user's next real keystroke.
		up := keyEvent(vkControl, true)
		procSendInput.Call(1, uintptr(unsafe.Pointer(&up)), unsafe.Sizeof(up))
		return fmt.Errorf("sendinput injected %d of %d events: %w", n, len(events), err)
	}
	return nil
}

// Default returns the platform injector: clipboard-paste on Windows.
func Default() Injector { return NewClipboardPaster() }
