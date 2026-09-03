# 09 — CI/CD and Releases

This document defines how **git-in-track** is built, verified and released. It explains the
committed GitHub Actions workflows and the GoReleaser configuration, and adds the
versioning policy, branch strategy, release checklist and the distribution channels planned
for later phases.

> Status: implemented. `.github/workflows/ci.yml`, `.github/workflows/release.yml`,
> `.goreleaser.yaml`, `.golangci.yaml` and the `Makefile` are committed and are the source
> of truth. This document explains them; when the two disagree, the file wins and this
> document is the bug.

- Module path: `github.com/digiogithub/git-in-track`
- CLI binary: `gintrack`
- Toolchain: Go 1.24 in CI (`go.mod` declares `go 1.23.0`, so 1.23 still builds), Node 22, npm
- Build artifacts: `web/dist` (Vite bundle), `web/public/core.wasm` + `web/public/wasm_exec.js`
  (Go → WASM core, both git-ignored), `gintrack` (single static binary embedding `web/dist`
  via `go:embed`)

---

## 1. Build pipeline overview

The build has a strict order, because the Go binary embeds the frontend and the frontend
ships the WASM core:

```
1. wasm    GOOS=js GOARCH=wasm go build -o web/public/core.wasm ./wasm
2. web     npm ci && npm run build            -> web/dist
3. build   go build ./cmd/gintrack             -> gintrack (embeds web/dist)
```

Any pipeline that produces a runnable binary must run these three steps in this order.
CI additionally runs the verification steps (vet, lint, typecheck, unit tests) which are
order-independent and are parallelised across jobs.

```mermaid
graph LR
  A[checkout] --> B[go vet + go test -race]
  A --> C[golangci-lint]
  A --> D[npm ci -> lint + typecheck + vitest]
  D --> E[vite build]
  A --> F[wasm build]
  F --> E
  E --> G[go build, embeds web/dist]
  B --> G
  C --> G
  G --> H{tag v*?}
  H -- yes --> I[GoReleaser -> GitHub Release]
  H -- no --> J[artifact upload only]
```

---

## 2. `.github/workflows/ci.yml`

**Source of truth: [`.github/workflows/ci.yml`](../.github/workflows/ci.yml).** The file is
the specification; this section explains it and is kept in step with it. `make lint-ci`
parses both workflows as YAML and runs `actionlint` when it is installed.

Runs on every push to `main`, on every pull request, and on demand. Fails fast on
formatting and static analysis, then runs the full test suite and a real build.

| Job         | Name in the checks list             | What it does                                                                                     |
| ----------- | ----------------------------------- | ------------------------------------------------------------------------------------------------ |
| `preflight` | Preflight (repository layout)       | Asserts the scaffold every other job needs is present, and fails if a change removed it            |
| `workflows` | Workflows (YAML + actionlint)       | Parses `.github/workflows/*.yml` with PyYAML, then runs `actionlint`                               |
| `go`        | Go (vet, test, lint)                | `go mod download`, tidy check, `gofmt` check, `go vet`, `go test -race` + coverage, golangci-lint  |
| `web`       | Web (lint, typecheck, test, build)  | `npm ci`, ESLint, `tsc -b`, vitest, `vite build`, uploads `web/dist`                               |
| `wasm`      | WASM core                           | `make wasm`, size report, `node scripts/wasm-smoke.mjs`, uploads `core.wasm` + `wasm_exec.js`      |
| `build`     | Full build (embed frontend)         | WASM → web → `go build`, runs `gintrack version` and `--help`, uploads the binary                  |

Everything except `build` fans out from `preflight`; `build` waits for all of them, so a
failed lint or test never produces a downloadable artifact.

### Preflight

The Phase 0 scaffold has landed, so the guard no longer skips the pipeline when `go.mod`
and `web/package.json` are missing. It now asserts the layout — `go.mod`, `go.sum`,
`Makefile`, `.goreleaser.yaml`, `.golangci.yaml`, `cmd/gintrack`, `wasm`, the web
workspace, `web/dist/.gitkeep` and `scripts/wasm-smoke.mjs` — and **fails** when something
is gone, which is a real regression rather than a reason to stay green. Downstream jobs
therefore no longer carry an `if:` condition, and the required status checks of §8 always
report.

### The two traps this repository sets

