package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/goreleaser/nfpm/v2"
	_ "github.com/goreleaser/nfpm/v2/deb"
	"github.com/goreleaser/nfpm/v2/files"
	_ "github.com/goreleaser/nfpm/v2/rpm"
	"github.com/tc-hib/winres"
)

func renderTemplate(c *pkgCtx, name string, data any) ([]byte, error) {
	t, err := template.ParseFiles(filepath.Join(c.r.Root, "packaging", filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, data)
	return buf.Bytes(), err
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// desktopEntry renders the .desktop file with the given Exec line.
func desktopEntry(c *pkgCtx, exec string) ([]byte, error) {
	return renderTemplate(c, "linux/widget.desktop.tmpl", map[string]string{
		"Name": oneLine(c.u.M.Name), "Description": oneLine(c.u.M.Description),
		"Exec": exec, "Icon": c.appID(), "Binary": c.u.M.Binary(),
	})
}

// ---- zip (Windows portable) ----

func packageZip(c *pkgCtx) error {
	f, err := os.Create(c.file())
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	if err := addZipFile(zw, c.bin, filepath.Base(c.bin)); err != nil {
		return err
	}
	return zw.Close()
}

func addZipFile(zw *zip.Writer, src, name string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, _ := in.Stat()
	h, _ := zip.FileInfoHeader(st)
	h.Name, h.Method = name, zip.Deflate
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

// ---- tar.gz (Linux portable, with user-level install.sh) ----

func packageTarGz(c *pkgCtx) error {
	desktop, err := desktopEntry(c, c.u.M.Binary())
	if err != nil {
		return err
	}
	install, err := renderTemplate(c, "linux/install.sh.tmpl", map[string]string{
		"Name": c.u.M.Name, "Binary": c.u.M.Binary(), "AppID": c.appID(),
	})
	if err != nil {
		return err
	}
	icon, err := os.ReadFile(c.icon())
	if err != nil {
		return err
	}
	bin, err := os.ReadFile(c.bin)
	if err != nil {
		return err
	}

	f, err := os.Create(c.file())
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	dir := c.u.M.Binary() + "/"
	entries := []struct {
		name string
		mode int64
		data []byte
	}{
		{dir + c.u.M.Binary(), 0o755, bin},
		{dir + "install.sh", 0o755, install},
		{dir + c.appID() + ".desktop", 0o644, desktop},
		{dir + c.appID() + ".png", 0o644, icon},
	}
	now := time.Now()
	if err := tw.WriteHeader(&tar.Header{Name: dir, Mode: 0o755, Typeflag: tar.TypeDir, ModTime: now}); err != nil {
		return err
	}
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.data)), ModTime: now}); err != nil {
			return err
		}
		if _, err := tw.Write(e.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// ---- deb / rpm via nfpm ----

func packageNfpm(c *pkgCtx) error {
	desktopPath := filepath.Join(c.work, c.appID()+".desktop")
	desktop, err := desktopEntry(c, "/usr/bin/"+c.u.M.Binary())
	if err != nil {
		return err
	}
	if err := os.WriteFile(desktopPath, desktop, 0o644); err != nil {
		return err
	}
	p := c.r.Project
	info := &nfpm.Info{
		Name:        c.u.M.Binary(),
		Arch:        c.t.Arch,
		Platform:    "linux",
		Version:     c.u.M.ParsedVersion().String(),
		Release:     "1",
		Section:     "utils",
		Maintainer:  p.Maintainer,
		Description: c.u.M.Description,
		Vendor:      p.Vendor,
		Homepage:    p.Homepage,
		License:     p.License,
		Overridables: nfpm.Overridables{
			Depends: c.t.Depends,
			Contents: files.Contents{
				{Source: c.bin, Destination: "/usr/bin/" + c.u.M.Binary(), FileInfo: &files.ContentFileInfo{Mode: 0o755}},
				{Source: desktopPath, Destination: "/usr/share/applications/" + c.appID() + ".desktop", FileInfo: &files.ContentFileInfo{Mode: 0o644}},
				{Source: c.icon(), Destination: "/usr/share/icons/hicolor/256x256/apps/" + c.appID() + ".png", FileInfo: &files.ContentFileInfo{Mode: 0o644}},
			},
		},
	}
	info = nfpm.WithDefaults(info)
	if err := nfpm.PrepareForPackager(info, c.format); err != nil {
		return err
	}
	pkgr, err := nfpm.Get(c.format)
	if err != nil {
		return err
	}
	f, err := os.Create(c.file())
	if err != nil {
		return err
	}
	defer f.Close()
	return pkgr.Package(info, f)
}

// ---- flatpak (compiled inside the GNOME SDK sandbox) ----

const flathubRepo = "https://dl.flathub.org/repo/flathub.flatpakrepo"

func packageFlatpak(c *pkgCtx) error {
	fp := c.t.Flatpak
	id, bin := c.appID(), c.u.M.Binary()
	desktop, err := desktopEntry(c, bin)
	if err != nil {
		return err
	}
	dir := filepath.Join(c.work, "flatpak")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, id+".desktop"), desktop, 0o644); err != nil {
		return err
	}
	if err := copyFile(c.icon(), filepath.Join(dir, id+".png")); err != nil {
		return err
	}
	build := "/run/build/" + bin
	manifest := map[string]any{
		"id":              id,
		"runtime":         fp.Runtime,
		"runtime-version": fp.RuntimeVersion,
		"sdk":             fp.SDK,
		"sdk-extensions":  []string{fp.GoExtension},
		"command":         bin,
		// X11 only for now (see runtime/layer_linux.go).
		"finish-args": []string{"--share=ipc", "--socket=x11", "--share=network", "--device=dri"},
		"modules": []any{map[string]any{
			"name":        bin,
			"buildsystem": "simple",
			"build-options": map[string]any{
				"append-path": "/usr/lib/sdk/golang/bin",
				"build-args":  []string{"--share=network"}, // GOTOOLCHAIN download
				"env": map[string]string{
					"GOTOOLCHAIN": "auto", "GOFLAGS": "-mod=vendor", "CGO_ENABLED": "1",
					"GOPATH": build + "/.gopath", "GOCACHE": build + "/.gocache",
				},
			},
			"build-commands": []string{
				"go build -trimpath -tags gtk3,production -ldflags='-s -w' -o /app/bin/" + bin + " ./gen/" + c.u.M.ID,
				"install -Dm644 " + id + ".desktop /app/share/applications/" + id + ".desktop",
				"install -Dm644 " + id + ".png /app/share/icons/hicolor/256x256/apps/" + id + ".png",
			},
			"sources": []any{
				map[string]any{"type": "dir", "path": c.r.Root, "skip": []string{".git", "dist", ".flatpak-builder", "library/node_modules"}},
				map[string]any{"type": "file", "path": filepath.Join(dir, id+".desktop")},
				map[string]any{"type": "file", "path": filepath.Join(dir, id+".png")},
			},
		}},
	}
	mpath := filepath.Join(dir, id+".json")
	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(mpath, data, 0o644); err != nil {
		return err
	}
	repoDir := filepath.Join(dir, "repo")
	// Modules come from the host's (CI-cached) module cache via vendor/, so
	// the sandbox does not download them for every widget.
	vendor := exec.Command("go", "mod", "vendor")
	vendor.Dir, vendor.Stdout, vendor.Stderr = c.r.Root, os.Stdout, os.Stderr
	if err := vendor.Run(); err != nil {
		return fmt.Errorf("go mod vendor: %w", err)
	}
	defer os.RemoveAll(filepath.Join(c.r.Root, "vendor"))

	// As root (the CI build image) use the system installation, where the
	// runtime and SDK are preinstalled; otherwise the user's.
	scope := "--user"
	if os.Geteuid() == 0 {
		scope = "--system"
	}
	steps := [][]string{
		{"flatpak", "remote-add", scope, "--if-not-exists", "flathub", flathubRepo},
		{"flatpak-builder", scope, "--install-deps-from=flathub", "--force-clean", "--disable-rofiles-fuse",
			"--state-dir=" + filepath.Join(c.out, ".work", ".flatpak-builder"),
			"--repo=" + repoDir, filepath.Join(dir, "build"), mpath},
		{"flatpak", "build-bundle", "--runtime-repo=" + flathubRepo, repoDir, c.file(), id},
	}
	for _, s := range steps {
		cmd := exec.Command(s[0], s[1:]...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = c.r.Root, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(s[:2], " "), err)
		}
	}
	return nil
}

