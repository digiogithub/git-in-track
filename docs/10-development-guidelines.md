# 10 — Development Guidelines

Coding standards, conventions and process for **git-in-track**. These rules apply to every
contribution, human or AI agent. They are enforced by CI where possible and by review
where not.

**All code, comments, documentation, commit messages and issue text are written in
English.** No exceptions, including for a Spanish-speaking core team.

- Module path: `github.com/digiogithub/git-in-track`
- CLI binary: `gintrack`
- Monorepo: Go core + CLI + server + MCP, React/Vite web app, Go→WASM build of the core

---

## 1. Repository layout

```
cmd/gintrack/            CLI entry point (cobra commands only, no business logic)
internal/core/           shared core: model, frontmatter, index, query, ids  -> also WASM
internal/server/         HTTP/WS API, embeds web/dist
internal/watcher/        fsnotify file watching
internal/gitops/         go-git wrapper (and system-git shell-out)
internal/mcp/            MCP server
wasm/                    WASM entry (main_js.go) + JS glue
web/                     React + Vite app
docs/                    planning docs + knowledge base + dogfooded .pmngr backlog
.github/workflows/       ci.yml, release.yml
Makefile, go.mod, .goreleaser.yaml
```

**The golden rule of this layout**: `internal/core` is the only package that understands
the data model, and it must compile for both `GOOS=linux` (and friends) and
`GOOS=js GOARCH=wasm`. Everything else is a delivery mechanism around it.

Consequences that are non-negotiable:

- `internal/core` must not import `os/exec`, `net/http`, `fsnotify`, `go-git`, or anything
  else unavailable or meaningless in a browser. File access goes through a small
  `core.FS` interface implemented natively by `os`-backed code and in WASM by a JS bridge
  to File System Access handles.
- Platform-specific code uses build tags (`//go:build js && wasm` / `//go:build !js`) and
  lives in files suffixed `_js.go` / `_native.go`, never in `if runtime.GOOS` branches.
- `cmd/gintrack` contains flag parsing, wiring and output formatting. If a cobra command
  function is longer than ~40 lines, the logic belongs in a package.
- `internal/server` may depend on `core`, `gitops`, `watcher`, `mcp`. Nothing may depend on
  `internal/server`.

---

## 2. Go standards

### 2.1 Formatting and imports

- `gofmt` is mandatory; CI fails on any unformatted file.
- `goimports -local github.com/digiogithub/git-in-track` groups imports in three blocks:
  standard library, third-party, project-local — in that order, separated by blank lines.
- Line length is not hard-limited, but wrap around 100 columns for readability.
- No `gofmt -s` violations (simplify is part of the lint set).

### 2.2 `golangci-lint` configuration outline

`.golangci.yaml` at the repository root:

```yaml
version: "2"

run:
  timeout: 5m
  tests: true

linters:
  enable:
    - errcheck        # unchecked errors
    - govet           # suspicious constructs
    - staticcheck     # includes gosimple and stylecheck
    - ineffassign     # unused assignments
    - unused          # unused code
    - revive          # style, replaces golint
    - errorlint       # correct %w / errors.As usage
    - wrapcheck       # errors crossing package boundaries must be wrapped
    - bodyclose       # http response bodies closed
    - rowserrcheck
    - contextcheck    # context propagated correctly
    - noctx           # no http requests without context
    - gocritic
    - misspell
    - unconvert
    - predeclared
    - copyloopvar
    - nilerr
    - testifylint
  settings:
    revive:
      rules:
        - name: exported          # doc comments on exported identifiers
        - name: package-comments
        - name: error-strings
        - name: error-naming
        - name: context-as-argument
        - name: indent-error-flow
    errcheck:
      check-type-assertions: true
    gocritic:
      enabled-tags: [diagnostic, style, performance]
      disabled-checks: [hugeParam, rangeValCopy]
    misspell:
      locale: US

  exclusions:
    rules:
      # Table-driven tests legitimately ignore some errors in setup helpers.
      - path: _test\.go
        linters: [errcheck, wrapcheck, gocritic]
      # Generated WASM glue is not ours to style.
      - path: wasm/glue/
        linters: [revive, gocritic]

formatters:
  enable:
    - gofmt
    - goimports
  settings:
    goimports:
      local-prefixes: [github.com/digiogithub/git-in-track]
```

Adding a linter is a PR of its own: enable it, fix the whole repository, merge. Never
enable a linter and leave `//nolint` sprinkled behind. Any `//nolint` **must** name the
linter and carry a reason: `//nolint:errcheck // best-effort close on a read-only file`.

### 2.3 Errors

- Return errors, do not log-and-continue. Only `cmd/` and the top-level HTTP handlers
  decide how an error is presented.
