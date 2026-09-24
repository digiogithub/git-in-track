package pando

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Pando's code property-graph tools. They read the call and import edges Pando
// builds while it indexes a code project, which is what an impact resolver
// needs on top of the ranked search in search.go.
const (
	toolCodeImpact  = "code_impact_analysis"
	toolCodeSymbol  = "code_find_symbol"
	toolCodeRelated = "code_related_files"
)

// projectOrDefault resolves an empty project id to Options.ProjectID, the same
// rule SearchCode applies.
func (c *Client) projectOrDefault(projectID string) (string, error) {
	if projectID == "" {
		projectID = c.opts.ProjectID
	}
	if projectID == "" {
		return "", fmt.Errorf("%w: no code project id", ErrNotConfigured)
	}
	return projectID, nil
}

type impactCallerWire struct {
	Name       string `json:"name"`
	NamePath   string `json:"name_path"`
	FilePath   string `json:"file_path"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	SymbolType string `json:"symbol_type"`
	Depth      int    `json:"depth"`
}

type impactResultWire struct {
	Symbol    string             `json:"symbol"`
	Count     int                `json:"count"`
	Truncated bool               `json:"truncated"`
	Callers   []impactCallerWire `json:"callers"`
}

// ImpactAnalysis runs code_impact_analysis once per distinct symbol name and
// merges the callers. An empty projectID falls back to Options.ProjectID.
//
// Pando's tool takes one symbol per call and resolves it by name over its call
// edges, so the answer is approximate where unrelated symbols share a name;
// pair it with FindSymbol to pin a definition down. A symbol nothing calls is
// not an error: it contributes no callers.
func (c *Client) ImpactAnalysis(ctx context.Context, projectID string, symbols []string, o ImpactOptions) (ImpactResult, error) {
	projectID, err := c.projectOrDefault(projectID)
	if err != nil {
		return ImpactResult{}, err
	}
	var names []string
	seen := map[string]bool{}
	for _, s := range symbols {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		names = append(names, s)
	}
	if len(names) == 0 {
		return ImpactResult{}, fmt.Errorf("%w: no symbol to analyze", ErrInvalidOptions)
	}

	var out ImpactResult
	for _, name := range names {
		args := map[string]any{"project_id": projectID, "symbol": name}
		if o.Depth > 0 {
			args["depth"] = o.Depth
		}
		if o.Limit > 0 {
			args["limit"] = o.Limit
		}
		res, err := c.call(ctx, toolCodeImpact, args)
		if err != nil {
			return ImpactResult{}, err
		}
		var wire impactResultWire
		ok, err := res.decodeInto(&wire)
		if err != nil {
			return ImpactResult{}, err
		}
		if !ok {
			continue
		}
		out.Truncated = out.Truncated || wire.Truncated
		for _, w := range wire.Callers {
			out.Callers = append(out.Callers, ImpactCaller{
				Symbol:     name,
				Name:       w.Name,
				NamePath:   w.NamePath,
				SymbolType: w.SymbolType,
				FilePath:   w.FilePath,
				StartLine:  w.StartLine,
				EndLine:    w.EndLine,
				Depth:      w.Depth,
			})
		}
	}
	return out, nil
}

type symbolWire struct {
	SymbolType string `json:"symbol_type"`
	Name       string `json:"name"`
	NamePath   string `json:"name_path"`
	FilePath   string `json:"file_path"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Signature  string `json:"signature"`
}

type symbolsWire struct {
	Count   int          `json:"count"`
	Total   int          `json:"total"`
	Offset  int          `json:"offset"`
	Symbols []symbolWire `json:"symbols"`
}

