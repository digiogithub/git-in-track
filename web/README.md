# `web/` — git-in-track frontend

React 18 + Vite 6 + TypeScript single-page application. It is built to
`web/dist`, which the `gintrack` binary embeds with `go:embed`, and it serves
two runtimes from the same bundle (see `docs/05-web-app.md`).

## Commands

All commands run from this directory. Node 20+ is required.

| Command                 | What it does                                             |
| ----------------------- | -------------------------------------------------------- |
| `npm ci`                | Install exactly the versions in `package-lock.json`      |
| `npm run dev`           | Vite dev server on <http://localhost:5173>               |
| `npm run dev:companion` | Same, with provider auto-detection forced to `companion` |
| `npm run build`         | `tsc -b && vite build` → `web/dist`                      |
| `npm run preview`       | Serve the production build locally                       |
| `npm run lint`          | ESLint 9 flat config, `--max-warnings 0`                 |
| `npm run typecheck`     | `tsc -b` (strict, `noUncheckedIndexedAccess`)            |
| `npm run test`          | Vitest (`npm run test -- --run` for a single pass)       |
| `npm run format`        | Prettier over the whole folder                           |

From the repository root the Makefile wraps these: `make web`, `make lint`,
`make test`.

## Structure

```
public/            static assets; core.wasm and wasm_exec.js land here (git-ignored)
src/
  main.tsx         bootstrap: providers, router, mode detection
  index.css        Tailwind entry + design tokens (CSS custom properties)
  app/
    router.tsx     code-based TanStack Router route tree
    providers.tsx  QueryClientProvider
    queryClient.ts TanStack Query defaults
    store.ts       Zustand store (runtime mode + capability snapshot)
    layout/        AppShell, NotFound
  api/
    provider.ts    the DataProvider interface and its vocabulary
    detect.ts      companion health probe and mode selection
  core-bridge/
    protocol.ts    request/response envelope shared with the worker
    worker.ts      Web Worker hosting the Go core compiled to WASM
    client.ts      typed CoreClient (request ids, timeouts, crash handling)
  components/ui/   vendored shadcn/ui-style primitives (button, card, input)
  features/        workspace, kb, backlog, boards, settings
  lib/cn.ts        clsx + tailwind-merge class composition
  test/setup.ts    Testing Library setup
```

Routes shipped by the Phase 0 scaffold: `/`, `/repos/add`, `/p/$project/kb/*`,
`/p/$project/items`, `/p/$project/items/$id`, `/boards`, `/settings`.

## The two modes and the companion proxy

The app never talks to a filesystem or to git directly. Everything goes through
the `DataProvider` interface in `src/api/provider.ts`, which has two
implementations (both land in later phases):

- **Browser-only** — File System Access API handles, `isomorphic-git`, and the
  Go core compiled to WebAssembly running in `src/core-bridge/worker.ts`.
- **Companion** — `gintrack serve` on `http://127.0.0.1:7317` provides native
  filesystem access, `go-git` and fsnotify through `/api/v1`.

`src/api/detect.ts` picks between them at boot: it issues
`GET /api/v1/health` with a 1500 ms timeout and, on a valid answer, records
`mode: 'companion'` plus the reported version in the Zustand store; every other
outcome (timeout, connection refused, CORS refusal, unexpected body) falls back
to `mode: 'browser'`. `VITE_FORCE_PROVIDER=browser|companion` skips detection.

During development the Vite dev server proxies `/api` (including the
`/api/v1/events` WebSocket upgrade) to `http://127.0.0.1:7317`, so the dev
server behaves like the embedded build and the probe is same-origin. Start the
companion alongside the dev server:

```bash
gintrack serve --dev     # companion on :7317, permissive CORS for :5173
npm run dev              # Vite on :5173
```

## WASM artifacts

`public/core.wasm` and `public/wasm_exec.js` are produced by `make wasm` and are
git-ignored. The worker loads them lazily and degrades gracefully when they are
absent: `ping` answers `{ pong: true, wasm: false }` and core-backed methods
fail with `core_unavailable`, so `npm run dev` works on a clean checkout.

## Conventions

- No default exports in `src/` (tool-mandated config files excepted).
- Feature code imports through the `@/` alias; relative imports stay inside a
  folder.
- Colours are tokens (`bg-background`, `text-muted-foreground`, …) defined in
  `src/index.css`; never hardcode a hex value in a component.
- Class names are composed with `cn()` and variants with `cva`.
