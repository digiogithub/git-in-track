# ADR-032 — The YouTrack token is stored on the machine, the link is stored in git

- **Status**: Accepted
- **Phase**: 7
- **Related**: [ADR-002](ADR-002-git-as-only-sync.md),
  [ADR-025](ADR-025-the-cors-proxy-security-model.md),
  [ADR-031](ADR-031-external-references.md)

## Context

Phase 7 connects a backlog to YouTrack. Reaching a YouTrack instance needs a
**permanent token**, a long-lived credential that grants the account's full
access to the instance — every project the account can see, read and write.
Nothing in git-in-track needed such a thing before.

The tool's security model said so explicitly:
`docs/10-development-guidelines.md` promised that *git-in-track never stores
credentials*, because until now it never had to. Native git delegates to the
user's existing credential helper and SSH agent, and browser mode asks for a
personal access token per session and keeps it in memory only. Both of those are
someone else's store, or no store at all.

A YouTrack token cannot work that way. The companion has to present it on a
background sync, on a comment push, on a scheduled import — at moments when
nobody is at the keyboard to retype it. "Ask every time" is not a policy for a
process whose entire job is to run unattended.

Meanwhile the *other* half of the connection has the opposite requirement. Which
instance, which remote project, which field maps to which — a clone has to know
that, or every teammate reconnects the same project by hand and the `external`
references of ADR-031 point into an instance the repository cannot name. That
half belongs in git, with the rest of the state (ADR-002).

The available stores for the secret half were:

1. **`project.yaml`**, next to the link. Committed, shared with everyone who can
   read the repository, and pushed to whatever host it lives on.
2. **An OS keychain** — Keychain, Credential Manager, Secret Service. The right
   place in principle, and a new dependency, a new failure mode per platform, a
   headless-Linux story with no answer, and an ADR of its own.
3. **The machine-local configuration file**, which already holds the companion's
   own bearer token and is already created `0600`.
4. **Nothing** — an environment variable only, supplied by whoever starts the
   process.

## Decision

**The connection is split in two, and only one half is ever committed.**

The **committed half** is `integrations.youtrack` in the project's
`project.yaml` (doc 03 §6.5): instance URL, YouTrack project short name, field
map, `push_comments` and `kb_sync` modes. A clone therefore knows where its items
came from without holding a credential. It is written surgically, node by node,
so a write cannot delete the comments and unmodelled sections of a file the user
owns.

The **secret half** is the token, stored in `integrations.youtrack.<projectKey>.token`
of the machine-local configuration file — the one already created with mode
`0600`, in the per-platform location of doc 07 §3.1 — keyed by git-in-track
project key, so a machine serving two linked projects keeps two tokens.
`GINTRACK_YOUTRACK_TOKEN` overrides the file for every project, which is what a
CI checkout wants. The precedence is the documented one: flag, then environment,
then file, then nothing.

`GINTRACK_TOKEN` is deliberately **not** reused. It already means both the
companion's API bearer token and the HTTP password go-git authenticates a remote
with; a third meaning would make one leaked variable hand out three credentials.

**The token has exactly one destination: the YouTrack instance.** It is excluded
from the JSON encoding of the configuration, redacted by `gintrack config show`,
absent from every API response (`hasToken` and `tokenSource` report its presence
and provenance instead), absent from every problem document, every log line and
every WebSocket payload. `internal/youtrack` redacts it from its own errors on
every path, and no caller renders an error's cause itself.

**Browser-only mode stores nothing.** It cannot reach a YouTrack instance at all
— no companion, no token, and the same cross-origin wall that made the git CORS
proxy necessary (ADR-025) — so `features.youtrack` is false there and the whole
feature is hidden rather than half-offered. A token typed in a browser would live
in memory for the session only, never in `localStorage`, exactly as the git PAT
rule already demands.

The promise in `docs/10-development-guidelines.md` is revised, not quietly
dropped: it now says which single credential is stored, where, with which
permissions, and which ones are still never stored.

## Consequences

**Easier.** A connected project survives a restart, a reboot and an unattended
sync without anyone retyping anything. A teammate cloning the repository sees
where the items came from and needs only their own token. There is no new
dependency, no new platform-specific code path and no new file: the credential
goes into a file that already existed, already held a secret, and already had the
right mode.

**Harder, and accepted:**

- **git-in-track now stores a credential it did not generate.** The blanket
  "stores no credentials" claim is gone, and with it the simplicity of being able
  to say it. Every future security review has to start from this ADR instead.
- **`0600` is the whole boundary.** It protects the token from other users of the
  machine and from nothing else. A process running as the user — a malicious
  dependency in a build, a shell one-liner, a backup agent — reads the file as
  easily as the companion does. An OS keychain would have raised that bar; this
  does not.
- **A backup or a dotfile sync copies the token off the machine.** Users who sync
  `~/.config` to a git repository will commit it without noticing. The file
  header says the file holds secrets, which is a warning and not a defence.
- **A permanent token is broad and long-lived.** YouTrack permanent tokens are
  not scoped to one project, so the credential stored for project `DEMO` can read
  and write every project its account can see. Revocation is manual, in YouTrack.
- **The two halves can drift.** Someone can commit a `url` change and leave every
  teammate's stored token pointing at the old instance; the failure surfaces as a
  401 or a 404 at sync time rather than as a merge conflict.
- **`GINTRACK_YOUTRACK_TOKEN` is all-or-nothing.** It overrides the token of
  every project, so a machine serving two linked projects cannot use the
  environment for one and the file for the other.
- **Rotation is a command, not a flow.** Changing a token means running
  `gintrack youtrack connect` again or patching the settings; there is no expiry
  tracking and no warning before a token stops working.

## Alternatives considered

**The token in `project.yaml`.** Rejected outright. It is a committed file: the
credential would reach the git host, every clone, every fork and every CI log
that cats the file, and `git rm` would not unpublish it.

**An OS keychain (Keychain, Credential Manager, Secret Service).** The stronger
store, and rejected *for now* rather than on the merits: it adds a dependency to
a module that has none for this, three platform paths to test, and no answer for
the headless Linux box that is exactly where an unattended companion runs. It
remains the obvious successor, as its own ADR, once the feature has users.

**Environment variable only, never stored.** Rejected as the sole mechanism: it
turns every companion start into a provisioning step, pushes the secret into
shell history and process listings, and gives a desktop user no way to connect a
project from the UI. It is kept as an *override*, which is where it is genuinely
better — CI, containers, and anyone who would rather not have the file at all.

**One shared token for all projects, in the server section.** Rejected because
two linked projects can live on two instances, and because a per-project key is
what lets a single project be disconnected without disturbing another.
