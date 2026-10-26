//go:build !windows && !(linux && cgo && gtk3)

package runtime

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Unsupported platform or a Linux build without the gtk3 tag (dwb always
// sets it): the widget still runs, but as a normal window.
func pinToDesktop(_ *application.WebviewWindow, layer string) {
	if layer == "bottom" {
		log.Print("dw: desktop pinning is not supported in this build")
	}
}

func keepAtBottom(*application.WebviewWindow) {}
