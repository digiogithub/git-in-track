---
title: Kitchen sink
tags: [fixture, markdown]
---

# Kitchen sink

Prose with **bold**, _italic_, ~~struck~~ text, an autolink to www.example.com,
an external link to [the spec](https://example.com/spec) and an inline `code`
span holding a literal `[[wikilink]]`.

## Wikilinks

- Page: [[architecture/overview]]
- Page with an alias: [[architecture/overview|the design]]
- Page with an anchor: [[architecture/overview#Session revocation]]
- Item: [[ACME-US-0042]]
- Cross-project item: [[WEB/WEB-US-0031]]
- Cross-project page: [[WEB:architecture/overview]]
- Unwritten page: [[architecture/not-yet]]

## Task list

- [x] Ship the parser
- [ ] Ship the editor

## Table

| Field    | Type   | Notes               |
| -------- | ------ | ------------------- |
| `id`     | string | Immutable           |
| `status` | string | From `project.yaml` |

## Callouts

> [!NOTE]
> GitHub alerts and Obsidian callouts share one renderer.

> [!warning]- Folded by default
> The body only appears when the reader opens it.

## Code

```go
func Slug(title string) string {
	return strings.ToLower(title)
}
```

```mermaid
graph TD;
  A-->B;
```

## Assets

![Flow diagram](../assets/flow.png)

A relative link to [the sibling page](./sso.md#session-revocation).

## Footnotes

Front matter is bounded[^1]; bodies are not.

[^1]: See docs/03-data-model.md §3.2.
