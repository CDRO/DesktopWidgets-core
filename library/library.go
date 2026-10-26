// Package library implements the Widget Library app's backend: it reads the
// release catalog, detects installed widgets and installs/updates/launches
// them using the platform's native package format.
package library

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/cdro/desktopwidgets-core/pkg/catalog"
	dw "github.com/cdro/desktopwidgets-core/runtime"
)

// item is one catalog entry as the frontend sees it.
type item struct {
	catalog.Entry
	Installed string         `json:"installed,omitempty"` // installed version, "" if not installed
	Method    string         `json:"method,omitempty"`    // how it is installed
	Asset     *catalog.Asset `json:"asset,omitempty"`     // best download for this machine
}

func init() {
	dw.Handle("library.list", list)
	dw.Handle("library.install", install)
	dw.Handle("library.launch", launch)
	dw.Handle("library.focus", func(*dw.Ctx, json.RawMessage) (any, error) {
		for i, a := range os.Args {
			if a == "--show" && i+1 < len(os.Args) {
				return os.Args[i+1], nil
			}
		}
		return "", nil
	})
}

func list(ctx *dw.Ctx, _ json.RawMessage) (any, error) {
	url := ctx.Build.CatalogURL
	if url == "" {
		url = "https://github.com/" + ctx.Build.ReleaseRepo + "/releases/latest/download/catalog.json"
	}
	c, err := catalog.Fetch(ctx, ctx.HTTP, url)
	if err != nil {
		return nil, fmt.Errorf("could not load the widget catalog: %w", err)
	}
	sys := detectSystem()
	items := []item{}
	for _, e := range c.Widgets {
		it := item{Entry: e}
		it.Installed, it.Method = installedVersion(ctx, appID(ctx, e.ID), "dw-"+e.ID)
		it.Asset = pickAsset(e.Assets, sys, it.Method)
		items = append(items, it)
	}
	return map[string]any{"release": c.Release, "system": sys, "widgets": items}, nil
}

// appID mirrors project.AppID: the library shares the widgets' prefix.
func appID(ctx *dw.Ctx, id string) string {
	prefix := strings.TrimSuffix(ctx.Build.AppID, ".library")
	return prefix + "." + strings.ReplaceAll(id, "-", "_")
}

type system struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Distro  string `json:"distro,omitempty"` // e.g. debian-12, from /etc/os-release
	Family  string `json:"family,omitempty"` // deb | rpm
	Flatpak bool   `json:"flatpak"`
	Sandbox bool   `json:"sandbox"` // library itself runs in Flatpak
}

func detectSystem() system {
	s := system{OS: goruntime.GOOS, Arch: goruntime.GOARCH, Sandbox: os.Getenv("FLATPAK_ID") != ""}
	if s.OS != "linux" {
		return s
	}
	_, err := exec.LookPath("flatpak")
	s.Flatpak = err == nil || s.Sandbox
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return s
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			kv[k] = strings.Trim(v, `"`)
		}
	}
	if kv["ID"] != "" && kv["VERSION_ID"] != "" {
		s.Distro = kv["ID"] + "-" + kv["VERSION_ID"]
	}
	like := " " + kv["ID"] + " " + kv["ID_LIKE"] + " "
	switch {
	case strings.Contains(like, " debian ") || strings.Contains(like, " ubuntu "):
		s.Family = "deb"
	case strings.Contains(like, " fedora ") || strings.Contains(like, " rhel "):
		s.Family = "rpm"
	}
	return s
}

// pickAsset chooses the download: the existing install method first, then
// a native package built for this exact distro, then Flatpak, then tarball.
func pickAsset(assets []catalog.Asset, s system, method string) *catalog.Asset {
	find := func(ok func(a catalog.Asset) bool) *catalog.Asset {
		for i := range assets {
			a := assets[i]
			if a.OS == s.OS && a.Arch == s.Arch && ok(a) {
				return &assets[i]
			}
		}
		return nil
	}
	if s.OS == "windows" {
		return find(func(a catalog.Asset) bool { return a.Format == "msi" })
	}
	if method != "" && method != "deb" && method != "rpm" {
		if a := find(func(a catalog.Asset) bool { return a.Format == method }); a != nil {
			return a
		}
	}
	if !s.Sandbox {
		if a := find(func(a catalog.Asset) bool { return a.Distro == s.Distro && a.Format == s.Family }); a != nil {
			return a
		}
	}
	if s.Flatpak {
		if a := find(func(a catalog.Asset) bool { return a.Format == "flatpak" }); a != nil {
			return a
		}
	}
	return find(func(a catalog.Asset) bool { return a.Format == "tar.gz" })
}

