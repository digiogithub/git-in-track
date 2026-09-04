# team-basic

Fixture team repository. It declares two projects: `DEMO`, which the tests open
alongside it from `testdata/fixtures/project-basic`, and `WEB`, which is never
cloned and is therefore rendered as a remote project.

`.pmngr/boards/delivery.md` is the kanban board of GIT-US-0017. It shows cards
from both projects at once: the `DEMO` ones resolve to real items, the `WEB`
ones stay remote.

`.pmngr/index/WEB.json` is the committed index snapshot of GIT-US-0019: the
title, status, labels and assignees the `WEB` cards render from, generated on
2026-09-03. Pin the clock to that day in a test that asserts staleness. The file
is byte-for-byte what `EncodeProjectSnapshot` writes; regenerate it with
`go test ./internal/core -run TestSnapshotFixtureIsStable -update` and review
the diff.
