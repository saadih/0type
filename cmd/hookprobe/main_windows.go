//go:build windows

// Command hookprobe verifies the load-bearing assumption behind 0type: that the
// OS reports mouse side buttons (MB4/MB5) to a global low-level hook on Windows.
// If these events don't come through, the whole "bind dictation to your mouse
// button" premise fails — so this gets proven before anything else is built.
//
// It installs a raw WH_MOUSE_LL hook (the same mechanism robotgo/libuiohook use
// on Windows) with zero external dependencies, so it builds offline with just
// the Go toolchain. Run it, then click every mouse button — especially your
// thumb buttons — and watch what prints.
package main

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	whMouseLL = 14

	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208
	wmMouseWheel  = 0x020A
	wmXButtonDown = 0x020B
	wmXButtonUp   = 0x020C

	vkXButton1 = 0x05 // "back"    side button (MB4)
	vkXButton2 = 0x06 // "forward" side button (MB5)
	keyDownBit = 0x8000
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procSetWindowsHookEx  = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx    = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHook = user32.NewProc("UnhookWindowsHookEx")
	procGetMessage        = user32.NewProc("GetMessageW")
	procGetAsyncKeyState  = user32.NewProc("GetAsyncKeyState")
	procGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")
)

// isDown reports whether the given virtual-key is currently pressed.
func isDown(vk uintptr) bool {
	r, _, _ := procGetAsyncKeyState.Call(vk)
	return r&keyDownBit != 0
}

// lastSide remembers which side button went down, so the matching "up" can be
// labelled the same way.
var lastSide = "side button"

// describe labels a mouse-button message, or returns "" for events we ignore.
//
// It identifies which side button fired via GetAsyncKeyState rather than reading
// the OS-supplied lParam struct — so the probe stays free of uintptr->pointer
// conversions (and the go vet warning they trigger). The real hotkey backend can
// read MSLLHOOKSTRUCT.mouseData directly when it needs to.
func describe(wParam uintptr) string {
	switch wParam {
	case wmLButtonDown:
		return "Left        down"
	case wmLButtonUp:
		return "Left        up"
	case wmRButtonDown:
		return "Right       down"
	case wmRButtonUp:
		return "Right       up"
	case wmMButtonDown:
		return "Middle      down"
	case wmMButtonUp:
		return "Middle      up"
	case wmXButtonDown:
		if isDown(vkXButton2) {
			lastSide = "MB5 (fwd) "
		} else {
			lastSide = "MB4 (back)"
		}
		return fmt.Sprintf("%s  down   <-- side button!", lastSide)
	case wmXButtonUp:
		return fmt.Sprintf("%s  up     <-- side button!", lastSide)
	}
	return ""
}

func main() {
	// Low-level hooks must be installed and pumped on a single OS thread.
	runtime.LockOSThread()

	callback := syscall.NewCallback(func(nCode, wParam, lParam uintptr) uintptr {
		if int32(nCode) >= 0 && wParam != wmMouseMove && wParam != wmMouseWheel {
			if label := describe(wParam); label != "" {
				fmt.Println(label)
			}
		}
		ret, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
		return ret
	})

	hInstance, _, _ := procGetModuleHandle.Call(0)
	hook, _, err := procSetWindowsHookEx.Call(whMouseLL, callback, hInstance, 0)
	if hook == 0 {
		fmt.Println("SetWindowsHookEx failed:", err)
		return
	}
	defer procUnhookWindowsHook.Call(hook)

	fmt.Println("hookprobe - click every mouse button, especially your thumb buttons. Ctrl+C to quit.")

	// Pump messages so the system can dispatch the hook on this thread.
	var msg [7]uintptr // oversized buffer for MSG; fields are never read
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
	}
}
