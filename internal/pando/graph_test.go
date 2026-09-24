package pando

import (
	"context"
	"errors"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Recorded from Pando's code_impact_analysis on 2026-09-24: the callers arrive
// as a TOON tabular array.
const impactTOON = `callers[2]{depth,file_path,name,name_path,start_line,symbol_type}:
  1,internal/pando/toon.go,decodeStructured,/decodeStructured,79,function
  2,internal/pando/search.go,decode,/toolResult.decode,57,method
count: 2
symbol: decodeTOON
truncated: true`

// Recorded from Pando's code_related_files on 2026-09-24: list form with an
// inline primitive array per entry.
const relatedTOON = `count: 2
file: internal/pando/search.go
related[2]:
  - file_path: internal/vault/vault_test.go
    reasons[2]: calls,imports
    score: 1.86
  - file_path: internal/server/api_test.go
    reasons[1]: calls
    score: 1.78
truncated: false`

// code_find_symbol renders compact lines as text and the machine-readable page
// in structuredContent.metadata, like code_hybrid_search.
var findSymbolMetadata = map[string]any{
	"count": 2, "total": 7, "offset": 5, "limit": 2, "has_more": true, "next_offset": 7,
	"symbols": []map[string]any{
		{
			"symbol_type": "method", "name": "SearchKB", "name_path": "/Client.SearchKB",
			"file_path": "internal/pando/search.go", "start_line": 118,
			"signature": "func (c *Client) SearchKB(ctx context.Context, query string, o KBSearchOptions) ([]KBHit, error)",
		},
		{
			"symbol_type": "function", "name": "searchKB", "name_path": "/searchKB",
			"file_path": "internal/mcp/tools_kb.go", "start_line": 262, "end_line": 290,
			"future_field": "ignored",
		},
	},
}

func TestImpactAnalysis(t *testing.T) {
	t.Parallel()

	t.Run("parses callers and merges symbols", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		f.setTool(toolCodeImpact, func(_ context.Context, args map[string]any) (*mcpsdk.CallToolResult, error) {
			if args["symbol"] == "unused" {
				return textResult(`No callers found for symbol "unused" (nothing in the indexed graph depends on it).`), nil
			}
			return textResult(impactTOON), nil
		})
		c := newTestClient(t, f, func(o *Options) { o.ProjectID = "default_project" })

		got, err := c.ImpactAnalysis(context.Background(), "", []string{"decodeTOON", " ", "unused", "decodeTOON"},
			ImpactOptions{Depth: 3, Limit: 10})
		if err != nil {
			t.Fatalf("ImpactAnalysis() error = %v", err)
		}
		if !got.Truncated {
			t.Error("Truncated = false, want true")
		}
		if len(got.Callers) != 2 {
			t.Fatalf("got %d callers, want 2", len(got.Callers))
		}
		want := ImpactCaller{
			Symbol: "decodeTOON", Name: "decode", NamePath: "/toolResult.decode", SymbolType: "method",
			FilePath: "internal/pando/search.go", StartLine: 57, Depth: 2,
		}
		if got.Callers[1] != want {
			t.Errorf("Callers[1] = %#v, want %#v", got.Callers[1], want)
		}

		calls := f.callsFor(toolCodeImpact)
		if len(calls) != 2 {
			t.Fatalf("got %d calls, want one per distinct non-empty symbol (2)", len(calls))
		}
		args := calls[0]
		if args["project_id"] != "default_project" || args["symbol"] != "decodeTOON" {
			t.Errorf("args = %v", args)
		}
		if args["depth"] != float64(3) || args["limit"] != float64(10) {
			t.Errorf("depth/limit = %v/%v", args["depth"], args["limit"])
		}
	})

	t.Run("empty result is not an error", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		f.setTool(toolCodeImpact, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
			return textResult(`No callers found for symbol "x".`), nil
		})
		c := newTestClient(t, f, nil)
		got, err := c.ImpactAnalysis(context.Background(), "p", []string{"x"}, ImpactOptions{})
		if err != nil {
			t.Fatalf("ImpactAnalysis() error = %v", err)
		}
		if len(got.Callers) != 0 || got.Truncated {
			t.Errorf("got %#v, want an empty result", got)
		}
		args := f.callsFor(toolCodeImpact)[0]
		for _, k := range []string{"depth", "limit"} {
			if _, ok := args[k]; ok {
				t.Errorf("%s was sent although it was not set", k)
			}
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		c := newTestClient(t, f, nil)
		if _, err := c.ImpactAnalysis(context.Background(), "p", []string{" "}, ImpactOptions{}); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("no symbols: error = %v, want ErrInvalidOptions", err)
		}
		if _, err := c.ImpactAnalysis(context.Background(), "", []string{"x"}, ImpactOptions{}); !errors.Is(err, ErrNotConfigured) {
			t.Errorf("no project: error = %v, want ErrNotConfigured", err)
		}
		if f.callCount() != 0 {
			t.Errorf("invalid input reached Pando %d times", f.callCount())
		}
	})
}

