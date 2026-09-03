# ADR-011 — GoReleaser with unsigned release artifacts in v1

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 6 (polish and 1.0 releases)
- **Related:** [ADR-005](ADR-005-companion-cli-go-embed.md)

## Context

`gintrack` is a single static binary that embeds the web app
([ADR-005](ADR-005-companion-cli-go-embed.md)) and must be published for
linux/darwin/windows on amd64 and arm64 — six artifacts per release, built
reproducibly from a tag, with checksums, changelog and release notes.

The complication is code signing. Users on macOS and Windows meet OS gatekeepers:

- **macOS:** an unsigned, un-notarised binary downloaded from the internet is
  quarantined; the user must clear the quarantine attribute or approve it in System
  Settings. Signing and notarising requires an Apple Developer Program membership
  (an annual fee), a Developer ID certificate, and a notarisation step with Apple
  credentials held in CI.
- **Windows:** SmartScreen warns on binaries without an established reputation.
  Authenticode signing requires a certificate from a commercial CA, and since 2023
  OV/EV certificates must be stored on hardware or in an attested cloud HSM — which
  is awkward and costly for a project with no legal entity or budget line.

For a new open-source project with no funding, both are real money and real
operational complexity (secrets in CI, key custody, renewal), for a benefit that is
partial anyway: signing removes a warning, it does not prove much about the build.

## Decision

**Use GoReleaser in a GitHub Actions workflow triggered on tags matching `v*`, and
ship unsigned artifacts in v1.**

- Matrix: `linux`, `darwin`, `windows` × `amd64`, `arm64`.
- Output: `.tar.gz` archives (`.zip` for Windows) containing the binary, `LICENSE`
  and `README.md`, plus a `checksums.txt` covering every archive, attached to the
  GitHub Release.
- Version, commit and build date are injected via `-ldflags` and reported by
  `gintrack version`.
- Builds are reproducible: `CGO_ENABLED=0`, trimmed paths, a pinned Go toolchain
  version, and a pinned GoReleaser version.
- Release notes are generated from Conventional Commits and edited before publishing.
- **Provenance instead of signatures**, because it is free: GitHub Actions
  attestations (SLSA build provenance) for the artifacts, and a documented
  verification procedure using the published checksums.
- **The gatekeeper story is documented, not hidden.** The install page gives the
  exact macOS quarantine-clearing command and explains the Windows SmartScreen
  prompt, in plain language, next to the checksums.
- Package-manager distribution (Homebrew tap, Scoop bucket, `go install`) is offered
  in parallel, since those paths avoid the download-quarantine problem entirely and
  are what most developers will actually use.
- **This decision is explicitly revisited before 1.0 GA.** If the project acquires a
  sponsoring entity, signing and notarisation are the first thing to buy.

## Consequences

**Positive**

- Releases are fully automated from `git tag` with no secrets beyond the default
  `GITHUB_TOKEN`, and any contributor can reproduce a build locally.
- No annual fees, no certificate custody, no renewal deadline, no signing key in CI
  to be stolen.
- Checksums plus build provenance give a verifiable chain from source to artifact —
  arguably more meaningful than a signature that only asserts identity.
- Homebrew and Scoop cover the friction for most users on macOS and Windows.

**Negative**

- **A visible warning on macOS and Windows for direct downloads.** Some users will
  bounce off it, and some corporate environments will block the binary outright. This
  is the real cost of the decision and it must be documented prominently rather than
  discovered.
- **Perceived lower trustworthiness** for a security-adjacent tool that handles
  repositories and git credentials.
- **Homebrew and Scoop are extra maintenance** (a tap and a bucket to keep current).
- **Adding signing later is not free**: it changes the release workflow, introduces
  secrets, and the first signed Windows build still has no SmartScreen reputation.
- **No Linux package repositories in v1** (`.deb`/`.rpm` via GoReleaser is possible
  but adds hosting and signing of its own). Archives only.

## Alternatives considered

- **Sign and notarise from day one.** The best user experience and the correct
  end state; rejected for v1 purely on cost and custody, with an explicit
  commitment to revisit before GA.
- **Sigstore/cosign keyless signing of the archives.** Cheap, keyless and verifiable,
  and it does *not* satisfy macOS Gatekeeper or Windows SmartScreen, which is the
  actual user-facing problem. We ship GitHub attestations for the same benefit and do
  not claim it solves gatekeeping.
- **Distribute only via package managers and `go install`.** Sidesteps gatekeepers
  entirely, and excludes users who want a direct download and those without a Go
  toolchain. Rejected as the only channel; adopted as a parallel one.
- **Ship containers only.** Wrong shape for a tool whose entire purpose is to read
  local folders and serve a local UI. Rejected.
- **Hand-rolled release scripts instead of GoReleaser.** More control, more
  maintenance, no benefit at this scale. Rejected.
