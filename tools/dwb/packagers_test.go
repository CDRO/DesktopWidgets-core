package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdro/desktopwidgets-core/pkg/manifest"
	"github.com/cdro/desktopwidgets-core/pkg/project"
)

func TestMSITemplateIsValidXML(t *testing.T) {
	m, err := manifest.Parse([]byte("id: clock\nname: Clock & Co <test>\nversion: 2.1.3\nwindow: {width: 1, height: 1}\n"))
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs("../..")
	c := &pkgCtx{
		r: &repo{Root: root, Project: &project.Project{AppIDPrefix: "io.example", Vendor: "V", Homepage: "https://example.com"}},
		u: &unit{M: m}, t: &project.Target{ID: "windows-amd64", OS: "windows", Arch: "amd64"},
		bin: `C:\build\dw-clock.exe`, format: "msi",
	}
	data, err := renderTemplate(c, "msi/widget.wxs.tmpl", map[string]string{
		"Name": "Clock &amp; Co &lt;test&gt;", "Description": "d", "Vendor": "V", "Homepage": "h",
		"Version": "2.1.3", "Binary": "dw-clock", "UpgradeCode": uuidV5("x"), "ExeGuid": uuidV5("y"),
		"ShortcutGuid": uuidV5("z"), "Exe": c.bin, "Ico": "icon.ico",
	})
	if err != nil {
		t.Fatal(err)
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		if _, err := dec.Token(); err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("invalid XML: %v\n%s", err, data)
		}
	}
}

func TestTarballInstallScriptRenders(t *testing.T) {
	root, _ := filepath.Abs("../..")
	c := &pkgCtx{r: &repo{Root: root}}
	data, err := renderTemplate(c, "linux/install.sh.tmpl", map[string]string{"Name": "Clock", "Binary": "dw-clock", "AppID": "io.example.clock"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "{{") || !strings.HasPrefix(string(data), "#!/bin/sh") {
		t.Fatalf("bad script:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(root, "packaging", "linux", "widget.desktop.tmpl")); err != nil {
		t.Fatal(err)
	}
}
