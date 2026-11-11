//go:build windows

package desktop

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// UIActions keeps the native window independent of the server supervisor.
type UIActions struct {
	InitialDirectory string
	OnBrowse         func(owner uintptr) (string, error)
	OnOpenPanel      func() error
	OnOpenLogs       func() error
	OnExit           func()
	OnStatus         func() (status string, directory string)
}

var (
	uiUser32   = syscall.NewLazyDLL("user32.dll")
	uiKernel32 = syscall.NewLazyDLL("kernel32.dll")
	uiShell32  = syscall.NewLazyDLL("shell32.dll")
	uiOle32    = syscall.NewLazyDLL("ole32.dll")
	uiGdi32    = syscall.NewLazyDLL("gdi32.dll")
	uiWndProc  = syscall.NewCallback(windowProcedure)
	uiCurrent  *nativeWindow
)

const (
	uiClassName = "InternetStabilityClientWindow"
	uiTitle     = "Connection Stability"
	wmAppShow   = 0x8001
	wmAppStatus = 0x8002
	wmAppExit   = 0x8003
	wmAppError  = 0x8004
	wmTray      = 0x8010
	idBrowse    = 1001
	idPanel     = 1002
	idHide      = 1003
	idTrayShow  = 1101
	idTrayPanel = 1102
	idTrayLogs  = 1103
	idTrayExit  = 1104
)

type uiPoint struct {
	X int32
	Y int32
}

type uiRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type uiMessage struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      uiPoint
	Private uint32
}

type uiWindowClass struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	SmallIcon  uintptr
}

type nativeWindow struct {
	actions      UIActions
	hwnd         uintptr
	instance     uintptr
	font         uintptr
	headingFont  uintptr
	icon         uintptr
	directory    uintptr
	status       uintptr
	browse       uintptr
	panel        uintptr
	hide         uintptr
	trayAdded    bool
	exiting      bool
	taskbarEvent uint32
	statusMu     sync.Mutex
	statusBusy   bool
	statusText   string
	statusDir    string
	asyncError   string
}

func uiString(text string) *uint16 {
	value, _ := syscall.UTF16PtrFromString(text)

	return value
}

func uiSigned(value int32) uintptr {
	return uintptr(int64(value))
}

func uiText(hwnd uintptr, text string) {
	uiUser32.NewProc("SetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(uiString(text))))
}

func uiMessageBox(owner uintptr, text string) {
	uiUser32.NewProc("MessageBoxW").Call(owner, uintptr(unsafe.Pointer(uiString(text))), uintptr(unsafe.Pointer(uiString(uiTitle))), 0x10)
}

// ShowClientWindow is also used when the named single-instance mutex already exists.
func ShowClientWindow() bool {
	hwnd, _, _ := uiUser32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(uiString(uiClassName))), 0)
	if hwnd == 0 {
		return false
	}

	uiUser32.NewProc("PostMessageW").Call(hwnd, wmAppShow, 0, 0)
	uiUser32.NewProc("SetForegroundWindow").Call(hwnd)

	return true
}

func (w *nativeWindow) child(class, text string, style uint32, x, y, width, height int32, id uintptr, font uintptr) uintptr {
	hwnd, _, _ := uiUser32.NewProc("CreateWindowExW").Call(0,
		uintptr(unsafe.Pointer(uiString(class))), uintptr(unsafe.Pointer(uiString(text))), uintptr(style|0x50000000),
		uiSigned(x), uiSigned(y), uiSigned(width), uiSigned(height), w.hwnd, id, w.instance, 0)
	uiUser32.NewProc("SendMessageW").Call(hwnd, 0x30, font, 1)

	return hwnd
}

func (w *nativeWindow) show() {
	uiUser32.NewProc("ShowWindow").Call(w.hwnd, 9)
	uiUser32.NewProc("ShowWindow").Call(w.hwnd, 5)
	uiUser32.NewProc("SetForegroundWindow").Call(w.hwnd)
}

func (w *nativeWindow) hideWindow() {
	// Never make the application unreachable if Explorer could not add the icon.
	if !w.trayAdded {
		w.addTray()
	}

	if w.trayAdded {
		uiUser32.NewProc("ShowWindow").Call(w.hwnd, 0)
	} else {
		uiMessageBox(w.hwnd, "Could not add the notification area icon. The window will remain open.")
	}
}

