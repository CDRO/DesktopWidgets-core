// Package runtime is the shared host for every widget: it reads widget.yml,
// creates the window, pins it to the desktop, runs providers, persists
// settings and checks for updates. Widgets import it as
//
//	import dw "github.com/cdro/desktopwidgets-core/runtime"
//
// and never contain platform-specific code themselves.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/cdro/desktopwidgets-core/pkg/manifest"
	"github.com/cdro/desktopwidgets-core/sdk"
)

// Build carries project-level values baked in by dwb's generated main.go.
type Build struct {
	AppID       string // reverse-DNS id, e.g. io.github.cdro.desktopwidgets.clock
	ReleaseRepo string // owner/repo of the release repository
	CatalogURL  string // empty disables update checks (dev builds)
}

// host is the running widget.
type host struct {
	m         *manifest.Manifest
	build     Build
	app       *application.App
	win       *application.WebviewWindow
	settings  *Settings
	state     *State
	providers *providerSet
	updates   *updateChecker
	http      *http.Client
	menu      *widgetMenu
}

// Main runs a widget. files must contain widget.yml, icon.png and frontend/.
func Main(files fs.FS, build Build) {
	if err := run(files, build); err != nil {
		log.Fatalf("dw: %v", err)
	}
}

func run(files fs.FS, build Build) error {
	data, err := fs.ReadFile(files, manifest.FileName)
	if err != nil {
		return err
	}
	m, err := manifest.Parse(data)
	if err != nil {
		return err
	}
	// Used by the library app (and humans) to detect installed versions.
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(m.ParsedVersion())
		return nil
	}
	frontend, err := fs.Sub(files, "frontend")
	if err != nil {
		return err
	}
	icon, _ := fs.ReadFile(files, "icon.png")

	dir, err := configDir(m.ID)
	if err != nil {
		return err
	}
	h := &host{
		m:        m,
		build:    build,
		settings: loadSettings(dir, m.Settings),
		state:    loadState(dir),
		http:     &http.Client{Timeout: 20 * time.Second},
	}
	if h.providers, err = newProviderSet(m); err != nil {
		return err
	}

	h.app = application.New(application.Options{
		Name:        m.Name,
		Description: m.Description,
		Icon:        icon,
		Assets:      application.AssetOptions{Handler: assetHandler(frontend)},
		Linux:       application.LinuxOptions{ProgramName: m.Binary()},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: build.AppID,
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				if h.win != nil {
					h.win.Show()
					h.win.Focus()
				}
				h.app.Event.Emit("dw:second-instance", d.Args)
			},
		},
	})
	h.updates = newUpdateChecker(h)
	h.createWindow(icon)
	if m.Kind == manifest.KindWidget {
		h.menu = newWidgetMenu(h)
	}
	h.registerCalls()

	h.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go h.started()
	})
	return h.app.Run()
}

// assetHandler serves the runtime SDK under /dw/ and the widget frontend
// everywhere else. /wails/* is handled by Wails itself.
func assetHandler(frontend fs.FS) http.Handler {
	widget := application.BundledAssetFileServer(frontend)
	sdkFS, _ := fs.Sub(sdk.Files, "js")
	sdkHandler := http.StripPrefix("/dw/", http.FileServerFS(sdkFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/dw/") {
			sdkHandler.ServeHTTP(w, r)
			return
		}
		widget.ServeHTTP(w, r)
	})
}

func (h *host) createWindow(icon []byte) {
	m := h.m
	isWidget := m.Kind == manifest.KindWidget
	opts := application.WebviewWindowOptions{
		Name:                       "main",
		Title:                      m.Name,
		Width:                      m.Window.Width,
		Height:                     m.Window.Height,
		DisableResize:              !m.Window.Resizable,
		Frameless:                  isWidget,
		AlwaysOnTop:                m.Window.Layer == "top",
		Hidden:                     true,
		URL:                        "/",
		DefaultContextMenuDisabled: isWidget,
		Windows: application.WindowsWindow{
			HiddenOnTaskbar:                   isWidget,
			DisableFramelessWindowDecorations: isWidget,
		},
		Linux: application.LinuxWindow{
			Icon:                icon,
			WindowIsTranslucent: m.Window.Transparent,
		},
	}
	if m.Window.Transparent {
		opts.BackgroundType = application.BackgroundTypeTransparent
		opts.BackgroundColour = application.NewRGBA(0, 0, 0, 0)
	}
	h.win = h.app.Window.NewWithOptions(opts)

	h.win.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		x, y := h.win.Position()
		h.state.X, h.state.Y = &x, &y
		h.state.save()
	})
	if isWidget && m.Window.Layer == "bottom" {
		h.win.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
			go keepAtBottom(h.win)
		})
	}
}

// started runs once the event loop is up: position, pin, show, start work.
func (h *host) started() {
	x, y := h.initialPosition()
	h.win.SetPosition(x, y)
	if h.m.Kind == manifest.KindWidget {
		pinToDesktop(h.win, h.m.Window.Layer)
	}
	h.win.Show()
	if h.m.Kind == manifest.KindWidget {
		pinToDesktop(h.win, h.m.Window.Layer)
	}

	ctx := h.app.Context()
	h.providers.start(ctx, h.newCtx, func(ev *dataEvent) { h.app.Event.Emit("dw:data", ev) })
	h.updates.start(ctx)
}

