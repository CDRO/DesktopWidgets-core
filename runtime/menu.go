package runtime

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// widgetMenu is the right-click menu every widget gets. The SDK attaches it
// to the page via the CSS property --custom-contextmenu: dw-menu.
type widgetMenu struct {
	h   *host
	mu  sync.Mutex
	upd *updateInfo
}

func newWidgetMenu(h *host) *widgetMenu {
	wm := &widgetMenu{h: h}
	wm.build()
	return wm
}

// build creates a fresh menu and registers it under "dw-menu". The menu is
// replaced rather than updated in place: hidden items are not honoured on
// every platform, and updating a never-shown menu breaks it on GTK
// (Wails v3 beta).
func (wm *widgetMenu) build() {
	h, m := wm.h, wm.h.app.ContextMenu.New()
	m.Add(h.m.Name + " " + h.m.Version).SetEnabled(false)
	if u := wm.upd; u != nil {
		label := "Update to " + u.Version + "…"
		if u.Critical {
			label = "Security update " + u.Version + "…"
		}
		m.Add(label).OnClick(func(*application.Context) { h.updates.openUpdate() })
	}
	m.AddSeparator()
	if len(h.m.Settings) > 0 {
		m.Add("Settings…").OnClick(func(*application.Context) { h.openSettings() })
	}
	if len(h.m.Providers) > 0 {
		m.Add("Refresh").OnClick(func(*application.Context) { h.providers.refresh("") })
	}
	m.AddCheckbox("Lock position", h.state.Locked).OnClick(func(c *application.Context) {
		h.state.Locked = c.ClickedMenuItem().Checked()
		h.state.save()
		h.app.Event.Emit("dw:locked", h.state.Locked)
	})
	if !inFlatpak() {
		enabled, _ := h.app.Autostart.IsEnabled()
		m.AddCheckbox("Start at login", enabled).OnClick(func(c *application.Context) {
			if c.ClickedMenuItem().Checked() {
				_ = h.app.Autostart.Enable()
			} else {
				_ = h.app.Autostart.Disable()
			}
		})
	}
	m.AddSeparator()
	m.Add("Quit").OnClick(func(*application.Context) { h.app.Quit() })
	h.app.ContextMenu.Add("dw-menu", m)
}

func (wm *widgetMenu) setUpdate(info *updateInfo) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.upd = info
	wm.build()
}
