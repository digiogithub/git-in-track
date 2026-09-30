package agentcfg

import (
	"slices"
	"strings"
	"testing"
)

func TestTools(t *testing.T) {
	t.Run("the allow-list admits no tool that writes a document", func(t *testing.T) {
		for _, w := range WritingKBTools {
			if slices.Contains(Tools, w) {
				t.Errorf("Tools admits %s", w)
			}
		}
	})
	t.Run("WithoutCodeTools drops exactly the code tools", func(t *testing.T) {
		got := WithoutCodeTools(Tools)
		for _, c := range CodeTools {
			if slices.Contains(got, c) {
				t.Errorf("still has %s", c)
			}
		}
		if len(got) != len(Tools)-len(CodeTools) || !slices.Contains(got, "gintrack_*") {
			t.Errorf("got %v", got)
		}
	})
}

func TestRender(t *testing.T) {
	data := TemplateData{
		RepoID: "r", AGUIPath: DefaultAGUIPath, AGUIHost: "127.0.0.1", AGUIPort: 4242,
		MaxConcurrentRuns: DefaultMaxRuns, Persona: PersonaID, Tools: Tools, MCPURL: "http://127.0.0.1:1/mcp",
	}
	for _, name := range []string{TemplatePando, TemplatePersona, TemplateSkill} {
		out, err := Render(name, data)
		if err != nil || len(out) == 0 {
			t.Fatalf("Render(%s) = %d bytes, %v", name, len(out), err)
		}
	}
	out, _ := Render(TemplatePando, data)
	if !strings.Contains(string(out), "Port = 4242") {
		t.Error("the port was not rendered")
	}
}
