//go:build linux && cgo && gtk3

package runtime

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>

static void dw_pin_window(void *w, int below) {
	GtkWindow *win = GTK_WINDOW(w);
	gtk_window_set_skip_taskbar_hint(win, TRUE);
	gtk_window_set_skip_pager_hint(win, TRUE);
	gtk_window_stick(win);
	if (below) {
		gtk_window_set_keep_below(win, TRUE);
	}
}
*/
import "C"

import (
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func init() {
	// Widgets target X11 for now. On Wayland sessions we run through
	// XWayland so that keep-below and positioning keep working.
	if os.Getenv("GDK_BACKEND") == "" {
		os.Setenv("GDK_BACKEND", "x11")
	}
}

// pinToDesktop hides the window from taskbar/pager, shows it on all
// workspaces and, for layer "bottom", keeps it below normal windows.
func pinToDesktop(w *application.WebviewWindow, layer string) {
	application.InvokeSync(func() {
		if ptr := w.NativeWindow(); ptr != nil {
			below := C.int(0)
			if layer == "bottom" {
				below = 1
			}
			C.dw_pin_window(ptr, below)
		}
	})
}

// keepAtBottom is not needed on X11: the window manager honours keep-below.
func keepAtBottom(*application.WebviewWindow) {}
