# ADR-040 — `gintrack update` is a stdlib self-update, verified against the release's `checksums.txt`

- **Status:** Accepted — 2026-09-30. The maintainer decided the four points below on 2026-09-30.
- **Date:** 2026-09-30
- **Phase:** Post-1.0 (distribution), epic GIT-EP-0032
- **Related:** [ADR-005](ADR-005-companion-cli-go-embed.md),
  [ADR-016](ADR-016-homebrew-cask-instead-of-formula.md),
  [ADR-029](ADR-029-signed-release-artifacts.md),
  [ADR-039](ADR-039-gintrack-launches-and-manages-pando.md)
- **Does not change:** the data model (docs/03), the release pipeline (`release.yml`,
  `.goreleaser.yaml`, both human-only) or the roadmap (docs/11). It turns the published
  asset names into a documented contract (docs/09 §3.1).
- **Evidence:** the Pando updater at `/www/MCP/Pando/pando` (`cmd/update.go`,
  `internal/updatecheck/release.go`, `internal/updatecheck/status.go`) and the assets of
  release `v2.2.0`.

## Context

`gintrack` ships as release archives, a Homebrew cask, a Scoop manifest and a container image
(docs/09 §10). A user on a release archive has no way to upgrade except repeating the download
steps of docs/07 §2.1. Pando, a sibling DIGIO product, has a `pando update` command. It is the
model for this one, but it has weaknesses gintrack should not inherit:

| Pando today                                              | Consequence                                        |
| -------------------------------------------------------- | -------------------------------------------------- |
| No checksum, signature or size check on the download     | A truncated or tampered archive replaces the binary |
| Pre-releases are not filtered                            | `update` can install an `-rc`                      |
| "Latest" is the first release in API order, of 30        | Not the highest version; a backport can win        |
| No download timeout and no size cap                      | A stalled or hostile response hangs or fills a disk |
| Overwrites a package-manager install                     | `brew`/`scoop` state and the binary disagree       |
| The stderr update notice is unconditional                | Noise in scripts, and output on stderr of a tool that speaks JSON or MCP |
| Unauthenticated GitHub API, 60 requests per hour per IP  | Shared CI and office IPs hit `403` quickly         |

gintrack's release facts, as of `v2.2.0`:

- `release.yml` builds each platform on the runner that can sign it (ADR-029): Linux `tar.gz`,
  Windows `zip` (Authenticode), macOS `zip` (Developer ID, notarised), plus `checksums.txt`
  (sha256) and a GitHub Release. The assets are `gintrack_2.2.0_{linux,darwin,windows}_{amd64,arm64}`
  with `.tar.gz` for linux and `.zip` for the other two, plus `checksums.txt`.
- The binary knows its origin through `cmd/gintrack/main.go`: `version` (`dev` when built from
  source), `builtBy` (`source` by default, `github-actions` for the tag workflow) and `commit`.
- The cask, the Scoop manifest and the GHCR images are paused (docs/09 §10), but they exist in
  `.goreleaser.yaml` and may come back; an installation through one of them is owned by that
  package manager.

## Decision

The maintainer decided on 2026-09-30, for epic GIT-EP-0032:

1. **Notices are pull, not push.** `gintrack doctor` and the web UI show "update available",
   from an answer cached for about six hours. A notice when the CLI starts happens only with the
   opt-in `update.checkOnStart: true`, and never in JSON or MCP output. There is no telemetry:
   the only request is the one to GitHub, and nothing identifies the user.
2. **Install channels are respected.** Homebrew, Scoop and Docker installs are refused, with a
   pointer to `brew upgrade`, `scoop update` or pulling the image. A source or dev build
   (`version=dev`, `builtBy=source`) accepts only an explicit version. `--force` overrides both.
3. **Only verified stable releases.** By default only stable releases are considered, choosing the
   highest semantic version; `--prerelease` opts in. Every download is verified against the
   release's `checksums.txt` (sha256) and against the asset size, and the update aborts on any
   mismatch, leaving the running binary untouched.
4. **Standard library, no auto-restart.** The implementation uses `net/http`, `archive/tar`,
   `archive/zip`, `crypto/sha256` and a minimal internal semver, with no new dependency. The swap
   is atomic with rollback and handles a running `.exe` on Windows. After updating, a running
   `gintrack serve` or managed Pando is detected and the user is told to restart it; nothing
   restarts on its own.

The code lives in a native package, `internal/selfupdate`, plus `cmd/gintrack`. It never goes in
`internal/core` (the WASM rule in AGENTS.md).

### Security

