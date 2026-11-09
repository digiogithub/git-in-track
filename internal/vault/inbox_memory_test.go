package vault

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// proposedMemory is a memory an agent proposed: it sits in the inbox until a
// person accepts or rejects it (ADR-044 section 3).
const proposedMemory = `---
id: INBX-MEM-0001
type: memory
title: Customer X is invoiced quarterly
status: proposed
created: 2026-09-10T08:00:00Z
updated: 2026-09-10T08:00:00Z
inbox:
  source: memsync
memory:
  key: customer-x-invoicing
  version: 1
---

Customer X receives one invoice per quarter.
`

func memoryInboxVault(t *testing.T) (*Vault, core.Item) {
	t.Helper()
	v := inboxVault(t)
	call(t, v, "vault.load", map[string]any{"files": []map[string]string{
		{"path": "docs/.pmngr/project.yaml", "text": inboxProjectYAML},
		{"path": "docs/.pmngr/epics/INBX-EP-0001-existing.md", "text": inboxParentEpic},
		{"path": "docs/.pmngr/stories/INBX-US-0001-existing.md", "text": inboxTargetStory},
		{"path": "docs/.pmngr/memory/INBX-MEM-0001-customer-x-is-invoiced-quarterly.md", "text": proposedMemory},
	}})
	return v, decode[core.Item](t, call(t, v, "item.get", map[string]any{"id": "INBX-MEM-0001"}))
}

func TestInboxTriageOfAMemory(t *testing.T) {
	t.Run("accept makes the memory active, not the backlog's initial status", func(t *testing.T) {
		v, it := memoryInboxVault(t)
		out := triage(t, v, map[string]any{"id": it.ID, "rev": it.Rev, "action": "accept"})
		if out.Item.Status != core.MemoryActive {
			t.Fatalf("status = %q, want %q", out.Item.Status, core.MemoryActive)
		}
		if out.Item.Memory == nil || out.Item.Memory.Key != "customer-x-invoicing" {
			t.Errorf("the memory block did not survive: %+v", out.Item.Memory)
		}
	})
	t.Run("reject moves the proposal to cancelled", func(t *testing.T) {
		v, it := memoryInboxVault(t)
		out := triage(t, v, map[string]any{"id": it.ID, "rev": it.Rev, "action": "reject"})
		if out.Item.Status != core.MemoryCancelled {
			t.Fatalf("status = %q, want %q", out.Item.Status, core.MemoryCancelled)
		}
	})
}