- Wrap with `%w` and add the operation, not the type: `fmt.Errorf("parse front matter %s:
  %w", path, err)`. Never `fmt.Errorf("error: %w", err)`.
- Error strings are lowercase, no trailing punctuation, no "failed to" prefix (the caller
  adds context; "failed to failed to parse" reads badly).
- Sentinel errors are exported vars named `ErrX` and compared with `errors.Is`:
  `core.ErrItemNotFound`, `core.ErrDuplicateID`, `core.ErrInvalidFrontMatter`,
  `core.ErrRevMismatch`.
- Rich errors are types named `XError` implementing `Unwrap()`, matched with `errors.As`:

  ```go
  // ValidationError reports a front-matter field that violates the schema.
  type ValidationError struct {
      Path  string // file that failed validation
      Field string // front-matter field name
      Msg   string
  }

  func (e *ValidationError) Error() string {
      return fmt.Sprintf("%s: field %q: %s", e.Path, e.Field, e.Msg)
  }
  ```

- `panic` is only acceptable for programmer errors detected at init time (an impossible
  switch default, a malformed embedded template). Never panic on user input; a malformed
  Markdown file is an expected condition.
- Validation collects **all** problems before returning, using `errors.Join`, so the UI can
  show every issue in a file at once instead of one per save.

### 2.4 Context

- Every function that does I/O, crosses a package boundary and may block takes
  `ctx context.Context` as its **first** parameter. Never store a context in a struct
  field.
- Never pass `nil`; use `context.TODO()` only in code that is explicitly marked for
  follow-up, never in merged production paths.
- HTTP handlers use `r.Context()`. The server sets a per-request timeout; long operations
  (full reindex, git fetch/push) get their own deadline and are cancellable from the UI.
- The indexer, the watcher and the git operations all honour cancellation and return
  `ctx.Err()` wrapped, so a user navigating away actually stops the work.
- `context.Value` is reserved for request-scoped metadata (request ID, actor). It is never
  used to pass dependencies — those are constructor parameters.

### 2.5 API design and package doc comments

- Accept interfaces, return structs. Interfaces are declared by the **consumer** package
  and kept small (`core.FS`, `core.Clock`, `index.Store`).
- No global mutable state and no `init()` side effects. Dependencies are injected through
  constructors: `core.NewIndexer(fs, opts)`.
- Exported identifiers have doc comments that start with the identifier name and are full
  sentences:

  ```go
  // Package core implements the shared, platform-independent model of a git-in-track
  // vault: front-matter parsing, validation, ID allocation, indexing and querying.
  //
  // It compiles both natively (for the CLI and the companion server) and to
  // WebAssembly (GOOS=js GOARCH=wasm) for the browser-only mode, so it must not
  // depend on the operating system directly. All file access goes through FS.
  package core

  // ParseItem parses a Markdown file with YAML front matter into an Item.
  // It returns ErrInvalidFrontMatter if the front-matter block is missing or
  // malformed, and a *ValidationError if a field violates the schema.
  func ParseItem(ctx context.Context, path string, r io.Reader) (*Item, error)
  ```

- Every package has a package comment, in a `doc.go` file when it is longer than a few
  lines.
- Unexported helpers get comments when the *why* is not obvious. Do not comment the *what*
  when the code already says it.

### 2.6 Tests

- **Table-driven by default.** One test function per behaviour, a `tests := []struct{...}`
  slice with a `name` field, `t.Run(tt.name, ...)`, and `t.Parallel()` in both the outer
  and inner function where the code under test allows it.

  ```go
  func TestParseFrontMatter(t *testing.T) {
      t.Parallel()

      tests := []struct {
          name    string
          input   string
          want    Item
          wantErr error
      }{
          {name: "minimal story", input: "...", want: Item{ /* ... */ }},
          {name: "missing delimiter", input: "...", wantErr: ErrInvalidFrontMatter},
      }

      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              t.Parallel()
              got, err := ParseFrontMatter(t.Context(), strings.NewReader(tt.input))
              if !errors.Is(err, tt.wantErr) {
                  t.Fatalf("err = %v, want %v", err, tt.wantErr)
              }
              // ...
          })
      }
  }
  ```

- **Golden files for the parser, the renderer and the query engine.** Inputs live in
  `testdata/`, expected outputs in `testdata/golden/<test-name>.json` (or `.html`,
  `.md`). Every golden test supports `go test ./... -update` via a package-level flag:

  ```go
  var update = flag.Bool("update", false, "rewrite golden files")
  ```

  Golden files are reviewed like code — a diff in a golden file in a PR must be explained
  in the PR description. Never regenerate goldens to "make the test pass".
- Assertions use the standard library plus `github.com/google/go-cmp/cmp` for structs.
  `testify/require` is allowed in tests only, never in non-test code.