- **Transport:** HTTPS to `api.github.com` and the release download URLs, with the system trust
  store. Redirects are followed only to GitHub-owned hosts over HTTPS.
- **Integrity:** the sha256 of the downloaded archive must equal its line in `checksums.txt`, and
  the byte count must equal the asset `size` the API reports. A missing line, a missing
  `checksums.txt`, or any mismatch aborts. A download is capped at that size (with a fixed
  ceiling when the API gives none) and runs under a timeout, so a hostile response cannot fill a
  disk or hang the command.
- **Authenticity:** `checksums.txt` travels with the archives on the same release, so it proves
  integrity against corruption and a bad mirror, not against a compromise of the release itself.
  `gintrack update` does **not** verify the macOS or Windows signatures (ADR-029). That would
  need platform tools (`codesign`, `Get-AuthenticodeSignature`) and a trust policy that the
  standard-library approach does not carry. Users who need the stronger check follow docs/09 §4
  by hand. Signature verification on update can be a later ADR.
- **Pre-releases:** excluded unless `--prerelease`. Drafts are always excluded. A version that is
  not greater than the running one is not an update, so the command never downgrades unless the
  user names the version.
- **Rate limits:** the release lookup is one unauthenticated request (60 per hour per IP). It
  sends `Authorization: Bearer $GITHUB_TOKEN` when that variable is set, which lifts the limit
  to 5000 per hour. The token is sent only to `api.github.com`, never to a download host, and is
  never logged. A `403` or `429` is reported with the reset time, not as "no update".
- **Filesystem:** the new binary is written next to the running one (same filesystem, so the
  rename is atomic), and only `gintrack` or `gintrack.exe` is extracted from the archive: no
  other entry name is honoured, and entries with path separators or `..` are rejected.

## Options considered

### Library (`go-selfupdate`, `go-update`, `minio/selfupdate`) versus the standard library

A library would save the replace-the-running-binary code and some platform quirks. It was rejected:

- the AGENTS.md rule is not to add dependencies casually, and the whole job is perhaps 500 lines;
- the libraries do not know our asset contract (zip for darwin, tar.gz for linux, a flat layout,
  `checksums.txt` lines), so we would write the adapter anyway;
- each has its own opinions on release selection and signature schemes (minisign, ECDSA) that we
  do not publish, and on which the decision 3 rules (stable only, highest semver) must still be
  enforced by us;
- a dependency under `cmd/gintrack` is one more thing to audit for a tool that overwrites its own
  executable.

### Auto-restart versus a hint

Restarting a running `gintrack serve` or a managed Pando would finish the upgrade in one step.
It was rejected: the update command may be run by a different user or terminal than the one that
owns the server, a restart kills in-flight requests and jobs, and a managed Pando is a child
process with its own lifecycle (ADR-039). Telling the user what is still on the old version is
cheap and never surprising.

### Notice on every start

Pando's behaviour. Rejected in favour of decision 1: it writes to stderr of tools whose output
is parsed, and it contacts the network without being asked.

## Consequences

### Easier

- A release-archive user upgrades with `gintrack update`, and a corrupt download is refused.
- Package-manager users are steered to their own tool, so the two never disagree.
- The asset names are a written contract, so a pipeline change that would break the updater is
  caught in review (docs/09 §3.1).

### Harder, and what we accept

- `internal/selfupdate` depends on the published asset names. Renaming an archive, changing the
  layout or dropping `checksums.txt` breaks every installed updater, including old versions that
  are already in the field, and cannot be repaired after the fact. The contract is in docs/09 §3.1.
- Without a signature check, a compromised release could ship a matching `checksums.txt`. We
  accept this for now, for the reason given under *Authenticity*.
- No detached rollback of the release itself: the previous binary is kept only until the swap
  succeeds.

## Implementation

Tracked under epic GIT-EP-0032:

- GIT-US-0192: find, download and verify a release (`internal/selfupdate`: lookup, semver,
  checksum and size).
- GIT-US-0193: replace the running binary safely (atomic swap, rollback, Windows `.exe`) and
  detect the install channel.
- GIT-US-0194: the `gintrack update` command, its flags and the restart hint.
- GIT-US-0195: "update available" in `gintrack doctor` and the web UI, the six-hour cache and
  the opt-in `update.checkOnStart`.
- GIT-US-0196 (human-only follow-up): `.goreleaser.yaml` still describes a darwin `tar.gz`
  while the tag pipeline ships a signed darwin `zip`. It is only used by
  `make release-snapshot` now, but it should match the contract of docs/09 §3.1.
