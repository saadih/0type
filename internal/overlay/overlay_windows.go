//go:build windows

// Package overlay shows a small always-on-top "recording" pill that floats near
// the cursor over whatever app you're dictating into. It is deliberately
// focus-safe (WS_EX_NOACTIVATE | WS_EX_TRANSPARENT): it must never steal focus,
// or the paste would land on the overlay instead of the user's document.
package overlay

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wsPopup = 0x80000000

	wsExTopmost     = 0x00000008
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000

	swHide           = 0
	swShowNoActivate = 4

	lwaAlpha = 0x00000002

	wmShow  = 0x8000 + 1 // WM_APP+1; wParam 1 = show, 0 = hide
	wmTimer = 0x0113

	swpNoSize     = 0x0001
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	pulseTimerID = 1

	pillW = 116
	pillH = 30
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostMessageW       = user32.NewProc("PostMessageW")
	procSetLayeredAttrs    = user32.NewProc("SetLayeredWindowAttributes")
	procSetWindowRgn       = user32.NewProc("SetWindowRgn")
	procGetCursorPos       = user32.NewProc("GetCursorPos")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procSetTimer           = user32.NewProc("SetTimer")
	procKillTimer          = user32.NewProc("KillTimer")
	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
)

type wndclassex struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

var (
	once  sync.Once
	hwnd  uintptr
	ready = make(chan struct{})
	pulse bool
)

// rgb builds a COLORREF (0x00BBGGRR).
func rgb(r, g, b uint32) uintptr { return uintptr(r | g<<8 | b<<16) }

// Start creates the overlay window on its own OS thread. Safe to call once; it
// blocks until the window exists (or fails, in which case Show is a no-op).
func Start() {
	once.Do(func() {
		go run()
		<-ready
	})
}

// Show shows (recording) or hides the pill. Safe to call from any goroutine.
func Show(recording bool) {
	if hwnd == 0 {
		return
	}
	var w uintptr
	if recording {
		w = 1
	}
	procPostMessageW.Call(hwnd, wmShow, w, 0)
}

func wndProc(h, message, wParam, lParam uintptr) uintptr {
	switch message {
	case wmShow:
		if wParam == 1 {
			var pt point
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
			procSetWindowPos.Call(h, ^uintptr(0), // HWND_TOPMOST
				uintptr(pt.x+16), uintptr(pt.y+20), 0, 0,
				swpNoSize|swpNoActivate|swpShowWindow)
			procSetTimer.Call(h, pulseTimerID, 450, 0)
			procSetLayeredAttrs.Call(h, 0, 235, lwaAlpha)
			pulse = true
		} else {
			procKillTimer.Call(h, pulseTimerID)
			procShowWindow.Call(h, swHide)
		}
		return 0
	case wmTimer:
		// Breathing flair: fade the pill between bright and dim.
		pulse = !pulse
		alpha := uintptr(150)
		if pulse {
			alpha = 235
		}
		procSetLayeredAttrs.Call(h, 0, alpha, lwaAlpha)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(h, message, wParam, lParam)
	return r
}

func run() {
	runtime.LockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("ZeroTypeOverlay")
	brush, _, _ := procCreateSolidBrush.Call(rgb(235, 66, 66)) // recording red

	wc := wndclassex{
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		hbrBackground: brush,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		close(ready)
		return
	}

	exStyle := uintptr(wsExTopmost | wsExToolWindow | wsExLayered | wsExTransparent | wsExNoActivate)
	h, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(className)),
		0,
		wsPopup,
		0, 0, pillW, pillH,
		0, 0, hInst, 0,
	)
	if h == 0 {
		close(ready)
		return
	}

	// Rounded pill; alpha is set per-show so the breathing starts fresh.
	if rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, pillW+1, pillH+1, pillH, pillH); rgn != 0 {
		procSetWindowRgn.Call(h, rgn, 1)
	}
	procSetLayeredAttrs.Call(h, 0, 235, lwaAlpha)

	hwnd = h
	close(ready)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
