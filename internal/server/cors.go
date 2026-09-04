package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// subprotocol is the WebSocket sub-protocol the companion speaks. Browsers may
// carry the bearer token in the second sub-protocol entry, "bearer.<token>",
// because they cannot set request headers on a WebSocket.
const subprotocol = "gintrack.v1"

// bearerSubprotocolPrefix marks that token-carrying entry.
const bearerSubprotocolPrefix = "bearer."

// viteDevPort is the Vite dev server, allowed only in development mode.
const viteDevPort = 5173

// corsHeaders are the request headers a browser may send (docs/07 §5.2).
const corsHeaders = "Authorization, Content-Type, If-Match, X-Request-Id"

// corsMethods are the methods the API answers.
const corsMethods = "GET, POST, PATCH, PUT, DELETE, OPTIONS"

// corsExposed are the response headers a browser may read.
const corsExposed = "ETag, X-Total-Count, X-Request-Id"

// staticOrigins are the origins allowed regardless of the port the listener
// ended up on: the loopback origins of the configured port, the Vite dev server
// in development mode, and whatever the configuration added.
func (s *Server) staticOrigins() []string {
	origins := loopbackOrigins(s.opts.Port)
	if s.opts.Dev {
		origins = append(origins, loopbackOrigins(viteDevPort)...)
	}
	origins = append(origins, s.opts.ExtraOrigins...)
	return origins
}

// loopbackOrigins renders the two spellings of a loopback origin.
func loopbackOrigins(port int) []string {
	if port <= 0 {
		return nil
	}
	return []string{
		fmt.Sprintf("http://127.0.0.1:%d", port),
		fmt.Sprintf("http://localhost:%d", port),
	}
}

// originAllowed reports whether a browser origin may read this API. The
// embedded origin is always allowed, which covers the case of a listener that
// resolved a wildcard port after the options were built.
func (s *Server) originAllowed(origin string, r *http.Request) bool {
	if origin == "" {
		return false
	}
	if r != nil && r.Host != "" {
		if origin == "http://"+r.Host || origin == "https://"+r.Host {
			return true
		}
	}
	for _, allowed := range s.staticOrigins() {
		if origin == allowed {
			return true
		}
	}
	return false
}

// originPatterns renders the allow-list as the host patterns the WebSocket
// upgrade checks. The Host of the request itself is always accepted by the
// library, so only the extra origins have to be listed.
func (s *Server) originPatterns() []string {
	origins := s.staticOrigins()
	out := make([]string, 0, len(origins))
	for _, origin := range origins {
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			out = append(out, u.Host)
			continue
		}
		out = append(out, origin)
	}
	if _, port, err := net.SplitHostPort(s.Addr()); err == nil {
		out = append(out, "127.0.0.1:"+port, "localhost:"+port)
	}
	return out
}

// corsMiddleware answers preflights and decorates allowed cross-origin
// responses. An origin that is not on the list simply gets no CORS headers,
// which is what makes a drive-by page on the open internet unable to read the
// API even though it can reach the port.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := s.originAllowed(origin, r)
		if allowed {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "false")
			h.Set("Access-Control-Expose-Headers", corsExposed)
		}
		w.Header().Add("Vary", "Origin")

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if allowed {
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", corsMethods)
				h.Set("Access-Control-Allow-Headers", corsHeaders)
				h.Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// subprotocolToken extracts a token from the WebSocket sub-protocol list.
func subprotocolToken(r *http.Request) string {
	for _, entry := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		entry = strings.TrimSpace(entry)
		if token, ok := strings.CutPrefix(entry, bearerSubprotocolPrefix); ok {
			return token
		}
	}
	return ""
}
