// Package syncengine is the companion's background job engine.
//
// It exists so that work started by an HTTP request — importing issues from a
// tracker, pushing a comment, publishing a knowledge-base page — runs off that
// request, coalesces with the work next to it, respects one shared outbound
// rate limit and survives both the response being closed and the process being
// restarted.
//
// # Shape
//
// A [Job] is a unit of work with a [Kind], a coalescing [Job.Key] and an opaque
// JSON payload. A [Handler] is registered per kind with [Engine.Register], so
// the engine never learns what an issue, a comment or a page is: it schedules,
// batches, rate-limits, retries and journals, and the handler does the work.
// Jobs that share a kind and a key coalesce into one batch, handed to the
// handler as a slice; jobs with different keys never merge.
//
// The lifecycle is New → Register → Start → Enqueue… → Close. [Engine.Flush]
// drains the queue, [Engine.Cancel] withdraws a job and [Engine.Snapshot]
// exposes everything a REST layer or a queue table needs to render.
//
// # Handlers must be idempotent
//
// This is a contract, not a suggestion. The same job is handed to a handler
// more than once when:
//
//   - a retryable error sends it round the retry ladder again;
//   - the process died while the job was running, in which case the journal
//     replays it as queued with its attempt count intact;
//   - a user retries it from the dead-letter list.
//
// A handler that writes must therefore be safe to run twice against the same
// input: look the target up before creating it, carry the `rev` of the item it
// read, and treat "already exists" as success.
//
// # State
//
// Job state moves queued → running → done | failed | cancelled. Two further
// edges exist and are documented on [Job.State]: running → queued when a
// retryable error schedules another attempt or a batch is abandoned mid-flight,
// and failed → queued when a user retries a dead-lettered job. Every transition
// goes through one guard ([transition]); nothing else is reachable.
//
// # Persistence
//
// The queue is journalled as JSON under the cache directory (see [Options] and
// [Journal]). The journal holds bookkeeping only — ids, kinds, keys, attempt
// counts, states, redacted errors — never item or page content, and never a
// credential. It is derived data: deleting it loses queued work, not user data,
// and a corrupt journal is moved aside rather than being fatal.
//
// # Time
//
// Every delay in the package — the debounce, the retry ladder, the rate limiter
// and the journal's write coalescing — goes through [Clock]. The default is the
// real one; tests inject a fake and advance it, so the suite has no wall-clock
// sleeps and stays deterministic under -race.
//
// The package is native-only. It must never be imported from internal/core,
// which compiles to WebAssembly.
package syncengine
