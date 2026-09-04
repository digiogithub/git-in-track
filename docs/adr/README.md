# Architecture Decision Records

This directory records the architectural decisions behind **git-in-track**. Each
ADR captures one decision, the context that forced it, and the consequences we
accepted — including the bad ones. ADRs are written so that a contributor joining
in two years can understand *why* the system looks the way it does without having
to reconstruct the argument from the code.

## Format

Every ADR uses the same sections:

- **Status** — `Proposed`, `Accepted`, `Superseded by ADR-NNN`, or `Deprecated`.
- **Context** — the forces at play: requirements, constraints, and what we know at
  the time of writing.
- **Decision** — what we are doing, stated in the active voice.
- **Consequences** — what becomes easier, what becomes harder, and what we now have
  to live with. Negative consequences are mandatory; an ADR with only benefits is
  an advertisement, not a record.
- **Alternatives considered** — the options we rejected and the specific reason each
  was rejected.

## Conventions

- File name: `ADR-NNN-short-slug.md`, numbers never reused.
- ADRs are immutable once accepted. A changed decision means a **new** ADR that
  supersedes the old one; the old one is edited only to update its `Status` line
  and add a pointer.
- Anything that constrains the file format, the sync model, the public API, or the
  build and release process needs an ADR before it is implemented.
- ADRs are written in English, like everything else in this repository.

## Index

| ADR | Title | Status | Phase |
|-----|-------|--------|-------|
| [ADR-001](ADR-001-markdown-yaml-storage.md) | Markdown with YAML front matter as the storage format | Accepted | 0 |
| [ADR-002](ADR-002-git-as-only-sync.md) | Git is the only sync mechanism; no central server | Accepted | 0 |
| [ADR-003](ADR-003-shared-go-core-wasm.md) | A shared Go core compiled natively and to WebAssembly | Accepted | 0 |
| [ADR-004](ADR-004-browser-only-file-system-access.md) | Browser-only mode via the File System Access API | Accepted | 1 |
| [ADR-005](ADR-005-companion-cli-go-embed.md) | Companion CLI serving the embedded web app with `go:embed` | Accepted | 2 |
| [ADR-006](ADR-006-isomorphic-git-vs-go-git.md) | isomorphic-git in the browser, go-git (or system git) in the CLI | Accepted | 4 |
| [ADR-007](ADR-007-team-repo-references.md) | The team repo holds references and index snapshots, never copies | Accepted | 3 |
| [ADR-008](ADR-008-id-scheme.md) | Item ID scheme `<KEY>-<TYPE>-<NNNN>` | Accepted | 0 |
| [ADR-009](ADR-009-react-vite-typescript.md) | React 18 + Vite + TypeScript as the front-end stack | Accepted | 1 |
| [ADR-010](ADR-010-mcp-agent-surface.md) | MCP is the integration surface for AI agents | Accepted | 5 |
| [ADR-011](ADR-011-goreleaser-unsigned-artifacts.md) | GoReleaser with unsigned release artifacts in v1 | Accepted | 6 |
| [ADR-012](ADR-012-comments-as-separate-files.md) | Comments are separate files, not inline in the item | Accepted | 1 |
| [ADR-013](ADR-013-board-card-ordering.md) | Card order as a plain ordered list, not a fractional index | Accepted | 3 |
| [ADR-014](ADR-014-snapshots-stay-on-the-main-branch.md) | Index snapshots stay on the main branch, written only when their content changes | Accepted | 3 |
| [ADR-015](ADR-015-official-go-mcp-sdk-and-verb-noun-tools.md) | The official Go MCP SDK, and verb-noun tool names | Accepted | 5 |

## Related documents

- [Vision and scope](../01-vision-and-scope.md)
- [Architecture](../02-architecture.md)
