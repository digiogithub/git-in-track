// Package mcp exposes the backlog to AI agents through the Model Context
// Protocol, over stdio by default and over streamable HTTP at /mcp when the
// companion server is running with it enabled.
//
// Responsibilities:
//
//   - declare the tool surface of docs/08-mcp-server.md (list and read items,
//     search, create, update, move and comment) as thin adapters over the
//     core's Store and Query interfaces, so that an agent and a human writing
//     through the UI go through exactly the same validation;
//   - enforce rev-based optimistic locking on every write, returning the current
//     rev and front matter on a mismatch so an agent can merge without a second
//     round trip;
//   - stay read-only unless writes are explicitly enabled, and record the agent
//     name in the audit log and in the Agent: commit trailer.
//
// Implementation lands in Phase 5 (docs/08-mcp-server.md).
package mcp
