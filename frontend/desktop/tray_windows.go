//go:build windows

package desktop

import (
	"syscall"
	"unsafe"
)

type uiNotifyIcon struct {
	Size            uint32
	Hwnd            uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            [16]byte
	BalloonIcon     uintptr
}

func (w *nativeWindow) addTray() {
	iconData := uiNotifyIcon{
		Hwnd:            w.hwnd,
		ID:              1,
		Flags:           1 | 2 | 4,
		CallbackMessage: wmTray,
		Icon:            w.icon,
	}
	iconData.Size = uint32(unsafe.Sizeof(iconData))
	copy(iconData.Tip[:], syscall.StringToUTF16("Connection Stability — double-click to open"))
	result, _, _ := uiShell32.NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&iconData)))
	w.trayAdded = result != 0
}

func (w *nativeWindow) removeTray() {
	if !w.trayAdded {
		return
	}

	iconData := uiNotifyIcon{
		Hwnd: w.hwnd,
		ID:   1,
	}
	iconData.Size = uint32(unsafe.Sizeof(iconData))
	uiShell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&iconData)))
	w.trayAdded = false
}

func (w *nativeWindow) trayMenu() {
	menu, _, _ := uiUser32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}

	defer uiUser32.NewProc("DestroyMenu").Call(menu)
	appendItem := func(id uintptr, text string) {
		uiUser32.NewProc("AppendMenuW").Call(menu, 0, id, uintptr(unsafe.Pointer(uiString(text))))
	}
	appendItem(idTrayShow, "Open window")
	appendItem(idTrayPanel, "Open dashboard")
	appendItem(idTrayLogs, "Open log folder")
	uiUser32.NewProc("AppendMenuW").Call(menu, 0x800, 0, 0)
	appendItem(idTrayExit, "Exit — stop everything")
	uiUser32.NewProc("SetMenuDefaultItem").Call(menu, idTrayShow, 0)
	var point uiPoint
	uiUser32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
	uiUser32.NewProc("SetForegroundWindow").Call(w.hwnd)
	selection, _, _ := uiUser32.NewProc("TrackPopupMenu").Call(menu, 0x100|0x2, uiSigned(point.X), uiSigned(point.Y), 0, w.hwnd, 0)
	uiUser32.NewProc("PostMessageW").Call(w.hwnd, 0, 0, 0)
	w.command(selection)
}

func (w *nativeWindow) trayMessage(message uint32) {
	switch message {
	case 0x203: // WM_LBUTTONDBLCLK
		w.show()
	case 0x205, 0x7b: // WM_RBUTTONUP / WM_CONTEXTMENU
		if !w.exiting {
			w.trayMenu()
		}
	}
}