- Tests never touch the network, never write outside `t.TempDir()`, and never depend on
  wall-clock time (inject a `core.Clock`).
- Test names describe behaviour: `TestIndexer_SkipsFilesOutsidePmngr`, not `TestIndexer2`.
- Fuzz targets exist for the front-matter parser and the wikilink extractor
  (`FuzzParseFrontMatter`); they run in CI on a short budget and in a nightly job for
  longer.
- Benchmarks for the indexer and the query engine live in `internal/core/*_test.go` and are
  compared before/after any performance-labelled PR with `benchstat`.

---

## 3. TypeScript / React standards

### 3.1 TypeScript

- `strict: true`, plus `noUncheckedIndexedAccess`, `noImplicitOverride`,
  `exactOptionalPropertyTypes`, `verbatimModuleSyntax`. `tsconfig.json` outline:

  ```json
  {
    "compilerOptions": {
      "target": "ES2022",
      "lib": ["ES2022", "DOM", "DOM.Iterable", "WebWorker"],
      "module": "ESNext",
      "moduleResolution": "bundler",
      "jsx": "react-jsx",
      "strict": true,
      "noUncheckedIndexedAccess": true,
      "noImplicitOverride": true,
      "exactOptionalPropertyTypes": true,
      "noUnusedLocals": true,
      "noUnusedParameters": true,
      "verbatimModuleSyntax": true,
      "isolatedModules": true,
      "skipLibCheck": true,
      "baseUrl": ".",
      "paths": { "@/*": ["./src/*"] }
    },
    "include": ["src", "vite.config.ts"]
  }
  ```

- **`any` is banned** (`@typescript-eslint/no-explicit-any` as an error). Use `unknown` and
  narrow. Data crossing a trust boundary — WASM results, REST responses, YAML front matter
  — is validated with `zod` schemas that are the single source of the TypeScript types
  (`type Item = z.infer<typeof itemSchema>`).
- Non-null assertions (`!`) require a comment explaining the invariant.
- Prefer `type` aliases for unions and object shapes; use `interface` only when
  declaration merging is genuinely needed.
- Discriminated unions model item kinds: `type Item = Epic | Story | Task | Milestone`,
  discriminated on `type`.

### 3.2 ESLint and Prettier

- ESLint flat config (`eslint.config.js`) with `typescript-eslint` (type-checked ruleset),
  `eslint-plugin-react`, `react-hooks`, `react-refresh`, `jsx-a11y`,
  `eslint-plugin-import` (enforcing `import/no-default-export`), and
  `eslint-config-prettier` last so formatting rules never conflict.
- Prettier owns formatting: 2-space indent, single quotes, semicolons, trailing commas
  (`all`), print width 100, with `prettier-plugin-tailwindcss` to sort class names
  deterministically. Nobody argues about formatting in review.
- `npm run lint` = `eslint . --max-warnings 0`. Warnings are errors in CI.

### 3.3 Components