// ---- msi via WiX (Windows runners only) ----

func packageMSI(c *pkgCtx) error {
	ico := filepath.Join(c.work, c.u.M.Binary()+".ico")
	if err := writeICO(c.icon(), ico); err != nil {
		return err
	}
	esc := func(s string) string {
		var b bytes.Buffer
		xml.EscapeText(&b, []byte(oneLine(s)))
		return b.String()
	}
	appID := c.appID()
	wxs, err := renderTemplate(c, "msi/widget.wxs.tmpl", map[string]string{
		"Name":         esc(c.u.M.Name),
		"Description":  esc(c.u.M.Description),
		"Vendor":       esc(c.r.Project.Vendor),
		"Homepage":     esc(c.r.Project.Homepage),
		"Version":      c.u.M.ParsedVersion().String(),
		"Binary":       c.u.M.Binary(),
		"UpgradeCode":  uuidV5(appID + "/upgrade"),
		"ExeGuid":      uuidV5(appID + "/component/exe"),
		"ShortcutGuid": uuidV5(appID + "/component/shortcut"),
		"Exe":          esc(c.bin),
		"Ico":          esc(ico),
	})
	if err != nil {
		return err
	}
	wxsPath := filepath.Join(c.work, c.u.M.Binary()+".wxs")
	if err := os.WriteFile(wxsPath, wxs, 0o644); err != nil {
		return err
	}
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[c.t.Arch]
	// -pdb keeps the debug symbols file out of the release directory.
	pdb := filepath.Join(c.work, c.u.M.Binary()+".wixpdb")
	cmd := exec.Command("wix", "build", "-arch", arch, "-pdb", pdb, "-o", c.file(), wxsPath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wix build (is the WiX toolset installed? see platforms.yml setup): %w", err)
	}
	return nil
}

func writeICO(pngPath, icoPath string) error {
	f, err := os.Open(pngPath)
	if err != nil {
		return err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return err
	}
	icon, err := winres.NewIconFromResizedImage(img, nil)
	if err != nil {
		return err
	}
	out, err := os.Create(icoPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return icon.SaveICO(out)
}

// uuidV5 derives a stable GUID from a name (RFC 4122, URL namespace), so
// MSI upgrade codes never change between releases.
func uuidV5(name string) string {
	ns := []byte{0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	h := sha1.Sum(append(ns, name...))
	u := h[:16]
	u[6] = (u[6] & 0x0f) | 0x50
	u[8] = (u[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16]))
}
