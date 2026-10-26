# DesktopWidgets core

Source of the DesktopWidgets project: the shared widget runtime, the widgets,
the Widget Library app and the build tooling. Releases are published to
[cdro/DesktopWidgets](https://github.com/cdro/DesktopWidgets).

Widgets are small frameless windows on the desktop. You write them in
HTML/CSS/JS, plus optional Go for data sources. The shared runtime
([Wails v3](https://v3.wails.io) + the system webview) handles the platform
work. Each widget is its own binary, built for Windows and Linux.

```
widgets/<id>/          one folder per widget, nothing else needed
  widget.yml           manifest: id, version, window, settings, providers
  icon.png             256×256
  frontend/            index.html + whatever it loads
  *.go                 optional data providers / handlers
library/               the Widget Library app (optional catalog + installer)
runtime/               shared host: window, desktop pinning, settings, updates
sdk/js/                frontend SDK served to every widget at /dw/
pkg/                   manifest, project/matrix and catalog formats
tools/dwb/             build tool: plan, generate, compile, package, catalog
packaging/             MSI / desktop-file / install.sh templates
platforms.yml          build matrix (OS × distro × format)
project.yml            app-id prefix, release repo, vendor
```

## Quick start

Requirements: Go (version from `go.mod`). On Linux you also need the GTK 3 and
WebKitGTK 4.1 headers (`libgtk-3-dev libwebkit2gtk-4.1-dev`, or
`gtk3-devel webkit2gtk4.1-devel` on Fedora). On Windows you need the WebView2
runtime, which ships with Windows 10 and 11.

```sh
go run ./tools/dwb list                 # widgets and versions
go run ./tools/dwb run clock            # run a widget locally
go run ./tools/dwb new my-widget        # scaffold widgets/my-widget from the template
go run ./tools/dwb targets              # show the expanded build matrix
go run ./tools/dwb build --target linux-amd64   # build + package into dist/
```

Linux builds always use the `gtk3` build tag. `dwb` sets it for you; pass
`-tags gtk3` if you call `go build`, `go vet` or `go test` yourself.

## How releases work

1. Change a widget and bump its `version` in `widget.yml`. CI fails a PR that
   changes a widget without bumping it.
2. Push a tag such as `v1.4.0` on this repository.
3. `release.yml` compares each widget's version with the latest published
   `catalog.json`. Only widgets with a new version are built, for every target
   in `platforms.yml`.
4. The same tag is created in the release repository. Its README and
   `catalog.json` are updated, and the packages, `catalog.json` and
   `SHA256SUMS` are attached to the GitHub release.

Repository tags (`vX.Y.Z`) version the project as a whole. Each widget has its
own version, so users are only asked to update widgets that actually changed.
See [docs/releasing.md](docs/releasing.md) for versioning rules and the
one-time token setup.

## Documentation

- [docs/widgets.md](docs/widgets.md): writing a widget (manifest, SDK, providers, settings)
- [docs/platforms.md](docs/platforms.md): build matrix, formats and platform notes
- [docs/releasing.md](docs/releasing.md): versioning, tagging, release setup