```yaml
# 1. .golangci.yaml is a v2 config, so the action must be v7+ with a v2.x linter.
- uses: golangci/golangci-lint-action@v8
  with:
    version: ${{ env.GOLANGCI_LINT_VERSION }}   # v2.x
    args: --timeout=5m

# 2. npm installs third-party Go sources under web/node_modules; never build them.
- run: go list ./... | grep -v '/web/node_modules/' | xargs go vet
```

### Notes on the CI workflow

- **Caching**: `actions/setup-go@v5` with `cache: true` caches both the module cache and
  the build cache, keyed on `go.sum`. `actions/setup-node@v4` with `cache: npm` caches the
  npm cache directory keyed on `web/package-lock.json`. `golangci/golangci-lint-action`
  keeps its own analysis cache. No manual `actions/cache` step is needed.
- **`concurrency`** cancels superseded runs on the same branch/PR, which keeps queue times
  low on a small open-source project.
- **`permissions: contents: read`** — CI never needs write access.
- **`go mod tidy` check** prevents drift between imports and `go.mod`.
- **golangci-lint v2.** `.golangci.yaml` is a `version: "2"` configuration, so the action
  must be `golangci/golangci-lint-action@v7` or later with a `v2.x` linter version;
  `@v6` only knows how to install golangci-lint v1 and fails to parse the config. The
  version is pinned in the `GOLANGCI_LINT_VERSION` environment variable to the release
  verified locally by `make lint`, and the action's default `verify: true` additionally
  validates the config against its JSON schema.
- **Never lint `web/node_modules`.** npm ships Go sources there; every Go step filters
  `go list ./...` through `grep -v '/web/node_modules/'`, matching `GO_PKGS` in the
  Makefile.
- **`gofmt` scope.** The check skips `web/node_modules/` only, so `web/embed.go` is
  formatted like every other Go file in the module.
- **No `--coverage` for vitest.** The web workspace does not install a coverage provider,
  and `vitest --coverage` would try to install one non-interactively in CI.
- The `build` job depends on every verification job so that a broken lint/test never
  produces a downloadable artifact.
- Later phases add an `e2e` job running Playwright against `gintrack serve` (Phase 2+) and
  a `matrix` of `ubuntu-latest`, `macos-latest`, `windows-latest` for the Go job once file
  watching is implemented (Phase 2), because `fsnotify` behaviour is platform specific.

---

## 3. `.github/workflows/release.yml`

**Source of truth: [`.github/workflows/release.yml`](../.github/workflows/release.yml).**

Triggered by pushing a tag matching `v*`. Builds the WASM core and the frontend first,
smoke-tests the WASM module, validates the GoReleaser configuration, then hands over to
GoReleaser, which cross-compiles, archives, checksums, generates the changelog and
publishes the GitHub Release.

| Job         | Steps                                                                                 |
| ----------- | ------------------------------------------------------------------------------------- |
| `preflight` | asserts the repository layout GoReleaser needs                                          |
| `release`   | checkout with `fetch-depth: 0` → setup Go/Node → `make wasm` → `node scripts/wasm-smoke.mjs` → `npm ci && npm run build` → verify embedded assets → `goreleaser check` → `goreleaser release --clean` |

Permissions follow least privilege: the workflow is `contents: read` at the top level and
only the `release` job raises itself to `contents: write`, which is what GoReleaser needs
to create the GitHub Release. `fetch-depth: 0` is required for the generated changelog.

### Snapshot build (local, or optional for `main`)

A snapshot build catches cross-compilation breakage before a tag is cut. Locally that is
`make release-snapshot`; as a nightly or manual workflow it is the same job with
`args: release --snapshot --clean --skip=publish` and no `contents: write` permission.
---

## 4. Artifacts are unsigned

**git-in-track releases are not code-signed and not notarized.** There is no Apple
Developer ID certificate, no Windows Authenticode certificate and no notarization step in
the release pipeline. This is a deliberate choice for the initial releases: certificates
cost money, require a legal entity and secrets management, and would gate the project on
non-technical work.

The consequence is that operating systems will warn users on first launch. Integrity is
instead verified through `checksums.txt`, which is published with every release, and
through the fact that every artifact is built by a public GitHub Actions run from a public
tag.

### macOS (Gatekeeper)