// FindSymbol runs code_find_symbol with name as the name-path pattern:
// "/Type/method" matches exactly, "Type/method" by suffix and "method" by simple
// name. An empty projectID falls back to Options.ProjectID. No match returns a
// nil slice and a nil error.
func (c *Client) FindSymbol(ctx context.Context, projectID, name string, o FindSymbolOptions) ([]Symbol, error) {
	projectID, err := c.projectOrDefault(projectID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w: an empty symbol name", ErrInvalidOptions)
	}
	args := map[string]any{"project_id": projectID, "name_path_pattern": name}
	if o.RelativePath != "" {
		args["relative_path"] = o.RelativePath
	}
	if len(o.SymbolTypes) > 0 {
		args["symbol_types"] = o.SymbolTypes
	}
	if len(o.Languages) > 0 {
		args["languages"] = o.Languages
	}
	if o.Substring {
		args["substring_matching"] = true
	}
	if o.Limit > 0 {
		args["limit"] = o.Limit
	}
	if o.Offset > 0 {
		args["offset"] = o.Offset
	}

	res, err := c.call(ctx, toolCodeSymbol, args)
	if err != nil {
		return nil, err
	}
	var wire symbolsWire
	ok, err := res.decodeInto(&wire)
	if err != nil || !ok {
		return nil, err
	}
	out := make([]Symbol, 0, len(wire.Symbols))
	for i, s := range wire.Symbols {
		out = append(out, Symbol{
			Name:       s.Name,
			NamePath:   s.NamePath,
			SymbolType: s.SymbolType,
			FilePath:   s.FilePath,
			StartLine:  s.StartLine,
			EndLine:    s.EndLine,
			Signature:  s.Signature,
			Rank:       wire.Offset + i + 1,
		})
	}
	return out, nil
}

type relatedFileWire struct {
	FilePath string   `json:"file_path"`
	Score    float64  `json:"score"`
	Reasons  []string `json:"reasons"`
}

type relatedFilesWire struct {
	File      string            `json:"file"`
	Count     int               `json:"count"`
	Truncated bool              `json:"truncated"`
	Related   []relatedFileWire `json:"related"`
}

// RelatedFiles runs code_related_files for one file, given relative to the
// indexed project root. An empty projectID falls back to Options.ProjectID. A
// file with no coupling returns an empty result and a nil error.
func (c *Client) RelatedFiles(ctx context.Context, projectID, path string, o RelatedFilesOptions) (RelatedFilesResult, error) {
	projectID, err := c.projectOrDefault(projectID)
	if err != nil {
		return RelatedFilesResult{}, err
	}
	if strings.TrimSpace(path) == "" {
		return RelatedFilesResult{}, fmt.Errorf("%w: an empty file path", ErrInvalidOptions)
	}
	args := map[string]any{"project_id": projectID, "file": path}
	if o.Limit > 0 {
		args["limit"] = o.Limit
	}
	res, err := c.call(ctx, toolCodeRelated, args)
	if err != nil {
		return RelatedFilesResult{}, err
	}
	var wire relatedFilesWire
	ok, err := res.decodeInto(&wire)
	if err != nil || !ok {
		return RelatedFilesResult{}, err
	}
	out := RelatedFilesResult{Truncated: wire.Truncated, Files: make([]RelatedFile, 0, len(wire.Related))}
	for _, r := range wire.Related {
		out.Files = append(out.Files, RelatedFile(r))
	}
	return out, nil
}

// decodeInto fills into from the tool result, preferring the JSON document in
// structuredContent.metadata (code_find_symbol puts its machine-readable result
// there) and falling back to the TOON text content (the graph tools render
// theirs as TOON). ok is false for Pando's "No ..." sentence, which is an empty
// result rather than a malformed one.
func (r *toolResult) decodeInto(into any) (ok bool, err error) {
	if len(r.metadata) > 0 {
		if err := json.Unmarshal(r.metadata, into); err != nil {
			return false, fmt.Errorf("%w: %s returned metadata this client cannot read: %w",
				ErrUnreachable, r.tool, err)
		}
		return true, nil
	}
	obj, ok, err := r.object()
	if err != nil || !ok {
		return false, err
	}
	if err := remarshal(r.tool, obj, into); err != nil {
		return false, err
	}
	return true, nil
}
