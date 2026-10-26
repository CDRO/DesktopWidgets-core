package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/cdro/desktopwidgets-core/pkg/catalog"
	"github.com/cdro/desktopwidgets-core/pkg/project"
)

// buildResult is written to <out>/build-<target>.json and consumed by
// `dwb catalog` in the publish job.
type buildResult struct {
	Target project.Target `json:"target"`
	Assets []builtAsset   `json:"assets"`
}

type builtAsset struct {
	Unit  string        `json:"unit"`
	Asset catalog.Asset `json:"asset"`
}

// pkgCtx is everything a packager needs for one unit on one target.
type pkgCtx struct {
	r      *repo
	u      *unit
	t      *project.Target
	bin    string // compiled binary ("" for formats that compile themselves)
	out    string // output directory for release assets
	work   string // scratch directory for this unit/target
	format string
}

func (c *pkgCtx) file() string  { return filepath.Join(c.out, assetName(c.u.M, c.t.ID, c.format)) }
func (c *pkgCtx) appID() string { return c.r.Project.AppID(c.u.M) }
func (c *pkgCtx) icon() string {
	return filepath.Join(c.r.Root, filepath.FromSlash(c.u.Dir), "icon.png")
}

var packagers = map[string]func(*pkgCtx) error{
	"zip":     packageZip,
	"tar.gz":  packageTarGz,
	"deb":     packageNfpm,
	"rpm":     packageNfpm,
	"flatpak": packageFlatpak,
	"msi":     packageMSI,
}

// selfCompiling formats build the binary themselves (inside a sandbox).
var selfCompiling = map[string]bool{"flatpak": true}

