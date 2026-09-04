# ADR-016 — Distribute Homebrew as a cask, not as a formula

- **Status:** Accepted
- **Date:** 2026-09-04
- **Phase:** 6 (polish and 1.0 releases)
- **Related:** [ADR-011](ADR-011-goreleaser-unsigned-artifacts.md)

## Context

[ADR-011](ADR-011-goreleaser-unsigned-artifacts.md) ships unsigned archives and
names Homebrew as the recommended macOS route precisely because installing
through Homebrew clears the `com.apple.quarantine` attribute that Gatekeeper
reacts to. [docs/09](../09-ci-cd-and-releases.md) §10 planned this as a
GoReleaser `brews:` block publishing a **formula** to `digiogithub/homebrew-tap`,
on the assumption that a formula covers macOS and Linux from one file, and
[GIT-US-0029](../.pmngr/stories/GIT-US-0029-publish-homebrew-scoop-and-docker-distributions.md)
carries that assumption into its acceptance criteria.

That assumption no longer holds on the tooling side. GoReleaser deprecated
`brews:` in v2.10 and hardened the deprecation in v2.16: since then
`goreleaser check` **exits non-zero** on a configuration that still uses it, and
the release workflow runs `goreleaser check` before it publishes anything. The
replacement is `homebrew_casks:`. Upstream's reasoning is Homebrew's own: a
formula that installs a pre-compiled binary was a Linuxbrew-era workaround, and
pre-built binaries belong in casks.

The cost of the replacement is that **Homebrew casks are macOS-only**. GoReleaser
does emit `on_linux` URL stanzas into the generated cask, which is misleading:
Homebrew on Linux refuses to install a cask whatever the file says.

Keeping `brews:` would mean pinning GoReleaser to a 2025 release across the
action, the `check` step and `make release-check`, and carrying a linter the
project knows is failing. That trades a permanent maintenance debt for one
platform's convenience path.

## Decision

**Publish the Homebrew channel as a cask (`homebrew_casks:`) to
`digiogithub/homebrew-tap`, serving macOS only, and cover Linux through the
other channels.**

- `brew install digiogithub/tap/gintrack` remains the recommended and documented
  macOS install, unchanged from the user's point of view.
- The cask carries a `postflight` hook running
  `xattr -dr com.apple.quarantine` over the staged binary, so the Gatekeeper
  benefit ADR-011 depends on is explicit in the cask rather than incidental.
- **Linux install paths are the release tarball, `go install`, and the
  `ghcr.io/digiogithub/git-in-track` image.** All three are documented in
  `README.md` and in [docs/09](../09-ci-cd-and-releases.md) §10. Linuxbrew is not
  a supported channel.
- `skip_upload: auto` keeps pre-release tags out of the tap.

## Consequences

**Positive**

- `goreleaser check` passes on current GoReleaser, so the release workflow keeps
  its pre-publish validation and the tooling stays on `~> v2`.
- The quarantine-clearing step is declared in the cask instead of being a side
  effect users have to trust.
- One less template to maintain: the cask needs no `install`/`test` Ruby block.

**Negative**

- **`brew install` does not work on Linux.** This is a real regression against
  the plan written in docs/09 §10 and against the wording of GIT-US-0029's first
  acceptance criterion, which is why it is recorded here rather than quietly
  implemented. Linux users get the tarball, `go install` or the container.
- A cask is not a formula: it cannot be a dependency of another formula, and
  `brew` will not build it from source on an unsupported architecture.
- If Linuxbrew support is ever wanted, it has to come back as a hand-maintained
  formula in the same tap, outside GoReleaser.

## Alternatives considered

- **Keep `brews:` and pin GoReleaser to a version predating the deprecation.**
  Rejected: it freezes the release tooling, and `brews:` is removed in
  GoReleaser v3 anyway, so this only defers the same decision.
- **Keep `brews:` and stop running `goreleaser check` in the release workflow.**
  Rejected outright: the check is the only thing standing between a malformed
  configuration and a half-published tag.
- **Publish a cask *and* hand-maintain a formula in the same tap.** Two files
  claiming the same name in one tap is ambiguous for `brew install`, and the
  formula would drift the first time nobody remembers to bump it.
- **Ship a `.deb`/`.rpm` for Linux instead.** Out of scope before 1.0 per
  docs/09 §10; the tarball and the image already serve that audience.
