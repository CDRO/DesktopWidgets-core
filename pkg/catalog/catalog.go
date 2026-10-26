// Package catalog defines catalog.json, the cumulative index of every widget
// and its downloadable assets. It is attached to each release in the release
// repository and read by widgets (update checks) and the library app.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"
)

const Schema = 1

type Catalog struct {
	Schema    int       `json:"schema"`
	Release   string    `json:"release"` // core/release tag that produced this catalog
	Generated time.Time `json:"generated"`
	Widgets   []Entry   `json:"widgets"`
}

type Entry struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Category    string  `json:"category,omitempty"`
	Version     string  `json:"version"`
	Released    string  `json:"released"`   // tag that published this version
	ReleaseURL  string  `json:"releaseUrl"` // human-readable release page
	Notes       string  `json:"notes,omitempty"`
	Critical    bool    `json:"critical,omitempty"`
	Icon        string  `json:"icon,omitempty"`
	Assets      []Asset `json:"assets"`
}

type Asset struct {
	Target string `json:"target"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Distro string `json:"distro,omitempty"`
	Format string `json:"format"`
	File   string `json:"file"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (c *Catalog) Find(id string) *Entry {
	for i := range c.Widgets {
		if c.Widgets[i].ID == id {
			return &c.Widgets[i]
		}
	}
	return nil
}

// Upsert replaces or adds an entry, keeping the list sorted by id.
func (c *Catalog) Upsert(e Entry) {
	if old := c.Find(e.ID); old != nil {
		*old = e
	} else {
		c.Widgets = append(c.Widgets, e)
	}
	sort.Slice(c.Widgets, func(i, j int) bool { return c.Widgets[i].ID < c.Widgets[j].ID })
}

func Read(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decode(data)
}

func Fetch(ctx context.Context, client *http.Client, url string) (*Catalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch catalog: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	return decode(data)
}

func decode(data []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	if c.Schema > Schema {
		return nil, fmt.Errorf("catalog schema %d is newer than supported (%d)", c.Schema, Schema)
	}
	return &c, nil
}

func (c *Catalog) Write(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
