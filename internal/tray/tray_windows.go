//go:build windows

// Package tray shows a Windows notification-area (system tray) icon with an
// Open/Quit menu, so 0type can run in the background with its window closed. It
// runs on its own OS thread with a message loop, like the overlay, and uses raw
// Shell_NotifyIcon rather than a dependency.
package tray

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wmApp      = 0x8000
	wmTrayCB   = wmApp + 1 // our Shell_NotifyIcon callback message
	wmTrayQuit = wmApp + 2 // posted by Stop to tear the window down

	wmDestroy       = 0x0002
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205

	nimAdd    = 0x00000000
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	tpmLeftAlign   = 0x0000
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	idOpen = 1
	idQuit = 2

	idiApplication = 32512
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procExtractIconW        = shell32.NewProc("ExtractIconW")
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

// notifyIconData mirrors NOTIFYICONDATAW. The full modern layout is declared so
// cbSize (set from unsafe.Sizeof) matches what the shell expects.
type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

var (
	once   sync.Once
	hwnd   uintptr
	nid    notifyIconData
	onOpen func()
	onQuit func()
	tip    string
	ready  = make(chan struct{})
)

// Start creates the tray icon on its own OS thread. openFn and quitFn run when
// the user picks Open (or left-clicks) and Quit. Safe to call once.
func Start(tooltip string, openFn, quitFn func()) {
	once.Do(func() {
		tip = tooltip
		onOpen = openFn
		onQuit = quitFn
		go run()
		<-ready
	})
}

// Stop removes the icon and tears down the tray window.
func Stop() {
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmTrayQuit, 0, 0)
	}
}

func run() {
	runtime.LockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("ZeroTypeTray")

	wc := wndclassex{
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		close(ready)
		return
	}

	// A normal but never-shown top-level window: invisible, yet eligible to be
	// the foreground window so the popup menu dismisses on an outside click.
	h, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), 0, 0,
		0, 0, 0, 0, 0, 0, hInst, 0,
	)
	if h == 0 {
		close(ready)
		return
	}
	hwnd = h

	nid = notifyIconData{
		hWnd:             h,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayCB,
		hIcon:            loadAppIcon(hInst),
	}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	if u, err := syscall.UTF16FromString(tip); err == nil {
		copy(nid.szTip[:len(nid.szTip)-1], u)
	}
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))

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
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func wndProc(h, message, wParam, lParam uintptr) uintptr {
	switch message {
	case wmTrayCB:
		switch lParam & 0xFFFF { // low word carries the mouse event
		case wmLButtonUp, wmLButtonDblClk:
			fire(onOpen)
		case wmRButtonUp:
			showMenu(h)
		}
		return 0
	case wmTrayQuit:
		procDestroyWindow.Call(h)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(h, message, wParam, lParam)
	return r
}

func showMenu(h uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendItem(menu, idOpen, "Open 0type")
	appendItem(menu, idQuit, "Quit")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(h) // required so the menu closes on outside click
	cmd, _, _ := procTrackPopupMenu.Call(
		menu, tpmLeftAlign|tpmRightButton|tpmReturnCmd,
		uintptr(pt.x), uintptr(pt.y), 0, h, 0,
	)
	switch cmd {
	case idOpen:
		fire(onOpen)
	case idQuit:
		fire(onQuit)
	}
}

func appendItem(menu, id uintptr, label string) {
	p, _ := syscall.UTF16PtrFromString(label)
	procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(p)))
}

func fire(fn func()) {
	if fn != nil {
		fn()
	}
}

// loadAppIcon prefers the running exe's own icon (set by Wails), falling back to
// the default application icon.
func loadAppIcon(hInst uintptr) uintptr {
	if exe, err := os.Executable(); err == nil {
		if p, err := syscall.UTF16PtrFromString(exe); err == nil {
			if ic, _, _ := procExtractIconW.Call(hInst, uintptr(unsafe.Pointer(p)), 0); ic > 1 {
				return ic
			}
		}
	}
	ic, _, _ := procLoadIconW.Call(0, idiApplication)
	return ic
}