func (w *nativeWindow) runAction(action func() error) {
	if action == nil || w.exiting {
		return
	}

	go func() {
		if err := action(); err != nil {
			w.statusMu.Lock()
			w.asyncError = err.Error()
			w.statusMu.Unlock()
			uiUser32.NewProc("PostMessageW").Call(w.hwnd, wmAppError, 0, 0)
		}
	}()
}

func (w *nativeWindow) pollStatus() {
	if w.exiting || w.actions.OnStatus == nil {
		return
	}

	w.statusMu.Lock()
	if w.statusBusy {
		w.statusMu.Unlock()

		return
	}

	w.statusBusy = true
	w.statusMu.Unlock()
	go func() {
		status, directory := w.actions.OnStatus()
		w.statusMu.Lock()
		w.statusText, w.statusDir, w.statusBusy = status, directory, false
		w.statusMu.Unlock()
		uiUser32.NewProc("PostMessageW").Call(w.hwnd, wmAppStatus, 0, 0)
	}()
}

func (w *nativeWindow) beginExit() {
	if w.exiting {
		return
	}

	w.exiting = true
	uiText(w.status, "Stopping the server and running checks…")
	for _, control := range []uintptr{w.browse, w.panel, w.hide} {
		uiUser32.NewProc("EnableWindow").Call(control, 0)
	}

	go func() {
		if w.actions.OnExit != nil {
			w.actions.OnExit()
		}

		uiUser32.NewProc("PostMessageW").Call(w.hwnd, wmAppExit, 0, 0)
	}()
}

func (w *nativeWindow) command(id uintptr) {
	if w.exiting {
		return
	}

	switch id {
	case idBrowse:
		w.browseDirectory()
	case idPanel, idTrayPanel:
		w.runAction(w.actions.OnOpenPanel)
	case idHide:
		w.hideWindow()
	case idTrayShow:
		w.show()
	case idTrayLogs:
		w.runAction(w.actions.OnOpenLogs)
	case idTrayExit:
		w.beginExit()
	}
}

func (w *nativeWindow) browseDirectory() {
	if w.actions.OnBrowse == nil {
		return
	}

	uiUser32.NewProc("EnableWindow").Call(w.browse, 0)
	directory, err := w.actions.OnBrowse(w.hwnd)
	uiUser32.NewProc("EnableWindow").Call(w.browse, 1)
	if err != nil {
		uiMessageBox(w.hwnd, err.Error())
	} else if directory != "" {
		uiText(w.directory, directory)
	}

	w.pollStatus()
}

func (w *nativeWindow) applyStatus() {
	if w.exiting {
		return
	}

	w.statusMu.Lock()
	status, directory := w.statusText, w.statusDir
	w.statusMu.Unlock()
	uiText(w.status, status)
	if directory != "" {
		uiText(w.directory, directory)
	}
}

func (w *nativeWindow) showPendingError(owner uintptr) {
	if w.exiting {
		return
	}

	w.statusMu.Lock()
	text := w.asyncError
	w.statusMu.Unlock()
	w.show()
	uiMessageBox(owner, text)
}

func windowProcedure(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	w := uiCurrent
	if w != nil {
		if message == w.taskbarEvent && w.taskbarEvent != 0 {
			w.addTray()

			return 0
		}

		switch message {
		case 0x138: // WM_CTLCOLORSTATIC, also used by the read-only path field.
			uiGdi32.NewProc("SetTextColor").Call(wparam, 0x00382d20)
			uiGdi32.NewProc("SetBkColor").Call(wparam, 0x00ffffff)
			brush, _, _ := uiGdi32.NewProc("GetStockObject").Call(0)

			return brush
		case 0x10: // WM_CLOSE: the tray owns application lifetime.
			w.hideWindow()

			return 0
		case 0x5: // WM_SIZE / SIZE_MINIMIZED
			if wparam == 1 {
				w.hideWindow()

				return 0
			}
		case 0x111: // WM_COMMAND
			w.command(wparam & 0xffff)

			return 0
		case 0x113: // WM_TIMER
			w.pollStatus()

			return 0
		case wmAppShow:
			w.show()

			return 0
		case wmAppStatus:
			w.applyStatus()

			return 0
		case wmAppError:
			w.showPendingError(hwnd)

			return 0
		case wmAppExit:
			uiUser32.NewProc("DestroyWindow").Call(hwnd)

			return 0
		case wmTray:
			w.trayMessage(uint32(lparam))

			return 0
		case 0x11: // WM_QUERYENDSESSION
			w.beginExit()

			return 1
		case 0x16: // WM_ENDSESSION
			if wparam != 0 {
				w.beginExit()
			}

			return 0
		case 0x2: // WM_DESTROY
			uiUser32.NewProc("KillTimer").Call(hwnd, 1)
			w.removeTray()
			uiUser32.NewProc("PostQuitMessage").Call(0)

			return 0
		}
	}

	result, _, _ := uiUser32.NewProc("DefWindowProcW").Call(hwnd, uintptr(message), wparam, lparam)

	return result
}

