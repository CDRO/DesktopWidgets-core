# Platforms and the build matrix

`platforms.yml` lists build targets. Each target is one CI job that builds
every (changed) widget and packages it in the listed formats.

```yaml
targets:
  - id: windows-amd64          # used in asset names and widget.yml platform filters
    os: windows                # windows | linux
    arch: amd64                # amd64 | arm64 (use a matching runner, no cross-compiling with cgo)
    runner: windows-latest     # GitHub Actions runner label
    container: ""              # optional Docker image the job runs in
    setup: "..."               # optional shell command to install build deps
    formats: [msi, zip]
    depends: [...]             # deb/rpm runtime dependencies
```

## Matrices

`matrix` works like GitLab's `parallel:matrix`. Each entry expands to the
cartesian product of its variables, and `${VAR}` is replaced in every string
field:

```yaml
  - id: ${DISTRO}-${VERSION}-amd64
    container: ${DISTRO}:${VERSION}
    formats: [deb]
    matrix:
      - DISTRO: [debian]
        VERSION: ["12", "13"]
      - DISTRO: [ubuntu]
        VERSION: ["24.04"]
```

This produces `debian-12-amd64`, `debian-13-amd64` and `ubuntu-24.04-amd64`.
To add a distro, add a value. `go run ./tools/dwb targets` prints the
expanded list.

## Formats

| Format | Built on | Notes |
| --- | --- | --- |
| `msi` | Windows runner, WiX 5 | Per-user install to `%LOCALAPPDATA%\Programs\DesktopWidgets\dw-<id>`, Start-menu entry, no admin rights. Stable upgrade code per widget. |
| `zip` | any | Portable `dw-<id>.exe`. |
| `tar.gz` | Ubuntu 22.04 (older glibc) | Binary, icon, `.desktop` and `install.sh` (installs to `~/.local`). Needs GTK 3 + WebKitGTK 4.1 on the system. |
| `deb` | distro container | Package `dw-<id>`, installs `/usr/bin/dw-<id>`. Built per distro release. |
| `rpm` | distro container | Same layout as deb. |
| `flatpak` | Ubuntu runner + flatpak-builder | Compiled inside the GNOME SDK, bundle depends on the Flathub GNOME runtime. X11 socket only. |

Asset names: `dw-<id>_<version>_<target>.<format>`, for example
`dw-clock_2.0.0_fedora-44-amd64.rpm`.

## Platform notes

- **Desktop layer.** `layer: bottom` keeps the window below normal windows,
  hidden from the taskbar and on all workspaces.
  - X11: window manager hints (keep-below, skip-taskbar/pager, sticky).
  - Windows: `HWND_BOTTOM`, re-applied when the widget gets focus.
  - Known limitation: Windows "Show desktop" (Win+D) also hides widgets.
- **Wayland.** Widgets force `GDK_BACKEND=x11`, so on Wayland sessions they
  run through XWayland. Native Wayland (layer-shell) can be added in
  `runtime/` later without touching widgets.
- **Transparency** needs a compositing window manager on Linux. All mainstream
  desktops have one.
- **Toolkit.** Linux builds use GTK 3 + WebKitGTK 4.1 (the `gtk3` build tag).
  Wails' default GTK 4 backend cannot do keep-below or window positioning on
  X11.
- **Native rendering later.** The runtime only exposes data (providers) and
  events to the view, so the web view could be replaced with native
  rendering without changing providers or manifests.
