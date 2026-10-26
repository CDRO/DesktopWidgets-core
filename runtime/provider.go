package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cdro/desktopwidgets-core/pkg/manifest"
)

// ProviderFunc produces the data for one provider. The returned value is
// JSON-encoded and delivered to the frontend's dw.subscribe(name, ...).
type ProviderFunc func(ctx *Ctx) (any, error)

// HandlerFunc answers a dw.call(name, args) from the frontend.
type HandlerFunc func(ctx *Ctx, args json.RawMessage) (any, error)

var (
	regMu     sync.Mutex
	providers = map[string]ProviderFunc{}
	handlers  = map[string]HandlerFunc{}
)

// Provide registers a data provider. Call it from an init() function in the
// widget's Go code; the refresh interval is declared in widget.yml.
func Provide(name string, fn ProviderFunc) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := providers[name]; dup {
		panic("dw: provider registered twice: " + name)
	}
	providers[name] = fn
}

// Handle registers a function the frontend can invoke with dw.call(name, args).
// Names starting with "dw." are reserved for the runtime.
func Handle(name string, fn HandlerFunc) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := handlers[name]; dup {
		panic("dw: handler registered twice: " + name)
	}
	handlers[name] = fn
}

// Ctx is passed to providers and handlers.
type Ctx struct {
	context.Context
	Widget   *manifest.Manifest
	Build    Build
	HTTP     *http.Client
	settings *Settings
}

func (c *Ctx) String(key string) string { return c.settings.String(key) }
func (c *Ctx) Float(key string) float64 { return c.settings.Float(key) }
func (c *Ctx) Bool(key string) bool     { return c.settings.Bool(key) }
func (c *Ctx) Setting(key string) any   { return c.settings.Get(key) }
func (c *Ctx) Settings() map[string]any { return c.settings.All() }

// dataEvent is emitted as "dw:data" for every provider run.
type dataEvent struct {
	Name  string `json:"name"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	At    int64  `json:"at"` // unix millis
}

type providerRunner struct {
	decl manifest.Provider
	fn   ProviderFunc
	kick chan struct{}
	last *dataEvent
}

// interval resolves the provider's refresh interval; 0 means run once.
func (p *providerRunner) interval(s *Settings) time.Duration {
	if p.decl.IntervalSetting != "" {
		secs := s.Float(p.decl.IntervalSetting)
		if secs < 1 {
			secs = 1
		}
		return time.Duration(secs * float64(time.Second))
	}
	if p.decl.Interval == "" {
		return 0
	}
	d, _ := time.ParseDuration(p.decl.Interval)
	return d
}

type providerSet struct {
	mu      sync.Mutex
	runners map[string]*providerRunner
}

func newProviderSet(m *manifest.Manifest) (*providerSet, error) {
	regMu.Lock()
	defer regMu.Unlock()
	ps := &providerSet{runners: map[string]*providerRunner{}}
	for _, d := range m.Providers {
		fn, ok := providers[d.Name]
		if !ok {
			return nil, fmt.Errorf("widget.yml declares provider %q but no Go code calls runtime.Provide(%q, ...)", d.Name, d.Name)
		}
		ps.runners[d.Name] = &providerRunner{decl: d, fn: fn, kick: make(chan struct{}, 1)}
	}
	for name := range providers {
		if _, ok := ps.runners[name]; !ok {
			return nil, fmt.Errorf("provider %q is registered in Go but not declared in widget.yml", name)
		}
	}
	return ps, nil
}

func (ps *providerSet) start(ctx context.Context, newCtx func(context.Context) *Ctx, emit func(*dataEvent)) {
	for _, r := range ps.runners {
		go func(r *providerRunner) {
			for {
				c := newCtx(ctx)
				runCtx, cancel := context.WithTimeout(c.Context, 30*time.Second)
				c.Context = runCtx
				data, err := r.fn(c)
				cancel()
				ev := &dataEvent{Name: r.decl.Name, Data: data, At: time.Now().UnixMilli()}
				if err != nil {
					ev.Error = err.Error()
					if prev := ps.lastOf(r); prev != nil {
						ev.Data = prev.Data // keep showing stale data
					}
				}
				ps.mu.Lock()
				r.last = ev
				ps.mu.Unlock()
				emit(ev)

				var tick <-chan time.Time
				var timer *time.Timer
				if d := r.interval(c.settings); d > 0 {
					timer = time.NewTimer(d)
					tick = timer.C
				}
				select {
				case <-ctx.Done():
				case <-tick:
				case <-r.kick:
				}
				if timer != nil {
					timer.Stop()
				}
				if ctx.Err() != nil {
					return
				}
			}
		}(r)
	}
}

func (ps *providerSet) lastOf(r *providerRunner) *dataEvent {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return r.last
}

// refresh triggers an immediate run of one provider ("" = all).
func (ps *providerSet) refresh(name string) {
	for n, r := range ps.runners {
		if name == "" || name == n {
			select {
			case r.kick <- struct{}{}:
			default:
			}
		}
	}
}

func (ps *providerSet) snapshot() []*dataEvent {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	var out []*dataEvent
	for _, r := range ps.runners {
		if r.last != nil {
			out = append(out, r.last)
		}
	}
	return out
}