- **Function components only.** No class components.
- **No default exports** anywhere in `web/src` (the sole exceptions are files a tool
  requires it for: `vite.config.ts`, `tailwind.config.ts`, and route modules if TanStack
  Router's file-based routing demands it — these are listed as ESLint overrides). Named
  exports keep rename-refactors honest and make auto-import unambiguous.
- One component per file; the file name equals the component name.
- Props are a named exported type `XProps`, destructured in the signature:

  ```tsx
  export type StoryCardProps = {
    story: Story;
    onOpen: (id: string) => void;
    dense?: boolean;
  };

  export function StoryCard({ story, onOpen, dense = false }: StoryCardProps) {
    // ...
  }
  ```

- Components render; they do not fetch, parse or write files directly. Data comes from
  TanStack Query hooks; mutations go through hooks that wrap the core adapter (WASM in
  browser-only mode, REST in companion mode) so a component never knows which mode it is
  in.
- Keep components under ~150 lines. Extract subcomponents and hooks rather than growing a
  file.
- Every interactive element is keyboard-reachable and labelled; `jsx-a11y` violations fail
  the build.

### 3.4 Hooks

- Custom hooks start with `use`, live in `src/hooks/` (shared) or beside their feature, and
  return objects, not positional tuples, when they expose more than two values.
- Rules of hooks are enforced by `eslint-plugin-react-hooks` with
  `exhaustive-deps` as an **error**. Silencing it requires a comment explaining why.
- Server/vault state belongs to TanStack Query (`useQuery`/`useMutation`) with stable,
  hierarchical query keys (`['project', projectKey, 'stories', filters]`) defined in one
  `queryKeys.ts` factory. UI state (open panels, drag state, filters-in-progress) belongs
  to Zustand stores, one slice per feature.
- No `useEffect` for derived state. `useEffect` is for subscriptions and imperative
  synchronisation only (WebSocket, file watcher events, CodeMirror instances).
- Expensive parsing/indexing never runs on the main thread: it goes to the Web Worker that
  hosts the WASM core, behind a typed request/response channel.

### 3.5 File and directory naming

```
web/src/
  components/ui/        shadcn primitives (generated, minimally edited)
  components/           shared app components      StoryCard.tsx
  features/<feature>/   feature slices            features/board/BoardColumn.tsx
    components/
    hooks/              useBoardDrag.ts
    api/                boardQueries.ts
    types.ts
  hooks/                shared hooks              useCompanion.ts
  lib/                  pure helpers, no React    lib/frontmatter.ts, lib/cn.ts
  workers/              web workers               workers/core.worker.ts
  routes/               TanStack Router routes    routes/projects.$key.board.tsx
  styles/
```

- Components: `PascalCase.tsx`. Hooks: `useCamelCase.ts`. Everything else:
  `camelCase.ts`. Tests: `X.test.ts(x)` next to the file under test. Types-only modules:
  `types.ts`.
- Import with the `@/` alias; relative imports only inside the same feature folder.

### 3.6 Tailwind and shadcn/ui

- Tailwind utility classes in JSX are the default. No CSS modules, no styled-components.
  A `@layer components` class is created only when the same 8+ utility combination appears
  in three or more places.
- Class names are composed with the `cn()` helper (`clsx` + `tailwind-merge`) so
  caller-supplied `className` reliably overrides.
- Conditional variants use `class-variance-authority` (`cva`) rather than nested ternaries.
- Design tokens live in `tailwind.config.ts` and as CSS variables in `styles/globals.css`
  (the shadcn convention: `--background`, `--foreground`, `--primary`, ...). **Never
  hardcode a hex colour in a component**; dark mode is a token swap on `.dark` and must
  keep working.
- Arbitrary values (`w-[327px]`) are a smell; add a token instead.
- shadcn/ui components are **vendored** into `components/ui/` with the CLI
  (`npx shadcn@latest add button`). They are treated as project source: edits are allowed
  and reviewed, but keep them generic — app-specific behaviour wraps them in
  `components/`, it does not fork them. Record which components were added in
  `web/components.json`. Do not upgrade a vendored component silently; a regeneration is
  its own PR.
- Radix primitives underneath shadcn provide accessibility; do not replace them with
  hand-rolled dropdowns/dialogs.

### 3.7 Frontend tests

- **Vitest + Testing Library** for units and components. Query by role and accessible name
  (`getByRole('button', { name: /new story/i })`); `data-testid` only when there is no
  accessible alternative.
- `userEvent`, not `fireEvent`.
- Network and companion API calls are stubbed with MSW so tests exercise the real query
  layer.
- The WASM core is faked in component tests through the same adapter interface the app
  uses; the real WASM module is exercised in dedicated integration tests and in Go tests.
- **Playwright** for end-to-end flows against `gintrack serve` with a fixture repository:
  open project → browse KB → create story → edit → verify the file on disk.

---

## 4. Markdown documentation style

- One sentence-per-idea, wrapped at 100 columns. Long lines make diffs unreadable.
- ATX headings (`##`), one `#` H1 per document, headings in sentence case, no trailing
  `#`. Numbered document files (`09-`, `10-`, `11-`) start with `# NN — Title`.
- Fenced code blocks always carry a language (` ```go `, ` ```yaml `, ` ```bash `). Shell
  examples show the command without a `$` prefix so they can be copied.
- Relative links between docs (`[roadmap](./11-roadmap.md)`), never absolute URLs into the
  repository. Wikilinks (`[[Page]]`) are for knowledge-base pages inside a vault, not for
  these planning docs.
- Tables use GFM; keep them narrow, prefer a list when a table would need horizontal
  scrolling.
- Mermaid diagrams in ` ```mermaid ` fences. Keep them small enough to read at a glance;
  a diagram with more than ~15 nodes should be split.
- Admonitions use the blockquote convention (`> **Note**`, `> **Warning**`).
- American English spelling, Oxford comma, no emoji in docs.
- Every document states its status (planning / implemented / superseded) when that is not
  obvious from context.
- `markdownlint` runs in CI on `docs/**/*.md` with the default ruleset minus MD013
  (line-length, handled by the 100-column convention) and MD033 (inline HTML, needed
  occasionally).

---

## 5. Commit message convention

[Conventional Commits 1.0.0](https://www.conventionalcommits.org/).

```
<type>(<scope>): <subject>

<body>

<footers>
```

### Types

| Type       | Use for                                              | In changelog |
| ---------- | ---------------------------------------------------- | ------------ |
| `feat`     | A new user-visible capability                        | Features     |
| `fix`      | A bug fix                                            | Bug fixes    |
| `perf`     | A change that improves performance                   | Performance  |
| `refactor` | Restructuring with no behaviour change               | Refactors    |
| `docs`     | Documentation only                                   | Documentation|
| `test`     | Tests only                                           | hidden       |
| `build`    | Build system, dependencies, Makefile, GoReleaser     | hidden       |
| `ci`       | GitHub Actions and CI configuration                  | hidden       |
| `chore`    | Housekeeping that fits nowhere else                  | hidden       |
| `style`    | Formatting only                                      | hidden       |
| `revert`   | Reverting a previous commit                          | Others       |

### Scopes

Exactly one of: **`core`**, **`cli`**, **`server`**, **`web`**, **`wasm`**, **`mcp`**,
**`docs`**, **`ci`**.

| Scope    | Covers                                                        |
| -------- | ------------------------------------------------------------- |
| `core`   | `internal/core` — model, front matter, index, query, IDs       |
| `cli`    | `cmd/gintrack` — commands, flags, output                       |
| `server` | `internal/server`, `internal/watcher`, `internal/gitops`       |
| `web`    | `web/` — React app                                             |
| `wasm`   | `wasm/` — WASM entry point and JS glue                         |
| `mcp`    | `internal/mcp` — MCP tools and transport                       |
| `docs`   | `docs/`, `README.md`, and the dogfooded `.pmngr` backlog       |
| `ci`     | `.github/workflows`, `Makefile`, `.goreleaser.yaml`, linters   |

A change that genuinely spans scopes (a model change threaded through core, server and web)
is split into one commit per scope where possible; if it must be atomic, use the scope of
the primary change and mention the rest in the body.

### Rules

- Subject: imperative mood, lowercase, no trailing period, ≤ 72 characters.
  `feat(core): add rev hash to item front matter`, not `Added rev hashes.`
- Body: wrapped at 72–100 columns, explains **why**, not what. Optional for trivial
  changes, expected for anything non-obvious.
- Breaking changes: `!` after the scope **and** a `BREAKING CHANGE:` footer describing the
  migration.

  ```
  feat(core)!: rename story `epic` field to `parent`

  Aligns stories, tasks and milestones on a single parent field so the
  hierarchy can be walked generically by the index.

  BREAKING CHANGE: existing story files using `epic:` must be migrated.
  `gintrack migrate --to 2` rewrites them in place.
  ```

- Reference work items in footers: `Refs: GIT-US-0007`, `Closes: #42`.
- Because PRs are squash-merged, **the PR title is the commit message that lands** and must
  satisfy all of the above. Commits inside a branch may be messy; the title may not.
- Trailers for co-authorship follow the standard `Co-Authored-By:` form. Commits produced
  with AI assistance are attributed the same way as any other collaborator.

---

## 6. Pull requests

### Size and shape

- One logical change per PR. Target under 400 changed lines excluding generated files,
  lockfiles and golden data. A PR that must be larger explains why in its description and
  provides a reading order.
- Draft PRs are welcome early; mark ready for review only when CI is green.
- Rebase on `main` rather than merging `main` in; the branch must be up to date before
  merge (enforced by branch protection).

### Description template (`.github/pull_request_template.md`)

```markdown
## What

Short description of the change.

## Why

The problem this solves. Link the story: `Refs: GIT-US-XXXX`.

## How

Notable implementation decisions, trade-offs, and anything a reviewer should read first.

## Testing

How this was verified: new tests, manual steps, platforms exercised.

## Screenshots / recordings

For any UI change, before and after, light and dark.

## Checklist

- [ ] Title is a valid Conventional Commit with a valid scope
- [ ] Tests added or updated
- [ ] Docs updated (`docs/`, README, doc comments)
- [ ] No new lint warnings, no new `//nolint` without a reason
- [ ] Data model change? migration + `schemaVersion` bump + changelog note
- [ ] Works in both browser-only and companion mode (or explicitly N/A)
```

### Review checklist

Reviewers check, in this order:

1. **Correctness** — does it do what the story asked? Are edge cases handled (empty vault,
   malformed front matter, duplicate IDs, missing parent, unreadable file)?
2. **Scope** — does the diff match the description? Unrelated drive-by changes are asked to
   be split out.
3. **Platform parity** — does anything added to `internal/core` still compile to WASM? Does
   it assume POSIX paths, case-sensitive filesystems, or `/` separators?
4. **Data model** — does it change files on disk? Is the change backwards compatible? Is
   there a migration? Does the on-disk shape still match
   [03-data-model.md](./03-data-model.md)?
5. **Errors and context** — errors wrapped with context, `ctx` threaded through, no
   swallowed errors, no `panic` on user input.
6. **Tests** — do they fail without the fix? Are they table-driven? Are goldens explained?
7. **Performance** — anything O(n²) over items, anything blocking the UI thread, anything
   re-indexing the whole vault on a single file change.
8. **Security** — see §8. Any new network listener, any credential handling, any path
   joined from user input.
9. **Accessibility and UX** — roles, labels, keyboard paths, focus management, dark mode.
10. **Docs** — public behaviour documented, doc comments on new exported identifiers.

Review etiquette: comments are specific and actionable; suggest code where possible; mark
non-blocking remarks as `nit:`. Approving means "I would be comfortable owning this code".

### Merging

- Squash merge only. The maintainer merging is responsible for the final title.
- Any PR touching the data model, the MCP tool schemas or the REST API needs two approvals.
- AI agents may open PRs; they are reviewed by a human before merge, without exception.

---

## 7. Testing strategy

### The pyramid

```
        /\        E2E (Playwright)            ~5%   flows, both modes, one browser matrix
       /  \       Integration                ~25%   server+core+fs, WASM in a real worker,
      /----\                                        git operations against temp repos
     /      \     Unit (Go tests, Vitest)    ~70%   parser, index, query, hooks, components
```

- **Unit**: pure functions and single components. Fast (whole Go suite under 30 s), no I/O
  beyond `t.TempDir()`.
- **Integration**: the companion server against a fixture repository on disk; the indexer
  and watcher together; `go-git` against a repository created in a temp dir; the MCP server
  driven over stdio with recorded tool calls.
- **E2E**: a handful of critical journeys only, because they are the slowest and flakiest:
  open a project, render a KB page with Mermaid, create a story and see the file appear,
  drag a card between board columns and see the status change on disk, sync with a remote.

### Coverage targets

| Area                                        | Target       | Enforcement          |
| ------------------------------------------- | ------------ | -------------------- |
| `internal/core` (parser, model, index, query)| **≥ 85 %**  | CI gate, hard fail   |
| `internal/gitops`, `internal/mcp`            | ≥ 70 %       | CI gate              |
| `internal/server`                            | ≥ 65 %       | reported, soft       |
| `cmd/gintrack`                               | ≥ 50 %       | reported             |
| `web/src/lib`, `web/src/**/hooks`            | ≥ 80 %       | CI gate              |
| `web/src` components                         | ≥ 60 %       | reported             |
| Overall repository                           | ≥ 70 %       | reported             |

Coverage is a floor, not a goal. A PR may not lower the coverage of a gated package.
Chasing the number with tests that assert nothing is a review failure.

### Fixture repositories

Deterministic fixtures live under `testdata/fixtures/` and are used by Go tests, Vitest
integration tests and Playwright alike.

```
testdata/fixtures/
  project-basic/            a minimal project repository
    docs/
      index.md
      architecture.md               (wikilinks, Mermaid, GFM tables, task list)
      .pmngr/
        project.yaml                key: DEMO, default workflow
        epics/DEMO-EP-0001-onboarding.md
        stories/DEMO-US-0001-sign-in-with-sso.md
        stories/DEMO-US-0002-reset-password.md
        tasks/DEMO-T-0001-add-oidc-client.md
        milestones/DEMO-M-0001-mvp.md
        comments/DEMO-US-0001/20260101T101500Z-alice.md
  team-basic/               a minimal team repository
    team.yaml                       members + two project repo entries
    knowledge/handbook.md
    .pmngr/
      boards/delivery-kanban.md
      boards/squad-scrum.md
      sprints/2026-S01.md
      retros/2026-S01-retro.md
      index/DEMO.json               remote-reference snapshot
  invalid/                  deliberately broken files for error-path tests
    missing-delimiter.md
    duplicate-id-a.md
    duplicate-id-b.md
    unknown-status.md
```

Rules for fixtures:

- Fixtures are committed as plain files, never generated at test time, so a human can read
  the exact input a failing test used.
- `project-basic` and `team-basic` are **stable**: adding a field is fine, renaming or
  removing one is a breaking change that updates every dependent test in the same PR.
- Fixture repositories that need git history are created at test time by a helper
  (`testutil.InitRepo(t, "project-basic")`) that copies the fixture into `t.TempDir()` and
  runs the commits, so no `.git` directory is committed.
- Dates and authors in fixtures are fixed values (never `time.Now()`), so goldens are
  reproducible.

---

## 8. Security guidelines

- **No secrets in the repository.** Ever. No tokens, no `.env` with real values, no private
  keys, no `.netrc`, no fixture credentials that look real. `.gitignore` covers `.env*`,
  `*.pem`, `*.key`, `.gintrack/credentials*`. Secret scanning and push protection are
  enabled on the GitHub repository, and `gitleaks` runs in CI on every PR.
- **Localhost binding by default.** `gintrack serve` binds `127.0.0.1:7317`. Binding to
  any other interface requires the explicit `--allow-remote` flag, which prints a warning
  and refuses to start without an authentication token. There is no scenario in which the
  default configuration exposes a user's repositories to the network.
- **Local auth token.** The server generates a random token per run, writes it to
  `$XDG_STATE_HOME/gintrack/session` with `0600`, and requires it on every API and
  WebSocket request. The embedded UI receives it from the server; a browser page from
  another origin cannot read it.
- **CSRF and origin checks.** The API rejects requests whose `Origin` is not the server's
  own, and the WebSocket upgrade validates `Origin` explicitly (never
  `CheckOrigin: func(*http.Request) bool { return true }`).
- **Git credentials are never stored by git-in-track.** Native mode delegates to the user's
  existing git credential helper and SSH agent. Browser mode asks for a personal access
  token per session, keeps it in memory only (never `localStorage`), and scopes it to the
  single remote it was entered for. Tokens are redacted from all logs and error messages.
- **Path handling.** Every path derived from user input, front matter, wikilinks or MCP
  arguments is cleaned and verified to stay inside the vault root before use. A `..`
  segment or an absolute path is rejected, not normalised silently. This is the single most
  likely place for a serious bug, and it gets tests for symlinks, Windows drive letters,
  UNC paths and case-insensitive collisions.
- **Untrusted content.** Repository content is untrusted input: rendered Markdown is
  sanitised (no raw `<script>`, no `javascript:` or `data:` URLs, links open with
  `rel="noopener noreferrer"`), YAML is parsed with `yaml.v3` in a mode that cannot
  instantiate arbitrary types, and file sizes are bounded before parsing. The MCP server
  treats item bodies as data and never as instructions.
- **Dependencies.** Dependabot is enabled for Go modules, npm and GitHub Actions.
  `govulncheck` runs in CI weekly and on release. Actions are pinned to major version tags
  from verified publishers; third-party actions are pinned to a commit SHA. Adding a
  dependency requires a one-line justification in the PR.
- **Least privilege in CI.** Workflows declare `permissions:` explicitly;
  `contents: read` everywhere except the release job. `pull_request_target` is never used.
- **Reporting.** `SECURITY.md` documents private vulnerability reporting through GitHub
  Security Advisories, with a 90-day disclosure window.

---

## 9. Running everything locally

### Prerequisites

| Tool           | Version    | Notes                                              |
| -------------- | ---------- | -------------------------------------------------- |
| Go             | 1.23+      | `wasm_exec.js` is taken from `$(go env GOROOT)`     |
| Node.js        | 22 LTS     | npm 10+                                            |
| git            | 2.40+      | used by tests and by native git mode               |
| GNU Make       | any        | Windows: use WSL, Git Bash, or run commands by hand |
| golangci-lint  | 1.61+      | `go install github.com/golangci/golangci-lint/...`  |
| GoReleaser     | v2         | only for `make release-snapshot`                    |
| Chromium browser | recent   | File System Access API for browser-only mode        |

### First run

```bash
git clone https://github.com/digiogithub/git-in-track.git
cd git-in-track
make deps          # go mod download + npm ci
make build         # wasm -> web -> go build (embeds web/dist)
./dist/gintrack version
```

### Day-to-day: two-process dev loop

The productive setup runs the Go server and the Vite dev server side by side, so the React
app hot-reloads while talking to a real companion.

```bash
# Terminal 1 — companion server on 127.0.0.1:7317
make run
# equivalently: go run ./cmd/gintrack serve --addr 127.0.0.1:7317 --root ../my-project

# Terminal 2 — Vite dev server on http://localhost:5173
cd web && npm run dev
```

`vite.config.ts` proxies API and WebSocket traffic to the companion so there is no CORS
configuration in development:

```ts
export default defineConfig({
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:7317', changeOrigin: true },
      '/ws': { target: 'ws://127.0.0.1:7317', ws: true },
    },
  },
});
```

Open `http://localhost:5173`. The app detects the companion through `GET /api/health` and
upgrades from browser-only mode automatically. To develop **browser-only mode**, stop the
companion (terminal 1) and reload: the app falls back to the WASM core and the File System
Access API, and you pick a folder from the UI.

After changing anything under `internal/core`, rebuild the WASM module so the browser sees
it: `make wasm` (Vite serves `web/public/` directly, so a page reload is enough).

### Common targets

```bash
make help              # list all targets
make wasm              # rebuild web/public/core.wasm
make web               # wasm + vite build -> web/dist
make build             # full binary with embedded UI
make test              # go test -race + vitest
make lint              # gofmt/vet/golangci-lint + eslint/tsc
make run               # build and serve on 127.0.0.1:7317
make release-snapshot  # local GoReleaser dry run
make clean
```

### Useful one-offs

```bash
go test ./internal/core/... -run TestParse -v          # focused Go test
go test ./internal/core/... -update                    # regenerate golden files
cd web && npm run test -- --run StoryCard              # focused Vitest
cd web && npx playwright test --ui                     # E2E with the inspector
./dist/gintrack mcp                                    # MCP server on stdio
```

### Troubleshooting

- **`core.wasm` 404 in the browser** — run `make wasm`; the file must exist at
  `web/public/core.wasm` (dev) or be copied into `web/dist` by the Vite build (prod).
- **`wasm_exec.js` version mismatch** — re-run `make wasm`, which copies the glue from the
  active `GOROOT`. Mixing a new binary with an old glue file fails with cryptic errors.
- **"This browser does not support the File System Access API"** — expected in Firefox and
  Safari; those fall back to read-only mode. Use a Chromium browser for write flows.
- **File changes not detected on Windows** — see the watcher notes in
  [11-roadmap.md](./11-roadmap.md) §5; the fallback is polling, enabled with
  `--watch-mode=poll`.

---

## 10. Definition of Done

A story is `done` only when every one of these is true. This list is the checklist a
reviewer applies before moving a card to Done, and it is what an AI agent must satisfy
before claiming completion.

1. Every acceptance criterion in the story is met and its checkbox is ticked in the story
   file.
2. The code is merged to `main` through a reviewed, squash-merged PR whose title is a valid
   Conventional Commit.
3. CI is green on `main`: `go vet`, `go test -race`, `golangci-lint`, ESLint, `tsc`,
   Vitest, Vite build, WASM build, full embedded build.
4. New behaviour has tests at the right level of the pyramid, and gated packages have not
   lost coverage.
5. The change works in **both** operating modes, or the story explicitly scopes itself to
   one and says so.
6. Cross-platform: paths, line endings and file watching behave on Linux, macOS and
   Windows, or the limitation is documented.
7. Errors are surfaced usefully to the user — no silent failures, no raw Go errors in the
   UI.
8. Any change to the on-disk data model ships with a migration, a `schemaVersion` bump and
   a `CHANGELOG.md` note.
9. Documentation is updated: doc comments, the relevant file under `docs/`, and the README
   if user-facing behaviour changed.
10. No new lint suppressions, no `TODO` without an issue reference, no commented-out code.
11. Accessibility checked for UI work: keyboard path, focus order, roles and labels, dark
    mode.
12. The story file in `docs/.pmngr/stories/` has `status: done` and `updated:` set, and its
    PR is linked in the body.

---

## 11. Writing and maintaining these docs

The `docs/` folder is simultaneously the project's planning documentation, its knowledge
base, and the fixture we dogfood: `docs/.pmngr/` holds git-in-track's own backlog, managed
by git-in-track itself.

### Structure

```
docs/
  NN-topic.md          numbered planning documents, read in order
  adr/                 architecture decision records, NNNN-title.md
  .pmngr/              this project's own backlog (dogfooding)
    project.yaml
    epics/  stories/  tasks/  milestones/  comments/
```

### Rules

- **Numbered documents are stable.** Never renumber a document; the numbers are referenced
  from commits, PRs and stories. A document that is superseded keeps its number and gains
  a `> **Superseded by** NN-<slug>.md` note at the top.
- **The brief wins.** Names, paths, the tech stack and the phase list come from the
  architecture brief. A document that needs to contradict it must first change it, in its
  own PR.
- **Decisions go in ADRs**, not buried in a planning document. An ADR is short: context,
  decision, consequences, status (proposed / accepted / superseded). Link the ADR from the
  document that depends on it.
- **Docs change in the same PR as the code they describe.** A `docs:`-only PR is for
  planning and for corrections, not for catching up after the fact.
- **The dogfooded backlog is real.** Stories in `docs/.pmngr/stories/` are the actual work
  items; they are created, moved and closed as the work happens, and their IDs appear in
  commit footers (`Refs: GIT-US-0012`). Keep `docs/11-roadmap.md` and the story files
  consistent: same IDs, same titles. When they disagree, the story file is authoritative
  for status and the roadmap is authoritative for sequencing.
- **IDs are never reused.** A cancelled story keeps its ID with `status: cancelled`.
- **Front matter is validated in CI** once the parser exists: a `docs`-scoped CI job runs
  `gintrack validate docs/.pmngr` so our own backlog can never be malformed. Until then,
  reviewers check it by hand against the data model.
- **Screenshots** live in `docs/assets/`, are PNG, and are regenerated rather than edited.
- Every new document is added to the index in `docs/README.md` with a one-line summary.