// installedVersion asks the installed binary for its version.
func installedVersion(ctx context.Context, appID, bin string) (version, method string) {
	for _, c := range candidates(appID, bin) {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := command(cctx, c.argv[0], c.argv[1:]...).Output()
		cancel()
		if err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				return v, c.method
			}
		}
	}
	return "", ""
}

type candidate struct {
	method string
	argv   []string
}

func candidates(appID, bin string) []candidate {
	var out []candidate
	if goruntime.GOOS == "windows" {
		exe := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "DesktopWidgets", bin, bin+".exe")
		if _, err := os.Stat(exe); err == nil {
			out = append(out, candidate{"msi", []string{exe, "--version"}})
		}
		return out
	}
	host := func(argv ...string) []string {
		if os.Getenv("FLATPAK_ID") != "" {
			return append([]string{"flatpak-spawn", "--host"}, argv...)
		}
		return argv
	}
	if p := "/usr/bin/" + bin; fileExists(p) || os.Getenv("FLATPAK_ID") != "" {
		out = append(out, candidate{packageFamily(), host(p, "--version")})
	}
	home, _ := os.UserHomeDir()
	out = append(out,
		candidate{"tar.gz", host(filepath.Join(home, ".local", "bin", bin), "--version")},
		candidate{"flatpak", host("flatpak", "run", "--command="+bin, appID, "--version")},
	)
	return out
}

func packageFamily() string {
	if fileExists("/usr/bin/dpkg") {
		return "deb"
	}
	return "rpm"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

type idArgs struct {
	ID string `json:"id"`
}

func install(ctx *dw.Ctx, raw json.RawMessage) (any, error) {
	var a idArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	res, err := list(ctx, nil)
	if err != nil {
		return nil, err
	}
	var it *item
	for _, w := range res.(map[string]any)["widgets"].([]item) {
		if w.ID == a.ID {
			it = &w
			break
		}
	}
	if it == nil || it.Asset == nil {
		return nil, fmt.Errorf("no download of %s is available for this system", a.ID)
	}
	file, err := download(ctx, ctx.HTTP, *it.Asset)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(filepath.Dir(file))
	if err := runInstaller(ctx, it.Asset.Format, file); err != nil {
		return nil, err
	}
	return it.Version, nil
}

// download fetches an asset into a temp dir and verifies its checksum.
func download(ctx context.Context, client *http.Client, a catalog.Asset) (string, error) {
	dir, err := os.MkdirTemp("", "dw-install-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, a.File)
	dctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(dctx, http.MethodGet, a.URL, nil)
	dl := *client
	dl.Timeout = 0 // bounded by dctx instead
	resp, err := dl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", a.File, resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	f.Close()
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); a.SHA256 != "" && got != a.SHA256 {
		return "", errors.New("checksum mismatch, download corrupted")
	}
	return path, nil
}

func runInstaller(ctx context.Context, format, file string) error {
	var argv []string
	switch format {
	case "msi":
		argv = []string{"msiexec", "/i", file, "/passive", "/norestart"}
	case "deb":
		argv = []string{"pkexec", "apt-get", "install", "-y", "--reinstall", file}
	case "rpm":
		argv = []string{"pkexec", "dnf", "install", "-y", file}
	case "flatpak":
		argv = []string{"flatpak", "install", "--user", "-y", "--noninteractive", "--reinstall", file}
	case "tar.gz":
		dir := filepath.Dir(file)
		if out, err := exec.CommandContext(ctx, "tar", "-xzf", file, "-C", dir).CombinedOutput(); err != nil {
			return fmt.Errorf("extract: %v: %s", err, out)
		}
		scripts, _ := filepath.Glob(filepath.Join(dir, "*", "install.sh"))
		if len(scripts) != 1 {
			return errors.New("archive has no install.sh")
		}
		argv = []string{"sh", scripts[0]}
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
	if os.Getenv("FLATPAK_ID") != "" {
		argv = append([]string{"flatpak-spawn", "--host"}, argv...)
	}
	out, err := command(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %v\n%s", argv[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

func launch(ctx *dw.Ctx, raw json.RawMessage) (any, error) {
	var a idArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	id, bin := appID(ctx, a.ID), "dw-"+a.ID
	_, method := installedVersion(ctx, id, bin)
	for _, c := range candidates(id, bin) {
		if c.method != method {
			continue
		}
		argv := c.argv[:len(c.argv)-1] // drop --version
		if method == "flatpak" {
			argv = append(argv[:len(argv)-2], id) // flatpak run <app-id>
		}
		return nil, command(context.Background(), argv[0], argv[1:]...).Start()
	}
	return nil, fmt.Errorf("%s is not installed", a.ID)
}
