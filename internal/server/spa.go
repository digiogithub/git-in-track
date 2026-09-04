package server

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the embedded web application.
//
// A request whose path exists in the bundle is served from it; hashed assets get
// an immutable cache header and index.html gets none, so a redeployed binary is
// picked up on the next reload. Anything else that is not under /api/ falls back
// to index.html, so a client-side route survives a hard refresh.
//
// When the bundle has no index.html — a `go install` of a checkout where the
// frontend was never built — the fallback is a page that says how to build it,
// which keeps the API and the MCP server usable.
func (s *Server) spaHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.handleAPINotFound(w, r)
			return
		}
		if s.opts.UI == nil {
			s.writeUINotBuilt(w, r)
			return
		}

		name := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if name == "." || name == "/" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}

		if name != "index.html" {
			if served := s.serveAsset(w, r, name); served {
				return
			}
		}
		s.serveIndex(w, r)
	}
}

// serveAsset serves one file of the bundle and reports whether it existed.
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, name string) bool {
	f, err := s.opts.UI.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	seeker, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}
	if isHashedAsset(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, path.Base(name), info.ModTime(), seeker)
	return true
}

// serveIndex serves index.html, or the "not built" page when it is absent.
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.opts.UI, "index.html")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.writeUINotBuilt(w, r)
			return
		}
		writeProblem(w, r, http.StatusInternalServerError, "internal", "Internal error",
			"The embedded web application could not be read.")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// isHashedAsset reports whether a bundle path is content-addressed and can
// therefore be cached forever. Vite emits those under assets/.
func isHashedAsset(name string) bool {
	return strings.HasPrefix(name, "assets/")
}

// uiNotBuiltPage explains how to build the frontend. It is deliberately plain
// HTML with no assets, because there are none to serve.
const uiNotBuiltPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>git-in-track — UI not built</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 16px/1.6 system-ui, sans-serif; margin: 0; display: grid; place-items: center; min-height: 100vh; }
  main { max-width: 40rem; padding: 2rem; }
  h1 { font-size: 1.4rem; margin: 0 0 1rem; }
  code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  pre { padding: 0.75rem 1rem; border-radius: 6px; overflow-x: auto; background: rgba(127,127,127,0.15); }
</style>
</head>
<body>
<main>
  <h1>The web UI is not built</h1>
  <p>This <code>gintrack</code> binary was compiled without a frontend bundle in
     <code>web/dist</code>, so there is nothing to show here.</p>
  <p>Build it and restart the server:</p>
  <pre>make web
make build</pre>
  <p>The REST API is unaffected:
     <code>GET /api/v1/health</code> answers right now.</p>
</main>
</body>
</html>
`

// writeUINotBuilt serves the explanatory page.
func (s *Server) writeUINotBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, uiNotBuiltPage)
}
