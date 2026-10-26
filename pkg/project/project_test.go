package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMatrixExpansion(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "project.yml", "name: X\nappIdPrefix: io.example.x\nreleaseRepo: o/r\n")
	write(t, dir, "platforms.yml", `
targets:
  - id: windows-amd64
    os: windows
    arch: amd64
    runner: windows-latest
    formats: [zip]
  - id: ${DISTRO}-${VERSION}-amd64
    os: linux
    arch: amd64
    runner: ubuntu-24.04
    container: ${DISTRO}:${VERSION}
    formats: [deb]
    matrix:
      - DISTRO: [debian]
        VERSION: ["12", "13"]
      - DISTRO: [ubuntu]
        VERSION: ["24.04"]
`)
	p, targets, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, tg := range targets {
		ids = append(ids, tg.ID)
	}
	want := []string{"windows-amd64", "debian-12-amd64", "debian-13-amd64", "ubuntu-24.04-amd64"}
	if len(ids) != len(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("got %v, want %v", ids, want)
		}
	}
	if targets[2].Container != "debian:13" {
		t.Fatalf("container not substituted: %q", targets[2].Container)
	}
	if p.CatalogURL() != "https://github.com/o/r/releases/latest/download/catalog.json" {
		t.Fatal(p.CatalogURL())
	}
}

func TestMatrixErrors(t *testing.T) {
	cases := map[string]string{
		"undefined var":   "targets:\n  - {id: x-${NOPE}, os: linux, arch: amd64, runner: r, formats: [deb]}\n",
		"wrong format":    "targets:\n  - {id: w, os: windows, arch: amd64, runner: r, formats: [deb]}\n",
		"flatpak section": "targets:\n  - {id: f, os: linux, arch: amd64, runner: r, formats: [flatpak]}\n",
		"duplicate":       "targets:\n  - {id: a, os: linux, arch: amd64, runner: r, formats: [deb]}\n  - {id: a, os: linux, arch: amd64, runner: r, formats: [rpm]}\n",
	}
	for name, src := range cases {
		dir := t.TempDir()
		write(t, dir, "project.yml", "name: X\nappIdPrefix: io.example.x\nreleaseRepo: o/r\n")
		write(t, dir, "platforms.yml", src)
		if _, _, err := Load(dir); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestRepositoryPlatforms(t *testing.T) {
	// The real platforms.yml must always load.
	if _, targets, err := Load("../.."); err != nil || len(targets) == 0 {
		t.Fatalf("platforms.yml: %v", err)
	}
}

func TestBuildImage(t *testing.T) {
	a := Target{ID: "debian-12-amd64", Container: "debian:12", Setup: "apt-get install x"}
	b := a
	b.Setup = "apt-get install y"
	ia, ib := a.BuildImage("ghcr.io/CDRO/Repo/build"), b.BuildImage("ghcr.io/CDRO/Repo/build")
	if ia == ib || !strings.HasPrefix(ia, "ghcr.io/cdro/repo/build:debian-12-amd64-") {
		t.Fatalf("unexpected images %q %q", ia, ib)
	}
	if ia != a.BuildImage("ghcr.io/cdro/repo/build") {
		t.Fatal("image name must be stable")
	}
	if (Target{ID: "windows-amd64"}).BuildImage("r") != "" {
		t.Fatal("host targets have no image")
	}
}
