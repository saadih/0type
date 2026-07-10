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

	vkXButton1 = 0x05 // "back" side button (MB4) — the default trigger
	keyDownBit = 0x8000
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procSetWindowsHookEx   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx     = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHook  = user32.NewProc("UnhookWindowsHookEx")
	procGetMessage         = user32.NewProc("GetMessageW")
	procPostThreadMessage  = user32.NewProc("PostThreadMessageW")
	procGetAsyncKeyState   = user32.NewProc("GetAsyncKeyState")
	procGetModuleHandle    = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

// isDown reports whether the given virtual-key is currently pressed.
func isDown(vk uintptr) bool {
	r, _, _ := procGetAsyncKeyState.Call(vk)
	return r&keyDownBit != 0
}

// MouseHook is a Windows Trigger that fires on the mouse back button (MB4),
// hold-to-talk: onPress when it goes down, onRelease when it comes up. It uses a
// raw WH_MOUSE_LL hook (no CGO) — the mechanism proven out by cmd/hookprobe.
type MouseHook struct {
	threadID uintptr
	hook     uintptr
}

// NewMouseHook returns a Windows mouse-back-button trigger.
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
	var pressed bool
	callback := syscall.NewCallback(func(nCode, wParam, lParam uintptr) uintptr {
		if int32(nCode) >= 0 {
			switch wParam {
			case wmXButtonDown:
				// isDown(MB4) distinguishes the back button from the forward one.
				if !pressed && isDown(vkXButton1) {
					pressed = true
					onPress()
				}
			case wmXButtonUp:
				// MB4 is no longer down => it's the button we care about releasing.
				if pressed && !isDown(vkXButton1) {
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
