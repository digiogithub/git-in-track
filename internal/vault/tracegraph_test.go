package vault

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// stubTracer is a RequirementTracer that echoes what it was asked.
type stubTracer struct {
	changes []core.TraceChange
}

func (s *stubTracer) TraceRequirement(_ context.Context, ix *core.Index, ref core.RequirementRef) (core.TracedRequirement, error) {
	if ix == nil {
		panic("no index")
	}
	return core.TracedRequirement{
		Ref: ref, Project: "DEMO",
		Code:  []core.TraceEdge{{Ref: ref, Role: core.TraceRoleCode, Path: "a.go", Symbol: "F", Sources: []core.TraceSource{core.TraceSourceMarker}}},
		Tests: []core.TraceEdge{},
		Work:  []core.TraceWork{},
	}, nil
}

func (s *stubTracer) TraceTouching(_ context.Context, _ *core.Index, changes []core.TraceChange) ([]core.TraceHit, error) {
	s.changes = changes
	return nil, nil
}

func TestTraceMethods(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		tracer   bool
		method   string
		params   map[string]any
		wantCode string // empty: success
	}{
		{"requirement unavailable without a tracer", false, "trace.requirement", map[string]any{"ref": "DEMO-SP-0001.R1"}, "unavailable"},
		{"touching unavailable without a tracer", false, "trace.touching", map[string]any{"changes": []any{}}, "unavailable"},
		{"bad ref", true, "trace.requirement", map[string]any{"ref": "DEMO-SP-0001.R01"}, "invalid_request"},
		{"unknown requirement", true, "trace.requirement", map[string]any{"ref": "DEMO-SP-0001.R9"}, "not_found"},
		{"requirement", true, "trace.requirement", map[string]any{"ref": "DEMO/DEMO-SP-0001.R2"}, ""},
		{"touching", true, "trace.touching", map[string]any{"changes": []any{
			map[string]any{"path": "a.go", "oldPath": "b.go", "lines": []any{map[string]any{"start": 3, "count": 2}}},
		}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := specVault(t)
			stub := &stubTracer{}
			if tc.tracer {
				v.SetRequirementTracer(stub)
			}
			if v.TraceAvailable() != tc.tracer {
				t.Fatalf("TraceAvailable() = %v", v.TraceAvailable())
			}
			env := rawCall(t, v, tc.method, tc.params)
			if tc.wantCode != "" {
				if env.OK || env.Error.Code != tc.wantCode {
					t.Fatalf("%s = %+v, want %s", tc.method, env, tc.wantCode)
				}
				return
			}
			if !env.OK {
				t.Fatalf("%s failed: %s %s", tc.method, env.Error.Code, env.Error.Message)
			}
			switch tc.method {
			case "trace.requirement":
				var got struct {
					Trace struct {
						Ref  string `json:"ref"`
						Code []struct {
							Path, Symbol string
							Sources      []string
						} `json:"code"`
					} `json:"trace"`
				}
				if err := json.Unmarshal(env.Result, &got); err != nil {
					t.Fatal(err)
				}
				if got.Trace.Ref != "DEMO-SP-0001.R2" || len(got.Trace.Code) != 1 || got.Trace.Code[0].Symbol != "F" {
					t.Errorf("trace.requirement = %s", env.Result)
				}
			case "trace.touching":
				if string(env.Result) != `{"hits":[]}` {
					t.Errorf("trace.touching = %s, want no hits", env.Result)
				}
				want := []core.TraceChange{{Path: "a.go", OldPath: "b.go", Lines: []core.LineSpan{{Start: 3, Count: 2}}}}
				if !reflect.DeepEqual(stub.changes, want) {
					t.Errorf("changes handed to the tracer = %+v, want %+v", stub.changes, want)
				}
			}
		})
	}
}
