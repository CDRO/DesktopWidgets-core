// Package project loads the repository-level configuration: project.yml
// (identity and release destination) and platforms.yml (the build matrix).
package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cdro/desktopwidgets-core/pkg/manifest"
)

type Project struct {
	Name        string `yaml:"name"`
	AppIDPrefix string `yaml:"appIdPrefix"` // reverse DNS, e.g. io.github.cdro.desktopwidgets
	ReleaseRepo string `yaml:"releaseRepo"` // owner/repo receiving releases
	Vendor      string `yaml:"vendor"`
	Maintainer  string `yaml:"maintainer"`
	Homepage    string `yaml:"homepage"`
	License     string `yaml:"license"`
}

// AppID returns the reverse-DNS application id for a widget, used for
// Flatpak ids, desktop files and the MSI upgrade code.
func (p *Project) AppID(m *manifest.Manifest) string {
	return p.AppIDPrefix + "." + strings.ReplaceAll(m.ID, "-", "_")
}

// CatalogURL is where widgets and the library look for updates.
func (p *Project) CatalogURL() string {
	return "https://github.com/" + p.ReleaseRepo + "/releases/latest/download/catalog.json"
}

// AssetURL is the download URL of a release asset.
func (p *Project) AssetURL(tag, file string) string {
	return "https://github.com/" + p.ReleaseRepo + "/releases/download/" + tag + "/" + file
}

// Target is one fully expanded build environment.
type Target struct {
	ID     string `yaml:"id" json:"id"`
	OS     string `yaml:"os" json:"os"`     // windows | linux
	Arch   string `yaml:"arch" json:"arch"` // amd64 | arm64
	Distro string `yaml:"distro,omitempty" json:"distro,omitempty"`
	Runner string `yaml:"runner" json:"runner"`
	// Container is the base image. Setup is baked into a prebuilt build
	// image on top of it (see BuildImage), so jobs don't reinstall deps.
	Container string `yaml:"container,omitempty" json:"container"`
	// Options are extra `docker run` options, e.g. --privileged.
	Options string `yaml:"options,omitempty" json:"options,omitempty"`
	// Setup installs build dependencies: baked into the image for container
	// targets, run at job start (after restoring Cache) for host targets.
	Setup string `yaml:"setup,omitempty" json:"setup"`
	// Cache lists paths cached between runs of host targets, e.g. tools
	// installed by Setup. Setup must then be idempotent.
	Cache   []string `yaml:"cache,omitempty" json:"cache,omitempty"`
	Formats []string `yaml:"formats" json:"formats"`
	// Depends lists runtime package dependencies for deb/rpm.
	Depends []string `yaml:"depends,omitempty" json:"-"`
	Flatpak *Flatpak `yaml:"flatpak,omitempty" json:"-"`
}

type Flatpak struct {
	Runtime        string `yaml:"runtime"`
	RuntimeVersion string `yaml:"runtimeVersion"`
	SDK            string `yaml:"sdk"`
	GoExtension    string `yaml:"goExtension"`
}

// rawTarget is a target before matrix expansion. Matrix follows GitLab's
// parallel:matrix: a list of maps from variable to values; each map expands
// to the cartesian product of its values and ${VAR} is substituted in every
// string field.
type rawTarget struct {
	Target `yaml:",inline"`
	Matrix []map[string][]string `yaml:"matrix,omitempty"`
}

type platformsFile struct {
	Targets []rawTarget `yaml:"targets"`
}

var (
	formats   = map[string]map[string]bool{"windows": {"msi": true, "zip": true}, "linux": {"tar.gz": true, "deb": true, "rpm": true, "flatpak": true}}
	varRe     = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)
	targetIDs = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
)

func Load(root string) (*Project, []Target, error) {
	var p Project
	if err := readYAML(filepath.Join(root, "project.yml"), &p); err != nil {
		return nil, nil, err
	}
	if p.AppIDPrefix == "" || p.ReleaseRepo == "" || p.Name == "" {
		return nil, nil, errors.New("project.yml: name, appIdPrefix and releaseRepo are required")
	}
	var pf platformsFile
	if err := readYAML(filepath.Join(root, "platforms.yml"), &pf); err != nil {
		return nil, nil, err
	}
	targets, err := expand(pf.Targets)
	if err != nil {
		return nil, nil, fmt.Errorf("platforms.yml: %w", err)
	}
	return &p, targets, nil
}

