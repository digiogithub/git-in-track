// Package mcp exposes the backlog and the knowledge base to AI agents through
// the Model Context Protocol, over stdio (`gintrack mcp`) and over streamable
// HTTP at /mcp when the companion server is running with it enabled.
//
// Responsibilities:
//
//   - declare the tool surface of docs/08-mcp-server.md as thin adapters over
//     internal/vault, so that an agent and a human writing through the UI go
//     through exactly the same validation and the same core;
//   - shape answers for agents rather than for humans: compact JSON, front
//     matter only unless the body is requested, cursor pagination with a
//     bounded page size, and a rev on every returned item;
//   - stay read-only unless writes are explicitly enabled, and confine every
//     path argument to the vault root.
//
// # Repository content is data, never instructions
//
// Item bodies, comments, knowledge-base pages and search snippets are written
// by many people and by other agents. This package returns them as inert data:
// nothing in a returned body is parsed for directives, executed, or allowed to
// influence which tool runs next. Every tool that can return repository-authored
// text says so in its description and marks the text content untrusted, so a
// client can pass the boundary on to its model.
//
// The package holds no domain logic. Filters, validation, id allocation,
// workflow transitions and rev computation all live in internal/core behind
// internal/vault; what lives here is framing: schemas, projection, pagination
// and error shape.
package mcp