func TestFindSymbol(t *testing.T) {
	t.Parallel()

	t.Run("parses structured metadata", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		f.setTool(toolCodeSymbol, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
			return metadataResult("6. method internal/pando/search.go:118 /Client.SearchKB", findSymbolMetadata), nil
		})
		c := newTestClient(t, f, nil)

		got, err := c.FindSymbol(context.Background(), "p", "SearchKB", FindSymbolOptions{
			RelativePath: "internal/", SymbolTypes: []string{"method"}, Languages: []string{"go"},
			Substring: true, Limit: 2, Offset: 5,
		})
		if err != nil {
			t.Fatalf("FindSymbol() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d symbols, want 2", len(got))
		}
		if got[0].NamePath != "/Client.SearchKB" || got[0].StartLine != 118 || got[0].Rank != 6 {
			t.Errorf("symbols[0] = %#v", got[0])
		}
		if got[1].StartLine != 262 || got[1].EndLine != 290 || got[1].Rank != 7 {
			t.Errorf("symbols[1] line range/rank = %d-%d/%d", got[1].StartLine, got[1].EndLine, got[1].Rank)
		}

		args := f.callsFor(toolCodeSymbol)[0]
		if args["name_path_pattern"] != "SearchKB" || args["relative_path"] != "internal/" ||
			args["substring_matching"] != true || args["limit"] != float64(2) || args["offset"] != float64(5) {
			t.Errorf("args = %v", args)
		}
		if _, ok := args["group_by_file"]; ok {
			t.Error("group_by_file was sent; it only changes the text rendering this client ignores")
		}
	})

	t.Run("empty result is not an error", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		f.setTool(toolCodeSymbol, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
			return textResult("No symbols found matching the pattern."), nil
		})
		c := newTestClient(t, f, nil)
		got, err := c.FindSymbol(context.Background(), "p", "nothing", FindSymbolOptions{})
		if err != nil || got != nil {
			t.Fatalf("FindSymbol() = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		c := newTestClient(t, f, nil)
		if _, err := c.FindSymbol(context.Background(), "p", "  ", FindSymbolOptions{}); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("error = %v, want ErrInvalidOptions", err)
		}
	})
}