func uiMakeFont(height, weight int32) uintptr {
	font, _, _ := uiGdi32.NewProc("CreateFontW").Call(uiSigned(-height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(uiString("Segoe UI"))))

	return font
}

// A compact code-drawn network icon avoids an external icon file in the portable build.
func uiMakeIcon(instance uintptr) uintptr {
	const size = 32
	andMask := make([]byte, size*size/8)
	pixels := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			pixelIndex := (y*size + x) * 4
			inside := x >= 2 && x < 30 && y >= 2 && y < 30
			if (x < 5 || x > 26) && (y < 5 || y > 26) {
				inside = false
			}

			if !inside {
				andMask[y*4+x/8] |= 0x80 >> (x % 8)
				continue
			}

			pixels[pixelIndex], pixels[pixelIndex+1], pixels[pixelIndex+2], pixels[pixelIndex+3] = 57, 38, 20, 255
			// Connected nodes and links, legible at 16x16 as well.
			node := (x >= 12 && x <= 19 && y >= 7 && y <= 12) ||
				(x >= 5 && x <= 11 && y >= 21 && y <= 26) ||
				(x >= 21 && x <= 27 && y >= 21 && y <= 26)
			link := (x >= 15 && x <= 16 && y >= 12 && y <= 18) ||
				(x >= 8 && x <= 24 && y >= 17 && y <= 18) ||
				((x >= 8 && x <= 9 || x >= 23 && x <= 24) && y >= 17 && y <= 21)
			if node {
				pixels[pixelIndex], pixels[pixelIndex+1], pixels[pixelIndex+2] = 255, 236, 206
			}

			if link {
				pixels[pixelIndex], pixels[pixelIndex+1], pixels[pixelIndex+2] = 240, 190, 91
			}
		}
	}

	icon, _, _ := uiUser32.NewProc("CreateIcon").Call(instance, size, size, 1, 32, uintptr(unsafe.Pointer(&andMask[0])), uintptr(unsafe.Pointer(&pixels[0])))
	if icon == 0 {
		icon, _, _ = uiUser32.NewProc("LoadIconW").Call(0, 32512)
	}

	return icon
}