Downloaded archives carry the `com.apple.quarantine` attribute, so the first run reports
that the binary "cannot be opened because the developer cannot be verified".

```bash
# Option A — remove the quarantine attribute after extracting
xattr -d com.apple.quarantine ./gintrack

# Option B — approve once via System Settings
#   Run ./gintrack, let it be blocked, then open
#   System Settings > Privacy & Security > "Open Anyway"

# Option C — install via Homebrew tap (Phase 6), which strips quarantine for you
```

Verify the download first:

```bash
shasum -a 256 -c checksums.txt --ignore-missing
```

### Windows (SmartScreen)

Windows Defender SmartScreen shows a "Windows protected your PC" dialog for unrecognised
publishers. Users click **More info** → **Run anyway**. Alternatively, unblock the archive
before extracting:

```powershell
Unblock-File -Path .\gintrack_1.0.0_windows_amd64.zip
Get-FileHash .\gintrack.exe -Algorithm SHA256
```

### Linux

No signature checks apply. Users should still verify the checksum and mark the binary
executable:

```bash
sha256sum -c checksums.txt --ignore-missing
chmod +x gintrack
```

The release notes template repeats these instructions for every release so users never
have to search for them.

---

## 5. `.goreleaser.yaml`

**Source of truth: [`.goreleaser.yaml`](../.goreleaser.yaml)**, validated by
`make release-check` (`goreleaser check`) and by the release workflow before it publishes.

GoReleaser v2 configuration. CGO is disabled everywhere so the binaries are fully static
and cross-compilation needs no C toolchain. `before.hooks` rebuild the WASM core and the
web bundle, in that order, so that a local `make release-snapshot` reproduces CI exactly:

```yaml
before:
  hooks:
    - go mod tidy
    - go mod download
    - sh -c "GOOS=js GOARCH=wasm go build -trimpath -o web/public/core.wasm ./wasm"
    - sh -c "cd web && npm ci && npm run build"
```

The `ldflags` set the four variables declared in `cmd/gintrack/main.go` — `main.version`,
`main.commit`, `main.date` and `main.builtBy` — which is what `gintrack version` prints:

```yaml
    ldflags:
      - -s -w
      - -X main.version={{ .Version }}
      - -X main.commit={{ .FullCommit }}
      - -X main.date={{ .CommitDate }}
      - -X main.builtBy=goreleaser
```

The rest of the file configures the 3×2 `goos`/`goarch` matrix, `tar.gz` archives with a
`zip` override for Windows, `checksums.txt` (SHA-256), the grouped Conventional-Commits
changelog, and the release notes template that repeats the Gatekeeper and SmartScreen
instructions of §4 on every release.

There is no `-tags=embed` build tag: `web/embed.go` embeds `web/dist` unconditionally and
reports `Built() == false` when the directory holds nothing but its `.gitkeep`, which is
what lets `go install` produce a working CLI without a frontend build.

### Artifact matrix produced

| OS      | Arch  | Archive                                    |
| ------- | ----- | ------------------------------------------ |
| linux   | amd64 | `gintrack_1.0.0_linux_amd64.tar.gz`        |
| linux   | arm64 | `gintrack_1.0.0_linux_arm64.tar.gz`        |
| darwin  | amd64 | `gintrack_1.0.0_darwin_amd64.tar.gz`       |
| darwin  | arm64 | `gintrack_1.0.0_darwin_arm64.tar.gz`       |
| windows | amd64 | `gintrack_1.0.0_windows_amd64.zip`         |
| windows | arm64 | `gintrack_1.0.0_windows_arm64.zip`         |
| —       | —     | `checksums.txt`                            |

---

## 6. `Makefile`

The Makefile is the single local entry point; CI calls the same targets (`make wasm`,
`node scripts/wasm-smoke.mjs`) or the identical commands, so "works on my machine" and
"works in CI" cannot diverge.

**Source of truth: [`Makefile`](../Makefile).** Targets:

