package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The transports. Everything above this file is transport-agnostic, which is
// the point: `gintrack mcp` over stdio and POST /mcp on the companion serve the
// same tools, from the same registry, over the same workspace. A client that
// works against one works against the other.
//
// This file is also where the SDK is isolated, as docs/08-mcp-server.md section 1
// requires: a change of SDK — or of protocol revision — touches these functions
// and the thin wrapper in tools.go, nothing else.

// ServeStdio speaks JSON-RPC 2.0 over stdin and stdout until the context is
// canceled or the client disconnects.
//
// Nothing may be written to stdout but protocol frames, so a caller must have
// pointed every logger at stderr before calling this.
func (s *Server) ServeStdio(ctx context.Context) error {
	if err := s.sdk.Run(ctx, &sdk.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp over stdio: %w", err)
	}
	return nil
}

// HTTPHandler returns the streamable-HTTP handler to mount at /mcp. Sessions
// are stateful: the handler issues an Mcp-Session-Id on initialize and expects
// it on every later call, and DELETE terminates one.
//
// The handler performs no authentication of its own. It is mounted behind the
// companion's bearer token and origin checks, which is where every other local
// API surface is authenticated too — one boundary, not two.
func (s *Server) HTTPHandler() http.Handler {
	return sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return s.sdk },
		&sdk.StreamableHTTPOptions{Logger: s.log},
	)
}

// HTTPHandlerFor returns a streamable-HTTP handler that asks `current` which
// server to serve on every request. A host that rebuilds its server — the write
// tools being switched on or off while it runs — keeps one handler and one
// mount point instead of re-registering a route.
//
// Swapping the server ends the sessions the previous one held: an initialized
// client is told its session is unknown and initializes again, which is the
// same thing it does when the companion restarts. That is why the swap belongs
// to a deliberate settings change and to nothing else.
func HTTPHandlerFor(current func() *Server, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server {
			s := current()
			if s == nil {
				return nil
			}
			return s.sdk
		},
		&sdk.StreamableHTTPOptions{Logger: log},
	)
}
