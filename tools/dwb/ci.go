package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/cdro/desktopwidgets-core/pkg/project"
)

// Build jobs come in two kinds: container targets run in a prebuilt image
// that already has their build dependencies, host targets (Windows) run
// their setup on the runner, with Cache paths restored first.
type hostEntry struct {
	Target   string `json:"target"`
	Runner   string `json:"runner"`
	Setup    string `json:"setup"`
	Cache    string `json:"cache"`     // newline-separated paths for actions/cache
	CacheKey string `json:"cache_key"` // changes when setup changes
	Widgets  string `json:"widgets"`
}

type containerEntry struct {
	Target  string `json:"target"`
	Runner  string `json:"runner"`
	Image   string `json:"image"`
	Options string `json:"options"`
	Widgets string `json:"widgets"`
}

// imageEntry is one image for the build-images workflow.
type imageEntry struct {
	Target string `json:"target"`
	Base   string `json:"base"`
	Setup  string `json:"setup"`
	Image  string `json:"image"`
}

func newImageEntry(t *project.Target, registry string) imageEntry {
	return imageEntry{Target: t.ID, Base: t.Container, Setup: t.Setup, Image: t.BuildImage(registry)}
}

func cmdPlan(args []string) error {
	fl := flag.NewFlagSet("plan", flag.ExitOnError)
	previous := fl.String("previous", "", "previous catalog.json (file or URL); empty = first release")
	all := fl.Bool("all", false, "rebuild every unit regardless of version")
	registry := fl.String("registry", "", "image repository for build images, e.g. ghcr.io/owner/repo/build")
	ghOut := fl.Bool("github-output", false, "write the outputs to $GITHUB_OUTPUT")
	fl.Parse(args)
	if *registry == "" {
		return fmt.Errorf("--registry is required")
	}

	r, err := loadRepo()
	if err != nil {
		return err
	}
	prev, err := loadPrevious(*previous)
	if err != nil {
		return err
	}
	units, err := changedUnits(r, prev, *all)
	if err != nil {
		return err
	}
	var ids []string
	for _, u := range units {
		ids = append(ids, u.M.ID)
	}
	hosts, containers, images := []hostEntry{}, []containerEntry{}, []imageEntry{}
	for i := range r.Targets {
		t := &r.Targets[i]
		sel, _ := selectUnits(r, t, strings.Join(ids, ","))
		if len(sel) == 0 {
			continue
		}
		var w []string
		for _, u := range sel {
			w = append(w, u.M.ID)
		}
		widgets := strings.Join(w, ",")
		if t.Container == "" {
			hosts = append(hosts, hostEntry{t.ID, t.Runner, t.Setup, strings.Join(t.Cache, "\n"), t.SetupHash(), widgets})
			continue
		}
		img := newImageEntry(t, *registry)
		images = append(images, img)
		containers = append(containers, containerEntry{t.ID, t.Runner, img.Image, t.Options, widgets})
	}

	fmt.Fprintf(os.Stderr, "units to release: %v\n", ids)
	for _, e := range hosts {
		fmt.Fprintf(os.Stderr, "  %-22s %s (host)\n", e.Target, e.Widgets)
	}
	for _, e := range containers {
		fmt.Fprintf(os.Stderr, "  %-22s %s (%s)\n", e.Target, e.Widgets, e.Image)
	}
	return writeOutputs(*ghOut, map[string]any{
		"widgets":          strings.Join(ids, ","),
		"matrix_host":      map[string]any{"include": hosts},
		"matrix_container": map[string]any{"include": containers},
		"images":           map[string]any{"include": images},
		"has_host":         len(hosts) > 0,
		"has_container":    len(containers) > 0,
	})
}

// cmdImages lists build images for some targets, for workflows that run
// in them outside a release (CI).
func cmdImages(args []string) error {
	fl := flag.NewFlagSet("images", flag.ExitOnError)
	registry := fl.String("registry", "", "image repository, e.g. ghcr.io/owner/repo/build")
	targets := fl.String("targets", "", "comma-separated container target ids (default: all)")
	ghOut := fl.Bool("github-output", false, "write images/image_map to $GITHUB_OUTPUT")
	fl.Parse(args)
	if *registry == "" {
		return fmt.Errorf("--registry is required")
	}
	r, err := loadRepo()
	if err != nil {
		return err
	}
	var sel []*project.Target
	if *targets == "" {
		for i := range r.Targets {
			sel = append(sel, &r.Targets[i])
		}
	} else {
		for _, id := range splitList(*targets) {
			t, err := r.target(id)
			if err != nil {
				return err
			}
			sel = append(sel, t)
		}
	}
	images, byTarget := []imageEntry{}, map[string]string{}
	for _, t := range sel {
		if t.Container == "" {
			if *targets != "" {
				return fmt.Errorf("target %q has no container image", t.ID)
			}
			continue
		}
		img := newImageEntry(t, *registry)
		images = append(images, img)
		byTarget[t.ID] = img.Image
	}
	return writeOutputs(*ghOut, map[string]any{
		"images":    map[string]any{"include": images},
		"image_map": byTarget,
	})
}

// writeOutputs prints outputs as JSON, or appends them to $GITHUB_OUTPUT
// (non-string values JSON-encoded, one line each).
func writeOutputs(gh bool, out map[string]any) error {
	if !gh {
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	f, err := os.OpenFile(os.Getenv("GITHUB_OUTPUT"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("GITHUB_OUTPUT: %w", err)
	}
	defer f.Close()
	for k, v := range out {
		s, ok := v.(string)
		if !ok {
			b, _ := json.Marshal(v)
			s = string(b)
		}
		fmt.Fprintf(f, "%s=%s\n", k, s)
	}
	return nil
}
