package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/cdro/desktopwidgets-core/pkg/manifest"
)

// Settings holds the user's values for the settings declared in widget.yml,
// persisted as JSON in the OS config directory.
type Settings struct {
	mu     sync.RWMutex
	path   string
	schema []manifest.Setting
	values map[string]any
}

// State is runtime-owned per-widget state (window position etc.).
type State struct {
	X      *int `json:"x,omitempty"`
	Y      *int `json:"y,omitempty"`
	Locked bool `json:"locked,omitempty"`
	path   string
	mu     sync.Mutex
}

func configDir(id string) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "desktopwidgets", id)
	return dir, os.MkdirAll(dir, 0o700)
}

func loadSettings(dir string, schema []manifest.Setting) *Settings {
	s := &Settings{path: filepath.Join(dir, "settings.json"), schema: schema, values: map[string]any{}}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &s.values)
	}
	return s
}

func (s *Settings) def(key string) (manifest.Setting, bool) {
	for _, d := range s.schema {
		if d.Key == key {
			return d, true
		}
	}
	return manifest.Setting{}, false
}

func (s *Settings) Get(key string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.values[key]; ok {
		return v
	}
	d, _ := s.def(key)
	return d.Default
}

func (s *Settings) All() map[string]any {
	out := map[string]any{}
	for _, d := range s.schema {
		out[d.Key] = s.Get(d.Key)
	}
	return out
}

func (s *Settings) String(key string) string {
	switch v := s.Get(key).(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func (s *Settings) Float(key string) float64 {
	switch v := s.Get(key).(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

func (s *Settings) Bool(key string) bool {
	b, _ := s.Get(key).(bool)
	return b
}

// Update validates and stores new values; unknown keys are rejected.
func (s *Settings) Update(in map[string]any) error {
	clean := map[string]any{}
	for k, v := range in {
		d, ok := s.def(k)
		if !ok {
			return fmt.Errorf("unknown setting %q", k)
		}
		cv, err := coerce(d, v)
		if err != nil {
			return fmt.Errorf("%s: %w", d.Label, err)
		}
		clean[k] = cv
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range clean {
		s.values[k] = v
	}
	data, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, data)
}

func coerce(d manifest.Setting, v any) (any, error) {
	switch d.Type {
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expected true/false")
		}
		return b, nil
	case "number":
		var f float64
		switch n := v.(type) {
		case float64:
			f = n
		case string:
			var err error
			if f, err = strconv.ParseFloat(n, 64); err != nil {
				return nil, fmt.Errorf("expected a number")
			}
		default:
			return nil, fmt.Errorf("expected a number")
		}
		if d.Min != nil && f < *d.Min {
			return nil, fmt.Errorf("must be at least %v", *d.Min)
		}
		if d.Max != nil && f > *d.Max {
			return nil, fmt.Errorf("must be at most %v", *d.Max)
		}
		return f, nil
	case "enum":
		str, _ := v.(string)
		for _, o := range d.Options {
			if o == str {
				return str, nil
			}
		}
		return nil, fmt.Errorf("must be one of %v", d.Options)
	default: // string, secret
		str, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected text")
		}
		return str, nil
	}
}

func loadState(dir string) *State {
	st := &State{path: filepath.Join(dir, "state.json")}
	if data, err := os.ReadFile(st.path); err == nil {
		_ = json.Unmarshal(data, st)
	}
	return st
}

func (st *State) save() {
	st.mu.Lock()
	defer st.mu.Unlock()
	if data, err := json.Marshal(st); err == nil {
		_ = writeFileAtomic(st.path, data)
	}
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
