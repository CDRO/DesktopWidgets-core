//go:build windows

package runtime

import (
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var procSetWindowPos = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")

const (
	hwndBottom     = 1
	swpNoSize      = 0x0001
	swpNoMove      = 0x0002
	swpNoActivate  = 0x0010
	swpNoOwnerZOrd = 0x0200
)

// pinToDesktop pushes the window to the bottom of the z-order. Taskbar
// hiding is done through WebviewWindowOptions.Windows.HiddenOnTaskbar.
func pinToDesktop(w *application.WebviewWindow, layer string) {
	if layer == "bottom" {
		keepAtBottom(w)
	}
}

// keepAtBottom re-applies HWND_BOTTOM; Windows raises a window when it is
// clicked, so this runs again on every focus change.
func keepAtBottom(w *application.WebviewWindow) {
	application.InvokeSync(func() {
		if hwnd := w.NativeWindow(); hwnd != nil {
			procSetWindowPos.Call(uintptr(hwnd), hwndBottom, 0, 0, 0, 0,
				swpNoSize|swpNoMove|swpNoActivate|swpNoOwnerZOrd)
		}
	})
}
