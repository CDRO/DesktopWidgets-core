// Package manifest defines widget.yml, the contract between a widget and the
// build tooling / runtime. It has no GUI dependencies so that dwb can use it.
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const FileName = "widget.yml"

type Kind string

const (
	KindWidget Kind = "widget" // frameless desktop widget
	KindApp    Kind = "app"    // regular framed window (e.g. the library)
)

type Manifest struct {
	ID          string     `yaml:"id" json:"id"`
	Name        string     `yaml:"name" json:"name"`
	Description string     `yaml:"description" json:"description"`
	Version     string     `yaml:"version" json:"version"`
	Kind        Kind       `yaml:"kind,omitempty" json:"kind"`
	Category    string     `yaml:"category,omitempty" json:"category,omitempty"`
	Window      Window     `yaml:"window" json:"window"`
	Settings    []Setting  `yaml:"settings,omitempty" json:"settings,omitempty"`
	Providers   []Provider `yaml:"providers,omitempty" json:"providers,omitempty"`
	Platforms   Platforms  `yaml:"platforms,omitempty" json:"-"`
	Release     Release    `yaml:"release,omitempty" json:"release"`
}

type Window struct {
	Width       int    `yaml:"width" json:"width"`
	Height      int    `yaml:"height" json:"height"`
	Resizable   bool   `yaml:"resizable,omitempty" json:"resizable"`
	Transparent bool   `yaml:"transparent,omitempty" json:"transparent"`
	Layer       string `yaml:"layer,omitempty" json:"layer"`       // bottom | normal | top
	Position    string `yaml:"position,omitempty" json:"position"` // top-left | top-right | bottom-left | bottom-right | center
}

