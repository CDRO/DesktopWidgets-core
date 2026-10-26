// Package __PKG__ holds the optional Go side of the __NAME__ widget.
// Delete this file if the widget only needs HTML/CSS/JS.
package __PKG__

import (
	"fmt"
	"time"

	dw "github.com/cdro/desktopwidgets-core/runtime"
)

func init() {
	// Declared under `providers` in widget.yml; the frontend receives the
	// return value via dw.subscribe("greeting", ...).
	dw.Provide("greeting", func(ctx *dw.Ctx) (any, error) {
		return map[string]string{
			"text": fmt.Sprintf("Hello, %s!", ctx.String("name")),
			"time": time.Now().Format("15:04"),
		}, nil
	})
}