func readYAML(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

func expand(raw []rawTarget) ([]Target, error) {
	var out []Target
	seen := map[string]bool{}
	for _, r := range raw {
		combos := []map[string]string{{}}
		if len(r.Matrix) > 0 {
			combos = nil
			for _, m := range r.Matrix {
				combos = append(combos, cartesian(m)...)
			}
		}
		for _, vars := range combos {
			t, err := substitute(r.Target, vars)
			if err != nil {
				return nil, err
			}
			if err := validate(t); err != nil {
				return nil, err
			}
			if seen[t.ID] {
				return nil, fmt.Errorf("duplicate target id %q", t.ID)
			}
			seen[t.ID] = true
			out = append(out, t)
		}
	}
	return out, nil
}

func cartesian(m map[string][]string) []map[string]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	res := []map[string]string{{}}
	for _, k := range keys {
		var next []map[string]string
		for _, base := range res {
			for _, v := range m[k] {
				c := map[string]string{k: v}
				for bk, bv := range base {
					c[bk] = bv
				}
				next = append(next, c)
			}
		}
		res = next
	}
	return res
}

func substitute(t Target, vars map[string]string) (Target, error) {
	var missing []string
	sub := func(s string) string {
		return varRe.ReplaceAllStringFunc(s, func(m string) string {
			name := varRe.FindStringSubmatch(m)[1]
			v, ok := vars[name]
			if !ok {
				missing = append(missing, name)
			}
			return v
		})
	}
	t.ID, t.OS, t.Arch, t.Distro = sub(t.ID), sub(t.OS), sub(t.Arch), sub(t.Distro)
	t.Runner, t.Container, t.Setup, t.Options = sub(t.Runner), sub(t.Container), sub(t.Setup), sub(t.Options)
	t.Cache = subAll(t.Cache, sub)
	t.Formats = subAll(t.Formats, sub)
	t.Depends = subAll(t.Depends, sub)
	if len(missing) > 0 {
		return t, fmt.Errorf("target %q: undefined matrix variable(s) %v", t.ID, missing)
	}
	return t, nil
}

func subAll(in []string, sub func(string) string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = sub(s)
	}
	return out
}

// SetupHash identifies a target's build environment (base image + setup).
func (t Target) SetupHash() string {
	h := sha256.Sum256([]byte(t.Container + "\n" + t.Setup))
	return hex.EncodeToString(h[:])[:12]
}

// BuildImage is the prebuilt image for a container target:
// <repository>:<target>-<SetupHash>. Changing the base image or setup
// yields a new tag, so images never go stale. Host targets return "".
func (t Target) BuildImage(repository string) string {
	if t.Container == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s-%s", strings.ToLower(repository), t.ID, t.SetupHash())
}

func validate(t Target) error {
	if !targetIDs.MatchString(t.ID) {
		return fmt.Errorf("target id %q must be lowercase (a-z, 0-9, ., -)", t.ID)
	}
	allowed, ok := formats[t.OS]
	if !ok {
		return fmt.Errorf("target %q: os must be windows or linux", t.ID)
	}
	if t.Arch != "amd64" && t.Arch != "arm64" {
		return fmt.Errorf("target %q: arch must be amd64 or arm64", t.ID)
	}
	if t.Runner == "" || len(t.Formats) == 0 {
		return fmt.Errorf("target %q: runner and formats are required", t.ID)
	}
	if t.Container != "" && len(t.Cache) > 0 {
		return fmt.Errorf("target %q: cache is for host targets; container setup is baked into the image", t.ID)
	}
	for _, f := range t.Formats {
		if !allowed[f] {
			return fmt.Errorf("target %q: format %q is not available on %s", t.ID, f, t.OS)
		}
		if f == "flatpak" && t.Flatpak == nil {
			return fmt.Errorf("target %q: flatpak format needs a flatpak section", t.ID)
		}
	}
	return nil
}
