# Writing a widget

A widget is a folder under `widgets/` whose name equals its `id`. Creating
the folder is all it takes: `dwb` finds it, generates its entry point and
packages it for every target. The next release tag publishes it.

```sh
go run ./tools/dwb new world-clock --name "World Clock"
go run ./tools/dwb run world-clock
```

## Files

| File | Required | Purpose |
| --- | --- | --- |
| `widget.yml` | yes | Manifest (below) |
| `icon.png` | yes | 256×256 icon: menus, installers, library |
| `frontend/index.html` | yes | Entry page; everything under `frontend/` is embedded |
| `*.go` | no | Data providers and call handlers, package name = id without dashes |

## `widget.yml`

```yaml
id: stock-ticker            # kebab-case, equals the folder name
name: Stock Ticker
description: Live quotes for a list of symbols.
version: 1.0.0              # see "Versions" below
category: finance           # grouping in the library
kind: widget                # widget (default) | app (framed window, e.g. the library)

window:
  width: 300
  height: 220
  resizable: false
  transparent: true         # transparent background, draw your own card
  layer: bottom             # bottom = on the desktop | normal | top = always on top
  position: top-right       # first start only; user drags are remembered

settings:                   # the settings window is generated from this list
  - key: symbols
    type: string            # bool | string | number | enum | secret
    label: Symbols
    description: Comma-separated
    default: AAPL, MSFT
  - key: refresh
    type: number
    default: 15
    min: 5
    max: 3600
  - key: source
    type: enum
    options: [yahoo, finnhub]
    default: yahoo

providers:                  # Go data sources (optional)
  - name: quotes
    intervalSetting: refresh    # seconds, from a number setting
  # - name: other
  #   interval: 30s             # or a fixed interval

platforms: all              # or {include: [...], exclude: [...]} with target ids or OS names

release:
  notes: Adds sparklines.   # shown in update prompts and release notes
  critical: false           # true = prompt even for a minor/patch update (security fixes)
```

Unknown fields are rejected, so typos fail the build.

## Versions

- Start at `1.0.0`.
- **New feature → bump the major version** (`1.x` → `2.0.0`). Users see a
  visible update badge on the widget.
- **Small fix → minor or patch** (`2.0.0` → `2.0.1`). It is offered quietly in
  the widget's right-click menu and in the library, but users get no badge.
- For a **security fix**, bump minor/patch and set `release.critical: true`,
  so users get a visible prompt even for a small version change.
- A version must never go down. CI fails a PR that changes a widget without
  bumping its version (`dwb check-versions`).

Only widgets whose version differs from the published catalog are built and
released, so a new widget or a fix to one widget doesn't touch the others.

## Frontend SDK

Every page can import the SDK from `/dw/dw.js`. Load the Wails runtime first:

```html
<link rel="stylesheet" href="style.css" />
<script type="module" src="/wails/runtime.js"></script>
<script type="module" src="app.js"></script>
```

```js
import * as dw from "/dw/dw.js";

dw.subscribe("quotes", (data, { error, at }) => render(data)); // provider data
dw.onSettings((settings) => apply(settings));                  // now + on change
dw.refresh("quotes");                                          // run a provider now
const result = await dw.call("myHandler", { any: "args" });    // Go handler
dw.onUpdate((u) => showMyOwnBanner(u));                        // replaces the default badge
dw.openURL("https://example.com");                             // system browser
await dw.ready;                                                // handshake done; dw.widget() has info
```

The SDK also injects `/dw/dw.css`, which gives you:

- a transparent page, so the whole widget can be dragged to move it (unless
  the user locked its position);
- the runtime's right-click menu: settings, refresh, lock position, start at
  login, update and quit;
- CSS variables such as `--dw-fg`, `--dw-fg-muted`, `--dw-bg`, `--dw-accent`,
  `--dw-up`, `--dw-down` and `--dw-radius`, and a `.dw-card` class for a
  rounded translucent card;
- `button`, `input`, `a` and `.dw-no-drag` elements, which stay clickable
  instead of starting a drag.

Pages that are not widgets, such as the library, opt out with
`<html data-dw-plain>`.

## Go providers and handlers

```go
package stockticker

import dw "github.com/cdro/desktopwidgets-core/runtime"

func init() {
	// Runs on the interval from widget.yml; the result goes to dw.subscribe("quotes").
	dw.Provide("quotes", func(ctx *dw.Ctx) (any, error) {
		symbols := ctx.String("symbols") // settings: String, Float, Bool, Setting
		return fetch(ctx, ctx.HTTP, symbols)
	})

	// Called from the frontend with dw.call("lookup", {...}).
	dw.Handle("lookup", func(ctx *dw.Ctx, args json.RawMessage) (any, error) { ... })
}
```

- `ctx` is a `context.Context`: each run times out after 30 s and is
  cancelled on quit.
- If a provider returns an error, the frontend gets the error together with
  the last good data, so it can show stale values.
- Providers re-run immediately when settings change.
- Every provider in `widget.yml` must be registered in Go, and every
  registered provider must be declared there. Mismatches stop the widget at
  startup with a clear message.

Keep platform-specific code out of widgets. If something needs OS APIs, add
it to `runtime/` so every widget benefits.
