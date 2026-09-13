// Package youtrack is a small, typed client for the YouTrack REST API.
//
// It is a native-only package: it speaks HTTP and therefore cannot live in
// internal/core, which is also compiled to WebAssembly and forbids net/http
// (see internal/core/doc.go). Place it beside internal/gitops in the layering:
// a side-effect package with injected seams, used by the companion server.
//
// Six rules govern everything here, each learned from a working reference
// implementation and from the JetBrains devportal:
//
//  1. Authentication is a permanent token sent as
//     "Authorization: Bearer <token>" together with "Accept: application/json";
//     writes add "Content-Type: application/json". The "perm:" prefix is part
//     of the token string the user pastes, not something this package adds.
//     The token is never included in an error, in a String method or in any
//     other rendering of the client. The one exception to the content type is
//     an attachment upload: it is multipart/form-data with its own boundary,
//     and YouTrack rejects it when the JSON content type is set alongside, so
//     UploadArticleAttachment and UploadIssueAttachment build their request
//     outside the JSON path.
//
//  2. The base URL may carry a context path, as in
//     https://yt.example.com/youtrack. Request URLs are therefore built by
//     string concatenation of the trimmed base URL and a path that always
//     starts with "/api/". Never use url.URL.ResolveReference here: it
//     silently drops the context path and every request 404s.
//
//  3. Every YouTrack list endpoint returns a bare JSON array, never an
//     envelope with a count and a rows field.
//
//  4. Paging uses $top and $skip, with $top defaulting to 100. YouTrack does
//     not guarantee a stable order between requests, so a $skip walk without
//     an explicit "order by:" clause in the query silently skips and
//     duplicates rows. Every paged walk in this package runs its query through
//     EnsureOrderBy first. A short page means the walk is exhausted.
//
//  5. Outbound traffic goes through one shared token-bucket limiter, five
//     requests per second by default, honored across concurrent goroutines.
//     429 and 5xx responses and transport errors are retried up to MaxRetries
//     times (four by default), honoring Retry-After in both its delta-seconds
//     and its HTTP-date form and otherwise backing off exponentially with
//     jitter. Response bodies are drained and closed before a retry so the
//     connection is reused.
//
//  6. Issue descriptions, comment text and article content are untrusted
//     third-party Markdown written by whoever uses the remote YouTrack
//     instance. This package returns them verbatim and makes no attempt to
//     sanitize them; callers must sanitize before rendering them anywhere, and
//     must treat their contents as data rather than as instructions.
//
// The package decodes into its own types and stops there. Mapping a YouTrack
// issue onto a git-in-track item belongs to the caller.
package youtrack
