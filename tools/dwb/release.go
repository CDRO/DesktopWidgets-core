package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/cdro/desktopwidgets-core/pkg/catalog"
	"github.com/cdro/desktopwidgets-core/pkg/manifest"
)

// loadPrevious reads the previous catalog from a file or URL. A missing
// catalog (first release) yields an empty one.
func loadPrevious(src string) (*catalog.Catalog, error) {
	if src == "" {
		return &catalog.Catalog{Schema: catalog.Schema}, nil
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := catalog.Fetch(ctx, http.DefaultClient, src)
		if err != nil && strings.Contains(err.Error(), "404") {
			return &catalog.Catalog{Schema: catalog.Schema}, nil
		}
		return c, err
	}
	c, err := catalog.Read(src)
	if errors.Is(err, os.ErrNotExist) {
		return &catalog.Catalog{Schema: catalog.Schema}, nil
	}
	return c, err
}

// changedUnits returns units whose version differs from the catalog, and
// fails if any version went backwards.
func changedUnits(r *repo, prev *catalog.Catalog, all bool) ([]*unit, error) {
	var out []*unit
	var errs []error
	for _, u := range r.Units {
		e := prev.Find(u.M.ID)
		if e == nil || all {
			out = append(out, u)
			continue
		}
		pv, err := manifest.ParseVersion(e.Version)
		if err != nil {
			return nil, err
		}
		switch u.M.ParsedVersion().Compare(pv) {
		case 1:
			out = append(out, u)
		case -1:
			errs = append(errs, fmt.Errorf("%s: version %s is lower than released %s", u.M.ID, u.M.ParsedVersion(), pv))
		}
	}
	return out, errors.Join(errs...)
}

func cmdCatalog(args []string) error {
	fl := flag.NewFlagSet("catalog", flag.ExitOnError)
	tag := fl.String("tag", "", "release tag, e.g. v1.4.0")
	previous := fl.String("previous", "", "previous catalog.json (file or URL)")
	dist := fl.String("dist", "dist", "directory with build-*.json and assets")
	out := fl.String("out", "", "output path (default <dist>/catalog.json)")
	fl.Parse(args)
	if *tag == "" {
		return errors.New("--tag is required")
	}
	r, err := loadRepo()
	if err != nil {
		return err
	}
	cat, err := loadPrevious(*previous)
	if err != nil {
		return err
	}

	results, _ := filepath.Glob(filepath.Join(*dist, "build-*.json"))
	assets := map[string][]catalog.Asset{}
	for _, p := range results {
		var br buildResult
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &br); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		for _, a := range br.Assets {
			a.Asset.URL = r.Project.AssetURL(*tag, a.Asset.File)
			assets[a.Unit] = append(assets[a.Unit], a.Asset)
		}
	}
	for id, list := range assets {
		u, err := r.unit(id)
		if err != nil {
			return err
		}
		sort.Slice(list, func(i, j int) bool { return list[i].File < list[j].File })
		icon := iconAssetName(u.M)
		if err := copyFile(filepath.Join(r.Root, filepath.FromSlash(u.Dir), "icon.png"), filepath.Join(*dist, icon)); err != nil {
			return err
		}
		cat.Upsert(catalog.Entry{
			ID: u.M.ID, Kind: string(u.M.Kind), Name: u.M.Name, Description: u.M.Description,
			Category: u.M.Category, Version: u.M.ParsedVersion().String(),
			Released: *tag, ReleaseURL: "https://github.com/" + r.Project.ReleaseRepo + "/releases/tag/" + *tag,
			Notes: u.M.Release.Notes, Critical: u.M.Release.Critical,
			Icon: r.Project.AssetURL(*tag, icon), Assets: list,
		})
	}
	// Drop entries whose unit no longer exists in the repository.
	kept := cat.Widgets[:0]
	for _, e := range cat.Widgets {
		if _, err := r.unit(e.ID); err == nil {
			kept = append(kept, e)
		}
	}
	cat.Widgets = kept
	cat.Schema, cat.Release, cat.Generated = catalog.Schema, *tag, time.Now().UTC()
	if *out == "" {
		*out = filepath.Join(*dist, "catalog.json")
	}
	fmt.Printf("catalog: %d entries, %d updated in %s\n", len(cat.Widgets), len(assets), *tag)
	if err := cat.Write(*out); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*dist, "RELEASE_NOTES.md"), releaseNotes(cat, assets), 0o644)
}

