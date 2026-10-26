package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cdro/desktopwidgets-core/pkg/manifest"
	"github.com/cdro/desktopwidgets-core/pkg/project"
)

const modulePath = "github.com/cdro/desktopwidgets-core"

// repo is the loaded repository: project config, targets and units.
type repo struct {
	Root    string
	Project *project.Project
	Targets []project.Target
	Units   []*unit
}

// unit is something dwb builds: a widget under widgets/ or the library app.
type unit struct {
	M     *manifest.Manifest
	Dir   string // relative to repo root, slash-separated
	HasGo bool   // has non-test .go files to import
}

func (u *unit) ImportPath() string { return modulePath + "/" + u.Dir }

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "project.yml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("project.yml not found; run dwb inside the repository")
		}
		dir = parent
	}
}

func loadRepo() (*repo, error) {
	root, err := findRoot()
	if err != nil {
		return nil, err
	}
	p, targets, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	r := &repo{Root: root, Project: p, Targets: targets}

	dirs, _ := filepath.Glob(filepath.Join(root, "widgets", "*", manifest.FileName))
	dirs = append(dirs, filepath.Join(root, "library", manifest.FileName))
	var errs []error
	seen := map[string]bool{}
	for _, f := range dirs {
		dir := filepath.Dir(f)
		base := filepath.Base(dir)
		if strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
			continue
		}
		if _, err := os.Stat(f); err != nil {
			continue
		}
		m, err := manifest.Load(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[m.ID] {
			errs = append(errs, fmt.Errorf("duplicate id %q", m.ID))
		}
		seen[m.ID] = true
		rel, _ := filepath.Rel(root, dir)
		u := &unit{M: m, Dir: filepath.ToSlash(rel)}
		gofiles, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, g := range gofiles {
			if !strings.HasSuffix(g, "_test.go") {
				u.HasGo = true
			}
		}
		for _, req := range []string{"icon.png", "frontend/index.html"} {
			if _, err := os.Stat(filepath.Join(dir, req)); err != nil {
				errs = append(errs, fmt.Errorf("%s: missing %s", rel, req))
			}
		}
		r.Units = append(r.Units, u)
	}
	sort.Slice(r.Units, func(i, j int) bool { return r.Units[i].M.ID < r.Units[j].M.ID })
	return r, errors.Join(errs...)
}

func (r *repo) unit(id string) (*unit, error) {
	for _, u := range r.Units {
		if u.M.ID == id {
			return u, nil
		}
	}
	return nil, fmt.Errorf("unknown widget %q", id)
}

func (r *repo) target(id string) (*project.Target, error) {
	for i := range r.Targets {
		if r.Targets[i].ID == id {
			return &r.Targets[i], nil
		}
	}
	return nil, fmt.Errorf("unknown target %q (see dwb targets)", id)
}

// assetName is the release file name, e.g. dw-clock_1.0.0_windows-amd64.msi.
func assetName(m *manifest.Manifest, targetID, format string) string {
	return fmt.Sprintf("%s_%s_%s.%s", m.Binary(), m.ParsedVersion(), targetID, format)
}

func iconAssetName(m *manifest.Manifest) string {
	return fmt.Sprintf("%s_%s.png", m.Binary(), m.ParsedVersion())
}