type Setting struct {
	Key         string   `yaml:"key" json:"key"`
	Type        string   `yaml:"type" json:"type"` // bool | string | number | enum | secret
	Label       string   `yaml:"label,omitempty" json:"label,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Default     any      `yaml:"default,omitempty" json:"default,omitempty"`
	Options     []string `yaml:"options,omitempty" json:"options,omitempty"` // enum only
	Min         *float64 `yaml:"min,omitempty" json:"min,omitempty"`         // number only
	Max         *float64 `yaml:"max,omitempty" json:"max,omitempty"`         // number only
}

// Provider declares a Go data source registered with runtime.Provide.
// The refresh interval is either fixed (Interval) or taken from a number
// setting holding seconds (IntervalSetting).
type Provider struct {
	Name            string `yaml:"name" json:"name"`
	Interval        string `yaml:"interval,omitempty" json:"interval,omitempty"`
	IntervalSetting string `yaml:"intervalSetting,omitempty" json:"intervalSetting,omitempty"`
}

// Release carries per-version metadata that ends up in the catalog.
type Release struct {
	// Notes is a short, user-facing summary of what changed in this version.
	Notes string `yaml:"notes,omitempty" json:"notes,omitempty"`
	// Critical forces a prominent update prompt even for minor/patch
	// versions (use for security fixes).
	Critical bool `yaml:"critical,omitempty" json:"critical,omitempty"`
}

// Platforms restricts which build targets a widget is built for. In YAML it
// is either the string "all" (the default) or {include: [...], exclude: [...]}
// where entries match a target id (e.g. "debian-12-amd64") or an OS ("linux").
type Platforms struct {
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
}

func (p *Platforms) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Value != "all" {
			return fmt.Errorf("platforms: expected \"all\" or a mapping, got %q", n.Value)
		}
		*p = Platforms{}
		return nil
	}
	type plain Platforms
	return n.Decode((*plain)(p))
}

// Matches reports whether a target (by id and OS) is enabled.
func (p Platforms) Matches(targetID, os string) bool {
	in := func(list []string) bool {
		for _, e := range list {
			if e == targetID || e == os {
				return true
			}
		}
		return false
	}
	if len(p.Include) > 0 && !in(p.Include) {
		return false
	}
	return !in(p.Exclude)
}

var (
	idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)
	layers    = map[string]bool{"bottom": true, "normal": true, "top": true}
	positions = map[string]bool{"top-left": true, "top-right": true, "bottom-left": true, "bottom-right": true, "center": true}
	types     = map[string]bool{"bool": true, "string": true, "number": true, "enum": true, "secret": true}
)

// Parse decodes and validates a manifest, filling in defaults.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("widget.yml: %w", err)
	}
	m.applyDefaults()
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Load reads <dir>/widget.yml and checks that the folder name matches the id.
func Load(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if base := filepath.Base(dir); base != m.ID && m.Kind == KindWidget {
		return nil, fmt.Errorf("%s: folder name must equal id %q", dir, m.ID)
	}
	return m, nil
}

func (m *Manifest) applyDefaults() {
	if m.Kind == "" {
		m.Kind = KindWidget
	}
	if m.Window.Layer == "" {
		if m.Kind == KindApp {
			m.Window.Layer = "normal"
		} else {
			m.Window.Layer = "bottom"
		}
	}
	if m.Window.Position == "" {
		m.Window.Position = "center"
	}
	for i := range m.Settings {
		if m.Settings[i].Label == "" {
			m.Settings[i].Label = m.Settings[i].Key
		}
	}
}

func (m *Manifest) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if !idPattern.MatchString(m.ID) {
		add("id %q must be kebab-case (a-z, 0-9, -)", m.ID)
	}
	if m.Name == "" {
		add("name is required")
	}
	if m.Kind != KindWidget && m.Kind != KindApp {
		add("kind must be widget or app")
	}
	if v, err := ParseVersion(m.Version); err != nil {
		add("version: %v", err)
	} else if v.Major < 1 {
		add("version must start at 1.0.0")
	}
	if m.Window.Width <= 0 || m.Window.Height <= 0 {
		add("window.width and window.height are required")
	}
	if !layers[m.Window.Layer] {
		add("window.layer %q must be bottom, normal or top", m.Window.Layer)
	}
	if !positions[m.Window.Position] {
		add("window.position %q is not supported", m.Window.Position)
	}

	keys := map[string]Setting{}
	for _, s := range m.Settings {
		if s.Key == "" {
			add("settings: key is required")
			continue
		}
		if _, dup := keys[s.Key]; dup {
			add("settings: duplicate key %q", s.Key)
		}
		keys[s.Key] = s
		if !types[s.Type] {
			add("settings.%s: unknown type %q", s.Key, s.Type)
		}
		if s.Type == "enum" && len(s.Options) == 0 {
			add("settings.%s: enum needs options", s.Key)
		}
	}

	names := map[string]bool{}
	for _, p := range m.Providers {
		if p.Name == "" {
			add("providers: name is required")
			continue
		}
		if names[p.Name] {
			add("providers: duplicate name %q", p.Name)
		}
		names[p.Name] = true
		switch {
		case p.Interval != "" && p.IntervalSetting != "":
			add("providers.%s: set interval or intervalSetting, not both", p.Name)
		case p.Interval != "":
			if d, err := time.ParseDuration(p.Interval); err != nil || d < time.Second {
				add("providers.%s: interval must be a duration >= 1s", p.Name)
			}
		case p.IntervalSetting != "":
			if s, ok := keys[p.IntervalSetting]; !ok || s.Type != "number" {
				add("providers.%s: intervalSetting must name a number setting", p.Name)
			}
		}
	}
	return errors.Join(errs...)
}

// ParsedVersion returns the validated version.
func (m *Manifest) ParsedVersion() Version {
	v, _ := ParseVersion(m.Version)
	return v
}

// Binary is the executable / package name, e.g. "dw-clock".
func (m *Manifest) Binary() string {
	return "dw-" + m.ID
}

// GoPackage is the Go package name for the widget's optional Go code.
func (m *Manifest) GoPackage() string {
	return strings.ReplaceAll(m.ID, "-", "")
}