// releaseNotes lists what this release changed.
func releaseNotes(cat *catalog.Catalog, updated map[string][]catalog.Asset) []byte {
	var b strings.Builder
	if len(updated) == 0 {
		b.WriteString("No widget changed in this release; the catalog was refreshed.\n")
		return []byte(b.String())
	}
	b.WriteString("## Updated\n\n")
	for _, e := range cat.Widgets {
		if _, ok := updated[e.ID]; !ok {
			continue
		}
		fmt.Fprintf(&b, "- **%s %s**", e.Name, e.Version)
		if e.Critical {
			b.WriteString(" (security update)")
		}
		if e.Notes != "" {
			b.WriteString(": " + oneLine(e.Notes))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nSee the [README](../../#readme) for which file to download.\n")
	return []byte(b.String())
}

var readmeTmpl = template.Must(template.New("readme").Funcs(template.FuncMap{
	"formats": func(e catalog.Entry) string {
		var parts []string
		for _, a := range e.Assets {
			label := a.Format
			if a.Distro != "" {
				label = a.Distro + " " + a.Format
			} else if a.OS == "windows" {
				label = "Windows " + a.Format
			}
			parts = append(parts, fmt.Sprintf("[%s](%s)", label, a.URL))
		}
		return strings.Join(parts, " · ")
	},
}).Parse(`# {{.Name}}

Desktop widgets for Windows and Linux. This repository only holds releases;
the source lives in [desktopwidgets-core](https://github.com/cdro/desktopwidgets-core).

Every widget can be installed on its own. The **Library** app lists all
widgets and installs or updates them for you, but you don't need it.

Latest release: **{{.Catalog.Release}}**. Machine-readable index: [catalog.json](catalog.json)

| Widget | Version | Description | Downloads |
| --- | --- | --- | --- |
{{range .Catalog.Widgets}}| **{{.Name}}** | {{.Version}} | {{.Description}} | {{formats .}} |
{{end}}
## Which file do I need?

- **Windows:** ` + "`.msi`" + ` installs for your user, no admin rights needed. ` + "`.zip`" + ` is portable.
  Widgets need the Microsoft Edge WebView2 runtime, which ships with Windows 10 and 11.
- **Debian / Ubuntu:** ` + "`.deb`" + ` for your distribution: ` + "`sudo apt install ./file.deb`" + `
- **Fedora:** ` + "`.rpm`" + `: ` + "`sudo dnf install ./file.rpm`" + `
- **Any distribution:** ` + "`.flatpak`" + `: ` + "`flatpak install --user ./file.flatpak`" + `, or the
  ` + "`.tar.gz`" + ` (extract, then run ` + "`./install.sh`" + `; it needs GTK 3 and WebKitGTK 4.1).

Linux widgets currently target X11. On Wayland they run through XWayland.

Verify downloads with the ` + "`SHA256SUMS`" + ` file attached to each release.
`))

func cmdReadme(args []string) error {
	fl := flag.NewFlagSet("readme", flag.ExitOnError)
	cat := fl.String("catalog", "dist/catalog.json", "catalog.json")
	out := fl.String("out", "README.md", "output file")
	fl.Parse(args)
	r, err := loadRepo()
	if err != nil {
		return err
	}
	c, err := catalog.Read(*cat)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := readmeTmpl.Execute(&buf, map[string]any{"Name": r.Project.Name, "Catalog": c}); err != nil {
		return err
	}
	return os.WriteFile(*out, buf.Bytes(), 0o644)
}

// cmdCheckVersions enforces that every changed unit bumps its version.
func cmdCheckVersions(args []string) error {
	fl := flag.NewFlagSet("check-versions", flag.ExitOnError)
	base := fl.String("base", "origin/main", "git ref to compare against")
	fl.Parse(args)
	r, err := loadRepo()
	if err != nil {
		return err
	}
	git := func(a ...string) (string, error) {
		cmd := exec.Command("git", a...)
		cmd.Dir = r.Root
		out, err := cmd.Output()
		return string(out), err
	}
	mb, err := git("merge-base", *base, "HEAD")
	if err != nil {
		return fmt.Errorf("git merge-base %s HEAD: %w", *base, err)
	}
	mb = strings.TrimSpace(mb)
	diff, err := git("diff", "--name-only", mb)
	if err != nil {
		return err
	}
	changed := map[string]bool{}
	shared := false
	for _, f := range strings.Fields(diff) {
		for _, u := range r.Units {
			if strings.HasPrefix(f, u.Dir+"/") {
				changed[u.M.ID] = true
			}
		}
		if strings.HasPrefix(f, "runtime/") || strings.HasPrefix(f, "sdk/") || strings.HasPrefix(f, "pkg/") {
			shared = true
		}
	}
	var errs []error
	for _, u := range r.Units {
		if !changed[u.M.ID] {
			continue
		}
		old, err := git("show", mb+":"+u.Dir+"/"+manifest.FileName)
		if err != nil {
			continue // new unit
		}
		om, err := manifest.Parse([]byte(old))
		if err != nil {
			continue
		}
		switch u.M.ParsedVersion().Compare(om.ParsedVersion()) {
		case 0:
			errs = append(errs, fmt.Errorf("%s changed but version is still %s: bump it in %s/widget.yml (major for features, minor/patch for fixes)", u.M.ID, om.ParsedVersion(), u.Dir))
		case -1:
			errs = append(errs, fmt.Errorf("%s: version went backwards (%s -> %s)", u.M.ID, om.ParsedVersion(), u.M.ParsedVersion()))
		default:
			fmt.Printf("%s: %s -> %s\n", u.M.ID, om.ParsedVersion(), u.M.ParsedVersion())
		}
	}
	if shared {
		fmt.Println("note: shared code (runtime/, sdk/, pkg/) changed; widgets only ship it when their version is bumped.")
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			fmt.Println("::warning::Shared code changed. Bump the versions of widgets that should ship this change.")
		}
	}
	return errors.Join(errs...)
}
