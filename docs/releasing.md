# Releasing

## One-time setup

The release workflow in this repository pushes to the release repository
`cdro/DesktopWidgets`. It needs a token stored as the `RELEASE_TOKEN` secret:

1. Create a fine-grained personal access token. Give it access to **only**
   `cdro/DesktopWidgets`, with the repository permission **Contents: Read and
   write**.
2. In this repository, go to Settings → Secrets and variables → Actions and add
   it as `RELEASE_TOKEN`.

(A GitHub App token works as well: generate it in the publish job with
`actions/create-github-app-token` and pass it in place of the secret.)

## Cutting a release

Bump the project version in `VERSION` (e.g. `1.2.0`) and merge that to
`main`. The release workflow creates the tag `v1.2.0` in this repository and
runs the release in the same run. A push to `main` that doesn't change
`VERSION` releases nothing. Pushing a `v*` tag by hand works too.

The tag versions the project. It is mirrored as the same tag in the release
repository. Tags with a suffix, such as `v1.3.0-rc1`, are published as
pre-releases and are not marked "latest", so widgets don't pick them up as
updates.

What gets built:

- Every widget/app whose `widget.yml` version differs from the latest
  published `catalog.json`. A version lower than the published one fails the
  release.
- For each of those, every target in `platforms.yml` that the widget's
  `platforms` filter allows.
- If nothing changed, the release still happens: the tag is mirrored and the
  catalog is re-published.

To re-publish the current `VERSION`, run the *Release* workflow manually on
`main`. To rebuild every widget, for example after a runtime security fix,
also check **Rebuild every widget**. Users only get the fix as an update if the versions were bumped, so prefer
bumping the affected widgets (with `release.critical: true` for security
fixes).

## What the release repository contains

- `README.md`: the widget table with download links, regenerated on every
  release.
- `catalog.json`: the cumulative index that widgets and the library use for
  update checks. Each entry points at the release that published its current
  version.
- GitHub releases with the packages, `catalog.json` and `SHA256SUMS`.

## Update behaviour for users

Widgets check `releases/latest/download/catalog.json` 20 seconds after start
and then every 6 hours.

- **Major update**, or a release marked `critical`: a badge on the widget plus
  a menu entry.
- **Minor/patch update**: a menu entry only.

Choosing the update opens the library app if it is installed, otherwise the
release page.