func cmdBuild(args []string) error {
	fl := flag.NewFlagSet("build", flag.ExitOnError)
	targetID := fl.String("target", "", "target id from platforms.yml")
	widgets := fl.String("widgets", "", "comma-separated unit ids (default: all enabled for the target)")
	out := fl.String("out", "dist", "output directory")
	formats := fl.String("formats", "", "comma-separated subset of the target's formats")
	fl.Parse(args)

	r, err := loadRepo()
	if err != nil {
		return err
	}
	t, err := r.target(*targetID)
	if err != nil {
		return err
	}
	units, err := selectUnits(r, t, *widgets)
	if err != nil {
		return err
	}
	fmts := t.Formats
	if *formats != "" {
		fmts = splitList(*formats)
	}
	outDir, _ := filepath.Abs(*out)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	res := buildResult{Target: *t}
	for _, u := range units {
		work := filepath.Join(outDir, ".work", t.ID, u.M.ID)
		if err := os.MkdirAll(work, 0o755); err != nil {
			return err
		}
		if _, err := generate(r, u, genOptions{Release: true}); err != nil {
			return err
		}
		var bin string
		for _, f := range fmts {
			if !selfCompiling[f] && bin == "" {
				fmt.Printf("==> compile %s for %s\n", u.M.ID, t.ID)
				if bin, err = compile(r, u, t, work); err != nil {
					return fmt.Errorf("compile %s: %w", u.M.ID, err)
				}
			}
		}
		for _, f := range fmts {
			pack, ok := packagers[f]
			if !ok {
				return fmt.Errorf("unknown format %q", f)
			}
			c := &pkgCtx{r: r, u: u, t: t, bin: bin, out: outDir, work: work, format: f}
			fmt.Printf("==> package %s as %s\n", u.M.ID, filepath.Base(c.file()))
			if err := pack(c); err != nil {
				return fmt.Errorf("package %s %s: %w", u.M.ID, f, err)
			}
			a, err := describe(c)
			if err != nil {
				return err
			}
			res.Assets = append(res.Assets, builtAsset{Unit: u.M.ID, Asset: a})
		}
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	return os.WriteFile(filepath.Join(outDir, "build-"+t.ID+".json"), data, 0o644)
}

// selectUnits returns the requested units that are enabled for the target.
func selectUnits(r *repo, t *project.Target, list string) ([]*unit, error) {
	var out []*unit
	if list == "" {
		for _, u := range r.Units {
			if u.M.Platforms.Matches(t.ID, t.OS) {
				out = append(out, u)
			}
		}
		return out, nil
	}
	for _, id := range splitList(list) {
		u, err := r.unit(id)
		if err != nil {
			return nil, err
		}
		if u.M.Platforms.Matches(t.ID, t.OS) {
			out = append(out, u)
		}
	}
	return out, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// compile builds gen/<id> for the target and returns the binary path.
func compile(r *repo, u *unit, t *project.Target, work string) (string, error) {
	genDir := filepath.Join(r.Root, "gen", u.M.ID)
	name := u.M.Binary()
	tags := []string{"production"}
	ldflags := "-s -w"
	env := append(os.Environ(), "GOOS="+t.OS, "GOARCH="+t.Arch)
	switch t.OS {
	case "windows":
		name += ".exe"
		ldflags += " -H windowsgui"
		env = append(env, "CGO_ENABLED=0")
		if err := writeSyso(r, u, t, genDir); err != nil {
			return "", err
		}
	case "linux":
		tags = append(tags, "gtk3")
		env = append(env, "CGO_ENABLED=1")
	}
	bin := filepath.Join(work, "bin", name)
	// -buildvcs=false: CI containers check out the repo as another user,
	// which git rejects ("dubious ownership"), failing VCS stamping.
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-tags", strings.Join(tags, ","),
		"-ldflags", ldflags, "-o", bin, "./gen/"+u.M.ID)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = r.Root, env, os.Stdout, os.Stderr
	return bin, cmd.Run()
}

// writeSyso embeds icon, manifest and version info into the Windows exe.
func writeSyso(r *repo, u *unit, t *project.Target, genDir string) error {
	f, err := os.Open(filepath.Join(genDir, "icon.png"))
	if err != nil {
		return err
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	icon, err := winres.NewIconFromResizedImage(img, nil)
	if err != nil {
		return err
	}
	rs := winres.ResourceSet{}
	// 32512 is IDI_APPLICATION, which the Wails window class loads.
	if err := rs.SetIcon(winres.ID(32512), icon); err != nil {
		return err
	}
	rs.SetManifest(winres.AppManifest{
		DPIAwareness:        winres.DPIPerMonitorV2,
		ExecutionLevel:      winres.AsInvoker,
		UseCommonControlsV6: true,
	})
	var vi version.Info
	v := u.M.ParsedVersion().String() + ".0"
	vi.SetFileVersion(v)
	vi.SetProductVersion(v)
	vi.Set(version.LangDefault, version.ProductName, u.M.Name)
	vi.Set(version.LangDefault, version.FileDescription, u.M.Name)
	vi.Set(version.LangDefault, version.CompanyName, r.Project.Vendor)
	vi.Set(version.LangDefault, version.OriginalFilename, u.M.Binary()+".exe")
	rs.SetVersionInfo(vi)

	arch := map[string]winres.Arch{"amd64": winres.ArchAMD64, "arm64": winres.ArchARM64}[t.Arch]
	out, err := os.Create(filepath.Join(genDir, "rsrc_windows_"+t.Arch+".syso"))
	if err != nil {
		return err
	}
	defer out.Close()
	return rs.WriteObject(out, arch)
}

// describe hashes a produced file into a catalog asset (URL is filled in by
// `dwb catalog`, which knows the release tag).
func describe(c *pkgCtx) (catalog.Asset, error) {
	path := c.file()
	f, err := os.Open(path)
	if err != nil {
		return catalog.Asset{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return catalog.Asset{}, err
	}
	return catalog.Asset{
		Target: c.t.ID, OS: c.t.OS, Arch: c.t.Arch, Distro: c.t.Distro,
		Format: c.format, File: filepath.Base(path), Size: n, SHA256: hex.EncodeToString(h.Sum(nil)),
	}, nil
}