func (h *host) initialPosition() (int, int) {
	if h.state.X != nil && h.state.Y != nil {
		return *h.state.X, *h.state.Y
	}
	const margin = 24
	w, ht := h.m.Window.Width, h.m.Window.Height
	area := application.Rect{Width: 1280, Height: 720}
	if s := h.app.Screen.GetPrimary(); s != nil {
		area = s.WorkArea
	}
	left, top := area.X+margin, area.Y+margin
	right, bottom := area.X+area.Width-w-margin, area.Y+area.Height-ht-margin
	switch h.m.Window.Position {
	case "top-left":
		return left, top
	case "top-right":
		return right, top
	case "bottom-left":
		return left, bottom
	case "bottom-right":
		return right, bottom
	default:
		return area.X + (area.Width-w)/2, area.Y + (area.Height-ht)/2
	}
}

func (h *host) newCtx(parent context.Context) *Ctx {
	return &Ctx{Context: parent, Widget: h.m, Build: h.build, HTTP: h.http, settings: h.settings}
}

// info is what dw.info() returns to the frontend.
func (h *host) info() map[string]any {
	return map[string]any{
		"id":       h.m.ID,
		"name":     h.m.Name,
		"version":  h.m.Version,
		"kind":     h.m.Kind,
		"os":       goruntime.GOOS,
		"arch":     goruntime.GOARCH,
		"locked":   h.state.Locked,
		"settings": h.settings.All(),
		"update":   h.updates.current(),
	}
}

// RPC over events: the SDK emits "dw:call" {id, method, args} and gets
// "dw:reply" {id, result, error} back.
type callRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args"`
}

func (h *host) registerCalls() {
	builtin := map[string]HandlerFunc{
		"dw.info":    func(*Ctx, json.RawMessage) (any, error) { return h.info(), nil },
		"dw.schema":  func(*Ctx, json.RawMessage) (any, error) { return h.m.Settings, nil },
		"dw.ready":   h.callReady,
		"dw.refresh": h.callRefresh,
		"dw.save":    h.callSaveSettings,
		"dw.openURL": h.callOpenURL,
		"dw.close":   func(*Ctx, json.RawMessage) (any, error) { h.closeSettings(); return nil, nil },
		"dw.update":  func(*Ctx, json.RawMessage) (any, error) { h.updates.openUpdate(); return nil, nil },
	}
	h.app.Event.On("dw:call", func(e *application.CustomEvent) {
		raw, _ := json.Marshal(e.Data)
		var req callRequest
		if err := json.Unmarshal(raw, &req); err != nil || req.ID == "" {
			return
		}
		go func() {
			fn, ok := builtin[req.Method]
			if !ok {
				regMu.Lock()
				fn, ok = handlers[req.Method]
				regMu.Unlock()
			}
			reply := map[string]any{"id": req.ID}
			if !ok {
				reply["error"] = fmt.Sprintf("unknown method %q", req.Method)
			} else if res, err := fn(h.newCtx(h.app.Context()), req.Args); err != nil {
				reply["error"] = err.Error()
			} else {
				reply["result"] = res
			}
			h.app.Event.Emit("dw:reply", reply)
		}()
	})
}

// callReady is sent by the SDK once the page has loaded; it replays the
// latest provider data so nothing emitted before page load is lost.
func (h *host) callReady(*Ctx, json.RawMessage) (any, error) {
	go func() {
		for _, ev := range h.providers.snapshot() {
			h.app.Event.Emit("dw:data", ev)
		}
	}()
	return h.info(), nil
}

func (h *host) callRefresh(_ *Ctx, args json.RawMessage) (any, error) {
	var a struct{ Name string }
	_ = json.Unmarshal(args, &a)
	h.providers.refresh(a.Name)
	return nil, nil
}

func (h *host) callSaveSettings(_ *Ctx, args json.RawMessage) (any, error) {
	var values map[string]any
	if err := json.Unmarshal(args, &values); err != nil {
		return nil, err
	}
	if err := h.settings.Update(values); err != nil {
		return nil, err
	}
	all := h.settings.All()
	h.app.Event.Emit("dw:settings", all)
	h.providers.refresh("")
	return all, nil
}

func (h *host) callOpenURL(_ *Ctx, args json.RawMessage) (any, error) {
	var a struct{ URL string }
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(a.URL, "https://") && !strings.HasPrefix(a.URL, "http://") {
		return nil, fmt.Errorf("only http(s) URLs can be opened")
	}
	return nil, h.app.Browser.OpenURL(a.URL)
}

func (h *host) openSettings() {
	if w, ok := h.app.Window.GetByName("settings"); ok {
		w.Show()
		w.Focus()
		return
	}
	h.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "settings",
		Title:  h.m.Name + " – Settings",
		Width:  420,
		Height: 520,
		URL:    "/dw/settings.html",
	})
}

func (h *host) closeSettings() {
	if w, ok := h.app.Window.GetByName("settings"); ok {
		w.Close()
	}
}

func inFlatpak() bool { return os.Getenv("FLATPAK_ID") != "" }