func TestRelatedFiles(t *testing.T) {
	t.Parallel()

	t.Run("parses recorded payload", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		f.setTool(toolCodeRelated, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
			return textResult(relatedTOON), nil
		})
		c := newTestClient(t, f, nil)

		got, err := c.RelatedFiles(context.Background(), "p", "internal/pando/search.go", RelatedFilesOptions{Limit: 2})
		if err != nil {
			t.Fatalf("RelatedFiles() error = %v", err)
		}
		if got.Truncated || len(got.Files) != 2 {
			t.Fatalf("got %#v", got)
		}
		first := got.Files[0]
		if first.FilePath != "internal/vault/vault_test.go" || first.Score != 1.86 ||
			len(first.Reasons) != 2 || first.Reasons[1] != "imports" {
			t.Errorf("Files[0] = %#v", first)
		}
		args := f.callsFor(toolCodeRelated)[0]
		if args["file"] != "internal/pando/search.go" || args["limit"] != float64(2) {
			t.Errorf("args = %v", args)
		}
	})

	t.Run("empty result is not an error", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		f.setTool(toolCodeRelated, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
			return textResult(`No related files found for "a.go" (no resolved imports or call coupling in the indexed graph).`), nil
		})
		c := newTestClient(t, f, nil)
		got, err := c.RelatedFiles(context.Background(), "p", "a.go", RelatedFilesOptions{})
		if err != nil || len(got.Files) != 0 {
			t.Fatalf("RelatedFiles() = %#v, %v; want empty, nil", got, err)
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		t.Parallel()
		f := newFakePando(t, "")
		c := newTestClient(t, f, nil)
		if _, err := c.RelatedFiles(context.Background(), "p", "", RelatedFilesOptions{}); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("error = %v, want ErrInvalidOptions", err)
		}
	})
}

// The three graph wrappers report an absent Pando as unavailable and a failing
// tool as something else, so the impact resolver degrades only when it should.
func TestGraphWrappersUnavailability(t *testing.T) {
	t.Parallel()

	calls := map[string]func(*Client) error{
		"ImpactAnalysis": func(c *Client) error {
			_, err := c.ImpactAnalysis(context.Background(), "p", []string{"x"}, ImpactOptions{})
			return err
		},
		"FindSymbol": func(c *Client) error {
			_, err := c.FindSymbol(context.Background(), "p", "x", FindSymbolOptions{})
			return err
		},
		"RelatedFiles": func(c *Client) error {
			_, err := c.RelatedFiles(context.Background(), "p", "a.go", RelatedFilesOptions{})
			return err
		},
	}

	notConfigured, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	unreachable, err := New(Options{MCPURL: "http://127.0.0.1:1/mcp", Timeout: 5 * timeUnit})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	failing := newFakePando(t, "")
	for _, tool := range []string{toolCodeImpact, toolCodeSymbol, toolCodeRelated} {
		failing.setTool(tool, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
			return errorResult("project p is not indexed"), nil
		})
	}
	failingClient := newTestClient(t, failing, nil)

	for name, call := range calls {
		t.Run(name+"/not configured", func(t *testing.T) {
			err := call(notConfigured)
			if !errors.Is(err, ErrNotConfigured) || !IsUnavailable(err) {
				t.Fatalf("error = %v, want ErrNotConfigured classified unavailable", err)
			}
		})
		t.Run(name+"/unreachable", func(t *testing.T) {
			err := call(unreachable)
			if !errors.Is(err, ErrUnreachable) || !IsUnavailable(err) {
				t.Fatalf("error = %v, want ErrUnreachable classified unavailable", err)
			}
		})
		t.Run(name+"/tool failed", func(t *testing.T) {
			err := call(failingClient)
			if !errors.Is(err, ErrToolFailed) || IsUnavailable(err) {
				t.Fatalf("error = %v, want ErrToolFailed not classified unavailable", err)
			}
			if !contains(err.Error(), "not indexed") {
				t.Errorf("the tool's own message was lost: %v", err)
			}
		})
	}
}

func TestIsUnavailable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{ErrNotConfigured, true},
		{ErrUnreachable, true},
		{ErrUnauthorized, true},
		{ErrTimeout, true},
		{ErrToolFailed, false},
		{ErrInvalidOptions, false},
		{errors.New("other"), false},
	}
	for _, tt := range tests {
		t.Run(errString(tt.err), func(t *testing.T) {
			if got := IsUnavailable(tt.err); got != tt.want {
				t.Errorf("IsUnavailable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}
