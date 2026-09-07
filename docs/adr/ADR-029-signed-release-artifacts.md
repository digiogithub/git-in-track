# ADR-029 — Release artifacts are signed on macOS and Windows, on their own runners

- **Status:** Accepted
- **Date:** 2026-09-07
- **Phase:** 6 (polish and 1.0 releases)
- **Supersedes:** [ADR-011](ADR-011-goreleaser-unsigned-artifacts.md)
- **Related:** [ADR-016](ADR-016-homebrew-cask-instead-of-formula.md)

## Context

[ADR-011](ADR-011-goreleaser-unsigned-artifacts.md) shipped unsigned artifacts because
certificates cost money and need a legal entity, and it committed to revisiting the
decision before 1.0 GA. That premise no longer holds: DIGIO already owns the signing
material and pays for it for another product, Pando —

- an Apple Developer ID Application certificate plus notary credentials, held on
  `mac-mini-de-digio` and shipped to CI as one base64 bundle;
- an Azure Trusted Signing account (`digio-art-sign-acc`, certificate profile `digio`,
  `CN=Digio Soluciones Digitales SL`) that signs Windows binaries with no long-lived
  secret, through an OIDC federated credential;
- a set of reusable composite actions, `digiogithub/ci-actions@v1`, that already
  implement the keychain, signing, notarization and release steps.

The marginal cost of signing git-in-track is therefore a secret, six repository
variables and one federated credential — not a purchase.

The complication is the release mechanics. Code signing is platform-bound: `codesign`
and `notarytool` only exist on macOS, and `signtool` and the Trusted Signing dlib only
on Windows. The current pipeline is a single GoReleaser run on Linux that cross-compiles
everything, archives it, checksums it and publishes the Release, the Homebrew cask, the
Scoop manifest and the GHCR images. Feeding pre-signed binaries back into a GoReleaser
run (`builds.builder: prebuilt`) is a **GoReleaser Pro** feature, which this project does
not have.

## Decision

**Sign macOS and Windows artifacts, and build each platform on the runner that can sign
it — dropping GoReleaser from the tag pipeline.**

- `.github/workflows/release.yml` becomes five jobs: `assets` (builds the WASM core and
  the Vite bundle once, so every binary embeds an identical `web/dist`), `linux`,
  `windows`, `macos`, and `release`.
- macOS: Developer ID signature with the hardened runtime and a secure timestamp, then
  notarization. The archives are `zip` (what `notarytool` takes) and are **submit-only**:
  a bare Mach-O cannot carry a stapled ticket, so they satisfy Gatekeeper's online check.
- Windows: Authenticode via Azure Trusted Signing, with the signature verified by
  `Get-AuthenticodeSignature` before the archive is built. The `.exe` is built on the
  Windows runner, so an unsigned binary never crosses a job boundary.
- Linux: unsigned, as is normal there.
- The signing, notarization and publishing steps are the shared composite actions in
  `digiogithub/ci-actions@v1`, the same ones Pando uses.
- `checksums.txt` remains, covering every artifact — the signature says who built it, the
  checksum says the download is intact.
- `.goreleaser.yaml` stays as the **local snapshot** builder (`make release-snapshot`).

## Consequences

**Positive**

- No Gatekeeper or SmartScreen dialog for the ordinary download path, which is the whole
  user-facing benefit ADR-011 gave up.
- No new spend, no new key custody: the certificates, the Azure account and the actions
  are shared with Pando, and Windows needs no secret at all.
- Each platform is built by its own toolchain, so a platform-specific build failure is
  attributable to one job instead of one monolithic run.

**Negative**

- **The distribution channels are paused.** The Homebrew cask, the Scoop manifest and the
  GHCR images were GoReleaser outputs of the tag run; they now have to be re-plumbed on
  top of the signed archives (both the cask and the manifest embed the archive's SHA-256)
  before they can be cut from a tag again. ADR-016's cask remains the intended shape.
- **Two more runner types per release** (macOS and Windows are billed at higher multipliers
  than Linux), and notarization adds a wait on Apple's service to every release.
- **A new operational dependency**: an expired Apple certificate or a lapsed Trusted
  Signing identity validation breaks releases, and neither failure is visible until a tag
  is pushed.
- **The macOS archives are `zip`, not `tar.gz`** — a change in artifact names for anyone
  who scripted a download URL.
- **The first signed Windows build has no SmartScreen reputation**; that builds per
  publisher over downloads, exactly as ADR-011 warned.

## Alternatives considered

- **Sign inside a single Linux job** with `jsign` (Trusted Signing from Linux) and
  `rcodesign` (Mach-O signing from Linux), keeping GoReleaser and every channel. Tempting,
  and rejected for one reason: notarization from Linux needs an App Store Connect API key,
  a credential DIGIO does not currently ship in the signing bundle, and an unnotarized
  signature does not clear Gatekeeper. Worth revisiting to bring the channels back.
- **GoReleaser Pro's `prebuilt` builder.** The clean fix for the channel loss; a paid
  subscription for one feature.
- **A `gobinary` shim** that makes OSS GoReleaser "build" by copying the pre-signed
  binaries. Keeps every channel with no licence, at the price of a hack that breaks
  silently whenever GoReleaser changes how it invokes the toolchain. Rejected.
- **Keep shipping unsigned and lean on Homebrew and Scoop** (ADR-011's position).
  Rejected: the material is already paid for, and the direct download is the path a
  first-time user takes.