func RunWindow(actions UIActions) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// COM apartment is required by the native folder dialog.
	hr, _, _ := uiOle32.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) >= 0 {
		defer uiOle32.NewProc("CoUninitialize").Call()
	}
	// Match the system-aware manifest: Windows scales the complete fixed-size
	// window when it moves to a monitor with a different pixel density.
	uiUser32.NewProc("SetProcessDPIAware").Call()
	instance, _, _ := uiKernel32.NewProc("GetModuleHandleW").Call(0)
	w := &nativeWindow{actions: actions, instance: instance}
	uiCurrent = w
	defer func() {
		uiCurrent = nil
	}()
	w.icon = uiMakeIcon(instance)
	defer uiUser32.NewProc("DestroyIcon").Call(w.icon)
	cursor, _, _ := uiUser32.NewProc("LoadCursorW").Call(0, 32512)
	class := uiWindowClass{
		WndProc:    uiWndProc,
		Instance:   instance,
		Icon:       w.icon,
		SmallIcon:  w.icon,
		Cursor:     cursor,
		Background: 6,
		ClassName:  uiString(uiClassName),
	}
	class.Size = uint32(unsafe.Sizeof(class))
	registered, _, classErr := uiUser32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	if registered == 0 {
		return fmt.Errorf("Could not create the window: %w", classErr)
	}

	defer uiUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(class.ClassName)), instance)
	dpi := uintptr(96)
	if proc := uiUser32.NewProc("GetDpiForSystem"); proc.Find() == nil {
		if value, _, _ := proc.Call(); value > 0 {
			dpi = value
		}
	}

	scale := func(value int32) int32 {
		return value * int32(dpi) / 96
	}
	w.font, w.headingFont = uiMakeFont(scale(14), 400), uiMakeFont(scale(21), 600)
	defer uiGdi32.NewProc("DeleteObject").Call(w.font)
	defer uiGdi32.NewProc("DeleteObject").Call(w.headingFont)
	const windowStyle = 0x00ca0000 // caption, system menu and minimize; no maximize.
	rect := uiRect{Right: scale(584), Bottom: scale(284)}
	uiUser32.NewProc("AdjustWindowRectEx").Call(uintptr(unsafe.Pointer(&rect)), windowStyle, 0, 0)
	screenWidth, _, _ := uiUser32.NewProc("GetSystemMetrics").Call(0)
	screenHeight, _, _ := uiUser32.NewProc("GetSystemMetrics").Call(1)
	width, height := rect.Right-rect.Left, rect.Bottom-rect.Top
	hwnd, _, windowErr := uiUser32.NewProc("CreateWindowExW").Call(0,
		uintptr(unsafe.Pointer(class.ClassName)), uintptr(unsafe.Pointer(uiString(uiTitle))), windowStyle,
		uiSigned((int32(screenWidth)-width)/2), uiSigned((int32(screenHeight)-height)/2), uiSigned(width), uiSigned(height), 0, 0, instance, 0)
	if hwnd == 0 {
		return fmt.Errorf("Could not create the window: %w", windowErr)
	}

	w.hwnd = hwnd
	defer w.removeTray()
	defer func() {
		if alive, _, _ := uiUser32.NewProc("IsWindow").Call(hwnd); alive != 0 {
			uiUser32.NewProc("DestroyWindow").Call(hwnd)
		}
	}()
	w.createControls(scale)
	taskbarEvent, _, _ := uiUser32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(uiString("TaskbarCreated"))))
	w.taskbarEvent = uint32(taskbarEvent)
	w.addTray()
	uiUser32.NewProc("SetTimer").Call(hwnd, 1, 1000, 0)
	uiUser32.NewProc("ShowWindow").Call(hwnd, 5)
	uiUser32.NewProc("UpdateWindow").Call(hwnd)
	uiUser32.NewProc("SetFocus").Call(w.panel)
	w.pollStatus()

	return runWindowMessages(hwnd)
}

func (w *nativeWindow) createControls(scale func(int32) int32) {
	child := func(class, text string, style uint32, x, y, width, height int32, id uintptr, font uintptr) uintptr {
		return w.child(class, text, style, scale(x), scale(y), scale(width), scale(height), id, font)
	}

	child("STATIC", uiTitle, 0, 22, 17, 540, 31, 0, w.headingFont)
	child("STATIC", "The dashboard opens in your browser. This app manages metric collection.", 0, 22, 54, 540, 25, 0, w.font)
	child("STATIC", "Log folder", 0, 22, 91, 540, 21, 0, w.font)
	w.directory = child("EDIT", w.actions.InitialDirectory, 0x00800000|0x10000|0x800|0x80, 22, 115, 424, 29, 1000, w.font)
	w.browse = child("BUTTON", "Choose…", 0x10000, 456, 114, 106, 31, idBrowse, w.font)
	w.status = child("STATIC", "Starting the dashboard server…", 0, 22, 161, 540, 32, 0, w.font)
	w.panel = child("BUTTON", "Open dashboard", 0x10001, 22, 202, 218, 36, idPanel, w.font)
	w.hide = child("BUTTON", "Minimize to tray", 0x10000, 252, 202, 180, 36, idHide, w.font)
	child("STATIC", "Exiting from the tray icon stops the server and all checks.", 0, 22, 251, 540, 23, 0, w.font)
}

func runWindowMessages(hwnd uintptr) error {
	var message uiMessage
	for {
		result, _, err := uiUser32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("Window message handling error: %w", err)
		}

		if result == 0 {
			break
		}

		dialogMessage, _, _ := uiUser32.NewProc("IsDialogMessageW").Call(hwnd, uintptr(unsafe.Pointer(&message)))
		if dialogMessage == 0 {
			uiUser32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
			uiUser32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
		}
	}

	return nil
}
