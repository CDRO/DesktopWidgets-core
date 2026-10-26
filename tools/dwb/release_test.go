package main

import (
	"strings"
	"testing"

	"github.com/cdro/desktopwidgets-core/pkg/catalog"
	"github.com/cdro/desktopwidgets-core/pkg/manifest"
)

func testRepo(t *testing.T, versions map[string]string) *repo {
	t.Helper()
	r := &repo{}
	for id, v := range versions {
		m, err := manifest.Parse([]byte("id: " + id + "\nname: N\nversion: " + v + "\nwindow: {width: 1, height: 1}\n"))
		if err != nil {
			t.Fatal(err)
		}
		r.Units = append(r.Units, &unit{M: m, Dir: "widgets/" + id})
	}
	return r
}

func TestChangedUnits(t *testing.T) {
	r := testRepo(t, map[string]string{"clock": "2.0.0", "ticker": "1.0.0", "fresh": "1.0.0"})
	prev := &catalog.Catalog{Widgets: []catalog.Entry{{ID: "clock", Version: "1.0.0"}, {ID: "ticker", Version: "1.0.0"}}}
	got, err := changedUnits(r, prev, false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, u := range got {
		ids = append(ids, u.M.ID)
	}
	if s := strings.Join(ids, ","); !strings.Contains(s, "clock") || !strings.Contains(s, "fresh") || strings.Contains(s, "ticker") {
		t.Fatalf("got %s", s)
	}
	all, _ := changedUnits(r, prev, true)
	if len(all) != 3 {
		t.Fatalf("--all should select every unit, got %d", len(all))
	}
}

func TestChangedUnitsRejectsDowngrade(t *testing.T) {
	r := testRepo(t, map[string]string{"clock": "1.0.0"})
	prev := &catalog.Catalog{Widgets: []catalog.Entry{{ID: "clock", Version: "2.0.0"}}}
	if _, err := changedUnits(r, prev, false); err == nil {
		t.Fatal("expected downgrade error")
	}
}

func TestUUIDv5Stable(t *testing.T) {
	a, b := uuidV5("io.example.clock/upgrade"), uuidV5("io.example.clock/upgrade")
	if a != b || len(a) != 36 || a[14] != '5' {
		t.Fatalf("unexpected uuid %s", a)
	}
	if a == uuidV5("io.example.other/upgrade") {
		t.Fatal("different names must give different uuids")
	}
}
