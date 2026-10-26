package manifest

import (
	"strings"
	"testing"
)

const valid = `
id: stock-ticker
name: Stock Ticker
version: 3
window: {width: 300, height: 200}
settings:
  - {key: refresh, type: number, default: 15}
providers:
  - {name: quotes, intervalSetting: refresh}
platforms: {exclude: [windows]}
`

func TestParseDefaults(t *testing.T) {
	m, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindWidget || m.Window.Layer != "bottom" || m.Window.Position != "center" {
		t.Fatalf("defaults not applied: %+v", m)
	}
	if m.ParsedVersion().String() != "3.0.0" || m.Binary() != "dw-stock-ticker" || m.GoPackage() != "stockticker" {
		t.Fatalf("derived values wrong: %s %s %s", m.ParsedVersion(), m.Binary(), m.GoPackage())
	}
	if m.Platforms.Matches("windows-amd64", "windows") || !m.Platforms.Matches("debian-12-amd64", "linux") {
		t.Fatal("platform filter wrong")
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"bad id":           strings.Replace(valid, "stock-ticker", "Stock_Ticker", 1),
		"version zero":     strings.Replace(valid, "version: 3", "version: 0.9.0", 1),
		"unknown field":    valid + "colour: red\n",
		"bad provider ref": strings.Replace(valid, "intervalSetting: refresh", "intervalSetting: nope", 1),
		"short interval":   strings.Replace(valid, "intervalSetting: refresh", "interval: 100ms", 1),
		"bad layer":        strings.Replace(valid, "height: 200}", "height: 200, layer: floor}", 1),
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestPlatformsAll(t *testing.T) {
	m, err := Parse([]byte(strings.Replace(valid, "platforms: {exclude: [windows]}", "platforms: all", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Platforms.Matches("windows-amd64", "windows") {
		t.Fatal(`"all" should match every target`)
	}
}

func TestVersions(t *testing.T) {
	v := func(s string) Version {
		x, err := ParseVersion(s)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	if v("v2.1").Compare(v("2.1.0")) != 0 || v("2.0.1").Compare(v("2.0.0")) != 1 || v("1.9.9").Compare(v("2")) != -1 {
		t.Fatal("compare")
	}
	if v("1.0.0").UpdateTo(v("2.0.0")) != UpdateMajor || v("2.0.0").UpdateTo(v("2.0.1")) != UpdateMinor || v("2.0.0").UpdateTo(v("2.0.0")) != UpdateNone {
		t.Fatal("update kind")
	}
	for _, bad := range []string{"", "1.2.3.4", "a.b", "-1"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}
