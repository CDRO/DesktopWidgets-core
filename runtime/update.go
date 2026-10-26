package runtime

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/cdro/desktopwidgets-core/pkg/catalog"
	"github.com/cdro/desktopwidgets-core/pkg/manifest"
)

const (
	updateFirstCheck = 20 * time.Second
	updateInterval   = 6 * time.Hour
)

// updateInfo is emitted as "dw:update". The SDK only shows a visible badge
// for major or critical updates; minor/patch updates appear in the context
// menu only, so routine fixes don't nag users.
type updateInfo struct {
	Version  string `json:"version"`
	Kind     string `json:"kind"` // major | minor
	Critical bool   `json:"critical"`
	Notes    string `json:"notes,omitempty"`
	URL      string `json:"url"`
	Prompt   bool   `json:"prompt"` // true when the user should be told visibly
}

type updateChecker struct {
	h    *host
	mu   sync.Mutex
	info *updateInfo
}

func newUpdateChecker(h *host) *updateChecker { return &updateChecker{h: h} }

func (u *updateChecker) current() *updateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.info
}

func (u *updateChecker) start(ctx context.Context) {
	if u.h.build.CatalogURL == "" {
		return
	}
	go func() {
		wait := updateFirstCheck
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			if err := u.check(ctx); err != nil {
				log.Printf("dw: update check: %v", err)
			}
			wait = updateInterval
		}
	}()
}

func (u *updateChecker) check(ctx context.Context) error {
	c, err := catalog.Fetch(ctx, u.h.http, u.h.build.CatalogURL)
	if err != nil {
		return err
	}
	e := c.Find(u.h.m.ID)
	if e == nil {
		return nil
	}
	latest, err := manifest.ParseVersion(e.Version)
	if err != nil {
		return err
	}
	kind := u.h.m.ParsedVersion().UpdateTo(latest)
	var info *updateInfo
	if kind != manifest.UpdateNone {
		info = &updateInfo{
			Version:  latest.String(),
			Kind:     string(kind),
			Critical: e.Critical,
			Notes:    e.Notes,
			URL:      e.ReleaseURL,
			Prompt:   kind == manifest.UpdateMajor || e.Critical,
		}
	}
	u.mu.Lock()
	u.info = info
	u.mu.Unlock()
	if info != nil {
		u.h.app.Event.Emit("dw:update", info)
		if u.h.menu != nil {
			u.h.menu.setUpdate(info)
		}
	}
	return nil
}

// openUpdate hands the update to the library app when it is installed,
// otherwise opens the release page in the browser.
func (u *updateChecker) openUpdate() {
	info := u.current()
	if info == nil {
		return
	}
	if lib := findLibrary(); lib != "" {
		if err := exec.Command(lib, "--show", u.h.m.ID).Start(); err == nil {
			return
		}
	}
	_ = u.h.app.Browser.OpenURL(info.URL)
}

// findLibrary locates dw-library: on PATH (deb/rpm/tarball installs) or as
// a sibling install directory (MSI layout: <ProgramFiles>\DesktopWidgets\dw-<id>\).
func findLibrary() string {
	name := "dw-library"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(filepath.Dir(exe)), "dw-library", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