| Target             | What it does                                                             |
| ------------------ | ------------------------------------------------------------------------ |
| `deps`             | `go mod download` and `npm ci` in `web/`                                  |
| `wasm`             | `GOOS=js GOARCH=wasm go build -o web/public/core.wasm ./wasm`, then copies the matching `wasm_exec.js` |
| `wasm-smoke`       | depends on `wasm`; runs `scripts/wasm-smoke.mjs` (§6.1)                   |
| `web`              | depends on `wasm`; `npm ci && npm run build` → `web/dist`                 |
| `build`            | `go build` → `bin/gintrack`, embedding `web/dist`                         |
| `test`             | `test-go` (race + coverage) and `test-web` (vitest)                       |
| `lint`             | `lint-go`, `lint-web` and `lint-ci`                                       |
| `lint-ci`          | parses both workflow files as YAML and runs `actionlint` when installed   |
| `fmt`              | `gofmt -w` over the tracked Go sources                                    |
| `run` / `dev`      | companion server / Vite dev server                                        |
| `release-check`    | `goreleaser check` on `.goreleaser.yaml`, no build                        |
| `release-snapshot` | `goreleaser release --snapshot --clean --skip=publish`                    |
| `clean`            | removes build outputs, keeping `web/dist/.gitkeep`                        |

Two details matter and are easy to get wrong:

- **`GO_PKGS`.** npm installs third-party *Go* sources under `web/node_modules` (for
  example `flatted/golang`), which `go list ./...` happily returns. Every Go target
  filters them out:

  ```makefile
  GO_PKGS = $(shell go list ./... | grep -v '/web/node_modules/')
  ```

  CI does the same with `go list ./... | grep -v '/web/node_modules/' | xargs go vet`.
  `golangci-lint` excludes the same path through `linters.exclusions.paths`.

- **`clean` keeps `web/dist/.gitkeep`.** `web/embed.go` declares `//go:embed all:dist`, so
  deleting the directory breaks `go build` until the frontend is rebuilt.

### 6.1 WASM smoke test

`scripts/wasm-smoke.mjs` (Node 22, ESM) proves the browser bundle can actually call into
the Go core, without a browser and without Playwright. It does exactly what
`web/src/core-bridge/worker.ts` does at runtime:

1. evaluates `web/public/wasm_exec.js` in the current context, which defines `globalThis.Go`;
2. instantiates `web/public/core.wasm` against `go.importObject`;
3. calls `go.run(instance)` — `wasm/main_js.go` ends in `select {}`, so the promise never
   resolves and the module stays resident; an early exit or a trap fails the test;
4. asserts `gintrackCore.version()` returns `{"ok":true,"data":{version,commit,date,schema}}`;
5. asserts `gintrackCore.parseItem(path, text)` returns an envelope whose `data` carries
   `id`, `type`, `title` and a computed `rev` — `rev` is never stored in a file, so its
   presence is the proof that real core code ran;
6. asserts a malformed item comes back as `{"ok":false,"error":{"code":...}}`.

Node 20+ already provides every global `wasm_exec.js` expects (`crypto`, `TextEncoder`,
`TextDecoder`, `performance`); the script installs them from `node:` builtins only when
they are missing, so it also runs on older hosts.

Run it with `make wasm-smoke` (which builds the artifacts first) or `npm run wasm:smoke`
from `web/`. The `wasm` job in CI and the `release` job both run it.

---

## 7. Versioning policy

