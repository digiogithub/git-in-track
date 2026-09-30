package main

import (
	"strings"
	"testing"
)

// TestSpecImpactHelpDescribesPando keeps the help text honest about how the
// command reaches Pando (GIT-US-0183).
func TestSpecImpactHelpDescribesPando(t *testing.T) {
	cmd := newSpecImpactCommand(&globalFlags{})
	for _, want := range []string{"search.pando.mcpUrl", "managed", "gintrack serve", "unavailable"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("help does not mention %q", want)
		}
	}
	if strings.Contains(cmd.Long, "no Pando client") {
		t.Error("help still claims the command line has no Pando client")
	}
}
