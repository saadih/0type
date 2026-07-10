//go:build windows

package hotkey

import (
	"runtime"
	"syscall"
	"unsafe"
)

const (
	whMouseLL     = 14
	wmXButtonDown = 0x020B
	wmXButtonUp   = 0x020C
	wmQuit        = 0x0012
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procSetWindowsHookEx   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx     = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHook  = user32.NewProc("UnhookWindowsHookEx")
	procGetMessage         = user32.NewProc("GetMessageW")
	procPostThreadMessage  = user32.NewProc("PostThreadMessageW")
	procGetModuleHandle    = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

// MouseHook is a Windows Trigger that fires on a mouse side button, hold-to-talk:
// onPress when it goes down, onRelease when it comes up. It uses a raw
// WH_MOUSE_LL hook (no CGO) — the mechanism proven out by cmd/hookprobe.
//
// For now it triggers on any X-button (MB4 or MB5). Distinguishing them requires
// reading MSLLHOOKSTRUCT.mouseData from the hook's lParam; that lands with the
// settings UI, where the specific button becomes user-configurable. We do NOT
// use GetAsyncKeyState to tell them apart: a low-level hook fires before the
// async key-state table updates, so that check is unreliable for the very event
// being delivered (it was the bug that made holding the button do nothing).
type MouseHook struct {
	threadID uintptr
	hook     uintptr
}

// NewMouseHook returns a Windows mouse-side-button trigger.
func NewMouseHook() *MouseHook { return &MouseHook{} }

// Start installs the hook and pumps messages until Stop. It blocks, so run it on
// its own goroutine. onPress/onRelease run on the hook thread and MUST return
// quickly — a slow low-level hook callback lags the whole system's mouse and
// Windows may silently drop the hook. Do heavy work elsewhere.
func (m *MouseHook) Start(onPress, onRelease func()) error {
	runtime.LockOSThread()
	tid, _, _ := procGetCurrentThreadID.Call()
	m.threadID = tid

	// pressed is touched only from the single hook thread, so no lock is needed.
	// It pairs each side-button down with its matching up.
	var pressed bool
	callback := syscall.NewCallback(func(nCode, wParam, lParam uintptr) uintptr {
		if int32(nCode) >= 0 {
			switch wParam {
			case wmXButtonDown:
				if !pressed {
					pressed = true
					onPress()
				}
			case wmXButtonUp:
				if pressed {
					pressed = false
					onRelease()
				}
			}
		}
		ret, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
		return ret
	})

	hInstance, _, _ := procGetModuleHandle.Call(0)
	hook, _, err := procSetWindowsHookEx.Call(whMouseLL, callback, hInstance, 0)
	if hook == 0 {
		return err
	}
	m.hook = hook

	var msg [7]uintptr // oversized MSG buffer; fields are never read
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
	}
	procUnhookWindowsHook.Call(m.hook)
	return nil
}

// Stop unhooks and breaks the message loop.
func (m *MouseHook) Stop() error {
	if m.threadID != 0 {
		procPostThreadMessage.Call(m.threadID, wmQuit, 0, 0)
	}
	return nil
}