git-in-track follows [Semantic Versioning 2.0.0](https://semver.org/).

- **Tags** are `vX.Y.Z` (leading `v`, no other prefix). GoReleaser derives the version by
  stripping the `v`.
- **MAJOR** — breaking change to any public contract: the on-disk data model in `.pmngr/`,
  the REST/WebSocket API, the MCP tool schemas, or the CLI flags and command names.
- **MINOR** — backwards-compatible features: new front-matter fields with defaults, new
  API endpoints, new MCP tools, new UI surfaces.
- **PATCH** — backwards-compatible fixes and internal changes.
- **Pre-releases** are `vX.Y.Z-rc.N` (for example `v1.0.0-rc.1`). GoReleaser's
  `prerelease: auto` marks any tag containing a hyphen as a GitHub pre-release, so no
  configuration change is needed per release.
- **Before 1.0.0** (Phases 0–5) the project is `v0.Y.Z`. The `0.` major means the data
  model may still change; every breaking change to `.pmngr/` bumps the MINOR and is
  documented with a migration note in `CHANGELOG.md`.
- **Data model version**: `project.yaml` and `team.yaml` carry a `schemaVersion` field so
  the tool can detect and migrate older vaults independently of the binary version.
- The binary reports its version through `gintrack version`, populated by the ldflags
  `main.version`, `main.commit`, `main.date`.

---

## 8. Branch strategy

**Trunk-based development.** There is exactly one long-lived branch.

- `main` is always releasable. Every commit on `main` passes CI.
- Work happens on short-lived branches named `<type>/<scope>-<short-slug>`, for example
  `feat/core-frontmatter-parser`, `fix/web-board-drag`, `docs/ci-release-plan`. Branches
  live hours to a few days, never weeks.
- Changes reach `main` only through pull requests, **squash-merged**, so `main` has one
  commit per PR and the PR title becomes the commit subject. That title must be a valid
  Conventional Commit — it is what the changelog is generated from.
- No release branches while the project is small. If a patch is ever needed for an old
  minor, a `release/vX.Y` branch is cut from the tag on demand and deleted after the patch
  release.
- Tags are created **on `main` only**, by a maintainer, after CI is green.

### Protection rules for `main`

- Require a pull request before merging, with at least 1 approving review.
- Require status checks to pass: `Preflight (repository layout)`,
  `Workflows (YAML + actionlint)`, `Go (vet, test, lint)`, `Web (lint, typecheck, test,
  build)`, `WASM core`, `Full build (embed frontend)`.
- Require branches to be up to date before merging.
- Require conversation resolution before merging.
- Require linear history (squash merges only).
- Block force pushes and deletions.
- Include administrators (maintainers follow the same rules).

### Conventional commits

Commit subjects and PR titles follow
[Conventional Commits 1.0.0](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>
```

Types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `style`,
`revert`. Scopes: `core`, `cli`, `server`, `web`, `wasm`, `mcp`, `docs`, `ci`. A breaking
change is marked with `!` after the scope (`feat(core)!: rename story parent field`) and a
`BREAKING CHANGE:` footer. The full convention is specified in
[10-development-guidelines.md](./10-development-guidelines.md).

A `commitlint`-style check runs in CI on PR titles (Phase 0 deliverable) so that a
malformed title cannot reach `main` and corrupt the generated changelog.

---

## 9. Release checklist

Run through this list for every tagged release. Steps marked *(automated)* are performed
by the pipeline; the rest are the maintainer's responsibility.

**Before tagging**

- [ ] `main` is green in CI and there are no open blocking issues for the milestone.
- [ ] `make lint test` passes locally on macOS **and** Linux (Windows if the release
      touches the watcher or path handling).
- [ ] `make wasm-smoke` passes: `core.wasm` instantiates and answers outside a browser.
- [ ] `make release-check` validates `.goreleaser.yaml`.
- [ ] `make release-snapshot` succeeds and produces all 6 archives plus `checksums.txt`.
- [ ] The snapshot binary runs: `./dist/gintrack_linux_amd64_v1/gintrack version`,
      `gintrack serve` starts and the embedded web app loads at `http://127.0.0.1:7317`.
- [ ] Data model changes, if any, are accompanied by a migration and a `schemaVersion`
      bump.
- [ ] `CHANGELOG.md` "Unreleased" section is reviewed; anything the generated changelog
      cannot express (migrations, deprecations, known issues) is written by hand.
- [ ] Documentation under `docs/` reflects the release; screenshots regenerated if the UI
      changed.
- [ ] `README.md` installation section mentions the new version where it is pinned.
- [ ] The version's milestone in `docs/.pmngr/milestones/` is closed and its stories are
      `done`.

**Tagging**

- [ ] Decide the version according to §7.
- [ ] `git switch main && git pull --ff-only`
- [ ] `git tag -a vX.Y.Z -m "vX.Y.Z"` (annotated tags only)
- [ ] `git push origin vX.Y.Z`

**After tagging**

- [ ] *(automated)* Release workflow builds WASM + web + 6 binaries.
- [ ] *(automated)* GoReleaser publishes the GitHub Release with archives, `checksums.txt`
      and the generated changelog.
- [ ] Download one archive per OS family and verify its checksum.
- [ ] Smoke test the macOS artifact through the documented Gatekeeper bypass and the
      Windows artifact through the SmartScreen bypass.
- [ ] Announce: GitHub Release notes, repository Discussions, project README badge.
- [ ] Open a follow-up issue for anything deferred during the release.

**If a release is broken**

- Do not delete or move a published tag. Cut `vX.Y.Z+1` with the fix and mark the broken
  release as deprecated in its release notes.

---

## 10. Distribution channels (later phases)

Only the GitHub Release exists for Phases 0–5. The following channels are added as part of
Phase 6 (polish and 1.0), each as its own user story.

### `go install` — available from day one

Works without any extra infrastructure, but produces a binary **without** the embedded web
app unless the frontend was built first, so it is documented as the "developer install":

```bash
go install github.com/digiogithub/git-in-track/cmd/gintrack@latest
```

`web/embed.go` embeds `web/dist` unconditionally, and a checkout that was never built ships
only the directory's `.gitkeep`. `web.Built()` then reports `false`: the CLI still runs
`gintrack mcp` and the file-based commands, and `gintrack serve` reports that no embedded
UI is available and points at the released binaries.

### Homebrew tap

A separate repository `digiogithub/homebrew-tap` holds the formula. GoReleaser publishes
it automatically on each release by adding to `.goreleaser.yaml`:

```yaml
brews:
  - name: gintrack
    repository:
      owner: digiogithub
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    directory: Formula
    homepage: https://github.com/digiogithub/git-in-track
    description: Git-native, markdown-first project management for teams
    license: MIT
    test: |
      system "#{bin}/gintrack", "version"
```

Install: `brew install digiogithub/tap/gintrack`. Homebrew removes the quarantine
attribute, so this is the recommended macOS path.

### Scoop bucket

A `digiogithub/scoop-bucket` repository, also published by GoReleaser:

```yaml
scoops:
  - name: gintrack
    repository:
      owner: digiogithub
      name: scoop-bucket
      token: "{{ .Env.SCOOP_BUCKET_TOKEN }}"
    homepage: https://github.com/digiogithub/git-in-track
    description: Git-native, markdown-first project management for teams
    license: MIT
```

Install: `scoop bucket add digiogithub https://github.com/digiogithub/scoop-bucket` then
`scoop install gintrack`.

### Docker image

Published to GitHub Container Registry as `ghcr.io/digiogithub/git-in-track`. Useful for
running the companion server against a repository mounted into the container, and for CI
of downstream projects. Added to `.goreleaser.yaml`:

```yaml
dockers:
  - image_templates:
      - "ghcr.io/digiogithub/git-in-track:{{ .Version }}-amd64"
    dockerfile: Dockerfile
    use: buildx
    goarch: amd64
    build_flag_templates:
      - "--platform=linux/amd64"
      - "--label=org.opencontainers.image.source=https://github.com/digiogithub/git-in-track"
  - image_templates:
      - "ghcr.io/digiogithub/git-in-track:{{ .Version }}-arm64"
    dockerfile: Dockerfile
    use: buildx
    goarch: arm64
    build_flag_templates:
      - "--platform=linux/arm64"

docker_manifests:
  - name_template: "ghcr.io/digiogithub/git-in-track:{{ .Version }}"
    image_templates:
      - "ghcr.io/digiogithub/git-in-track:{{ .Version }}-amd64"
      - "ghcr.io/digiogithub/git-in-track:{{ .Version }}-arm64"
  - name_template: "ghcr.io/digiogithub/git-in-track:latest"
    image_templates:
      - "ghcr.io/digiogithub/git-in-track:{{ .Version }}-amd64"
      - "ghcr.io/digiogithub/git-in-track:{{ .Version }}-arm64"
```

The release workflow then also needs `packages: write` permission and a
`docker/login-action` step against `ghcr.io`.

Usage:

```bash
docker run --rm -p 7317:7317 \
  -v "$PWD:/work" \
  ghcr.io/digiogithub/git-in-track:latest \
  serve --addr 0.0.0.0:7317 --root /work
```

Note that binding to `0.0.0.0` inside a container is acceptable because the port mapping
controls exposure; the native binary defaults to `127.0.0.1` and refuses non-loopback
addresses unless `--allow-remote` is passed. See
[10-development-guidelines.md](./10-development-guidelines.md) §8.

### Deliberately out of scope

Linux distribution packages (`.deb`, `.rpm`, AUR, nixpkgs), winget, and Snap/Flatpak are
not planned before 1.0. They add maintenance load without reaching an audience the GitHub
Release does not already serve. GoReleaser can produce `.deb`/`.rpm` with `nfpms:` if
demand appears.
