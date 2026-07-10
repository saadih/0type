//go:build windows

package hotkey

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	whMouseLL    = 14
	whKeyboardLL = 13

	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105

	wmXButtonDown = 0x020B
	wmXButtonUp   = 0x020C
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208

	xbutton2 = 0x0002
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procSetWindowsHookEx  = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx    = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHook = user32.NewProc("UnhookWindowsHookEx")
	procGetMessage        = user32.NewProc("GetMessageW")
	procGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")

	// Callbacks are created once for the process (Windows caps the number of
	// syscall callbacks), and read the single active manager.
	mouseCB uintptr
	kbCB    uintptr
	active  *manager
)

// kbdllhookstruct / msllhookstruct mirror the Win32 structs the low-level hooks
// receive via lParam. Reading them requires an unsafe.Pointer(uintptr)
// conversion that go vet flags — it is the necessary, standard WinAPI idiom.
type kbdllhookstruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

type point struct{ x, y int32 }

type msllhookstruct struct {
	pt          point
	mouseData   uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

type manager struct {
	mu        sync.Mutex
	target    Binding
	onPress   func()
	onRelease func()
	capture   chan Binding
	pressed   bool
	mouseHook uintptr
	kbHook    uintptr
}

func newController(b Binding) Controller { return &manager{target: b} }

func (m *manager) Start(onPress, onRelease func()) error {
	m.mu.Lock()
	m.onPress = onPress
	m.onRelease = onRelease
	m.mu.Unlock()
	errc := make(chan error, 1)
	go m.run(errc)
	return <-errc
}

func (m *manager) SetBinding(b Binding) {
	m.mu.Lock()
	m.target = b
	m.pressed = false
	m.mu.Unlock()
}

func (m *manager) Capture() (Binding, error) {
	ch := make(chan Binding, 1)
	m.mu.Lock()
	m.capture = ch
	m.pressed = false
	m.mu.Unlock()

	b := <-ch

	m.mu.Lock()
	m.capture = nil
	m.mu.Unlock()
	return b, nil
}

// dispatch decides what to do with an input event and returns true to suppress
// it (only while capturing a new binding). Runs on the hook thread.
func (m *manager) dispatch(b Binding, down bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.capture != nil {
		if down {
			select {
			case m.capture <- b:
			default:
			}
		}
		return true // swallow the key/button while capturing
	}
	if b.Kind != m.target.Kind || b.Code != m.target.Code {
		return false
	}
	if down {
		if !m.pressed {
			m.pressed = true
			if m.onPress != nil {
				m.onPress()
			}
		}
	} else if m.pressed {
		m.pressed = false
		if m.onRelease != nil {
			m.onRelease()
		}
	}
	return false
}

func (m *manager) handleMouse(wParam, lParam uintptr) bool {
	var code int
	var down bool
	switch wParam {
	case wmXButtonDown, wmXButtonUp:
		info := (*msllhookstruct)(unsafe.Pointer(lParam))
		if (info.mouseData>>16)&0xFFFF == xbutton2 {
			code = 5
		} else {
			code = 4
		}
		down = wParam == wmXButtonDown
	case wmMButtonDown, wmMButtonUp:
		code = 3
		down = wParam == wmMButtonDown
	default:
		return false
	}
	return m.dispatch(Binding{Kind: "mouse", Code: code, Name: mouseName(code)}, down)
}

func (m *manager) handleKey(wParam, lParam uintptr) bool {
	var down bool
	switch wParam {
	case wmKeyDown, wmSysKeyDown:
		down = true
	case wmKeyUp, wmSysKeyUp:
		down = false
	default:
		return false
	}
	info := (*kbdllhookstruct)(unsafe.Pointer(lParam))
	vk := int(info.vkCode)
	return m.dispatch(Binding{Kind: "key", Code: vk, Name: keyName(vk)}, down)
}

func mouseProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) >= 0 && active != nil && active.handleMouse(wParam, lParam) {
		return 1
	}
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

func keyProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) >= 0 && active != nil && active.handleKey(wParam, lParam) {
		return 1
	}
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

func (m *manager) run(errc chan error) {
	runtime.LockOSThread()
	hInst, _, _ := procGetModuleHandle.Call(0)
	if mouseCB == 0 {
		mouseCB = syscall.NewCallback(mouseProc)
		kbCB = syscall.NewCallback(keyProc)
	}
	active = m

	mh, _, err := procSetWindowsHookEx.Call(whMouseLL, mouseCB, hInst, 0)
	if mh == 0 {
		errc <- fmt.Errorf("mouse hook: %w", err)
		return
	}
	kh, _, err := procSetWindowsHookEx.Call(whKeyboardLL, kbCB, hInst, 0)
	if kh == 0 {
		procUnhookWindowsHook.Call(mh)
		errc <- fmt.Errorf("keyboard hook: %w", err)
		return
	}
	m.mouseHook = mh
	m.kbHook = kh
	errc <- nil

	var msg [7]uintptr
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
	}
	procUnhookWindowsHook.Call(mh)
	procUnhookWindowsHook.Call(kh)
}
