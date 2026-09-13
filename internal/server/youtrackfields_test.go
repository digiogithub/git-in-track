package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
)

// The custom-field discovery endpoint and the per-value field map, tasks
// GIT-T-0131 and GIT-T-0136.

// fieldValueBody is one value of one field as the endpoint reports it.
type fieldValueBody struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	Ordinal    int    `json:"ordinal"`
	Archived   bool   `json:"archived"`
	Background string `json:"background"`
	IsResolved *bool  `json:"isResolved"`
	Released   bool   `json:"released"`
}

// fieldsBody is the documented shape of GET /api/v1/youtrack/fields.
type fieldsBody struct {
	Project string `json:"project"`
	Fields  []struct {
		ID         string           `json:"id"`
		Name       string           `json:"name"`
		Type       string           `json:"type"`
		BundleType string           `json:"bundleType"`
		Bundled    bool             `json:"bundled"`
		CanBeEmpty bool             `json:"canBeEmpty"`
		Values     []fieldValueBody `json:"values"`
		Warnings   []string         `json:"warnings"`
	} `json:"fields"`
	Total               int      `json:"total"`
	GintrackFields      []string `json:"gintrackFields"`
	ValueMappableFields []string `json:"valueMappableFields"`
}

// newBundleStub starts a fake instance whose project declares a state bundle, an
// enum bundle, a plain text field and a field backed by a bundle kind this
// build does not resolve — the four cases the projection has to tell apart.
func newBundleStub(t *testing.T) *httptest.Server {
	t.Helper()

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/users/me"):
			_, _ = w.Write([]byte(`{"id":"1-1","login":"jose","fullName":"Jose"}`))
		case strings.Contains(r.URL.Path, "/bundles/state/b1/values"):
			_, _ = w.Write([]byte(`[
			  {"id":"v1","name":"Open","ordinal":0,"isResolved":false,"color":{"background":"#eee"}},
			  {"id":"v2","name":"In Progress","localizedName":"En curso","ordinal":1,"isResolved":false},
			  {"id":"v3","name":"Fixed","ordinal":2,"isResolved":true,"archived":true}
			]`))
		case strings.Contains(r.URL.Path, "/bundles/enum/b2/values"):
			_, _ = w.Write([]byte(`[{"id":"p1","name":"Critical","ordinal":0},{"id":"p2","name":"Normal","ordinal":1}]`))
		// The bundle paths are matched first: they are themselves under
		// /customFieldSettings, so the settings case would swallow them.
		case strings.Contains(r.URL.Path, "/customFieldSettings"):
			_, _ = w.Write([]byte(`[
			  {"id":"f1","field":{"id":"d1","name":"State","fieldType":{"id":"state[1]"}},
			   "bundle":{"id":"b1","$type":"StateBundle"},"canBeEmpty":false},
			  {"id":"f2","field":{"id":"d2","name":"Priority","fieldType":{"id":"enum[1]"}},
			   "bundle":{"id":"b2","$type":"EnumBundle"},"canBeEmpty":true},
			  {"id":"f3","field":{"id":"d3","name":"Customer","fieldType":{"id":"string"}},
			   "canBeEmpty":true},
			  {"id":"f4","field":{"id":"d4","name":"Weird","fieldType":{"id":"weird[1]"}},
			   "bundle":{"id":"b4","$type":"WeirdBundle"},"canBeEmpty":true}
			]`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(stub.Close)
	return stub
}

// TestYouTrackFieldsProjectsTheValues covers the acceptance criteria of
// GIT-T-0131: every custom field with its type, the values of the bundle-backed
// ones, `isResolved` on state values, and a field whose values could not be
// resolved left in place with a warning rather than dropped.
func TestYouTrackFieldsProjectsTheValues(t *testing.T) {
	t.Parallel()

	stub := newBundleStub(t)
	s, _, _ := newYouTrackServer(t, &config.YouTrackLink{URL: stub.URL, Project: "ACME"}, ytToken, false)

	var body fieldsBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/fields"}), http.StatusOK, &body)

	if body.Project != "ACME" || body.Total != 4 {
		t.Fatalf("fields = %+v", body)
	}
	byName := map[string]int{}
	for i, field := range body.Fields {
		byName[field.Name] = i
	}
	for _, want := range []string{"State", "Priority", "Customer", "Weird"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("the projection dropped field %q: %+v", want, body.Fields)
		}
	}

	state := body.Fields[byName["State"]]
	if !state.Bundled || state.Type != "state[1]" || state.BundleType != "StateBundle" {
		t.Errorf("State = %+v", state)
	}
	if len(state.Values) != 3 {
		t.Fatalf("State carries %d values, want 3", len(state.Values))
	}
	if state.Values[1].Name != "In Progress" || state.Values[1].Label != "En curso" {
		t.Errorf("the mappable key is the name and the displayed one the label: %+v", state.Values[1])
	}
	for _, value := range state.Values {
		if value.IsResolved == nil {
			t.Errorf("%s: isResolved is absent, so a known flag was lost", value.Name)
		}
	}
	if fixed := state.Values[2]; !*fixed.IsResolved || !fixed.Archived {
		t.Errorf("Fixed = %+v, want resolved and archived", fixed)
	}

	priority := body.Fields[byName["Priority"]]
	if !priority.Bundled || len(priority.Values) != 2 {
		t.Errorf("Priority = %+v", priority)
	}
	if priority.Values[0].IsResolved != nil {
		t.Error("an enum value must not claim to know whether it resolves an issue")
	}

	text := body.Fields[byName["Customer"]]
	if text.Bundled || len(text.Values) != 0 {
		t.Errorf("a text field reported values: %+v", text)
	}

	weird := body.Fields[byName["Weird"]]
	if weird.Bundled || len(weird.Values) != 0 {
		t.Errorf("an unknown bundle kind reported values: %+v", weird)
	}
	if len(weird.Warnings) == 0 {
		t.Error("an unresolvable bundle must say why it is empty")
	}

	if len(body.GintrackFields) != len(config.FieldMapKeys) {
		t.Errorf("gintrackFields = %v", body.GintrackFields)
	}
	if strings.Join(body.ValueMappableFields, ",") != strings.Join(config.FieldMapValueKeys, ",") {
		t.Errorf("valueMappableFields = %v, want %v", body.ValueMappableFields, config.FieldMapValueKeys)
	}
}

// TestYouTrackSettingsPatchPersistsAValueMap covers GIT-T-0136 through the
// patch handler: a nested field map round-trips through project.yaml and comes
// back in the same shape, with `persisted` reported the way the git settings
// report it.
func TestYouTrackSettingsPatchPersistsAValueMap(t *testing.T) {
	t.Parallel()

	s, root, _ := newYouTrackServer(t, nil, "", true)
	rec := send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/youtrack/settings",
		body: map[string]any{
			"url":     "https://yt.example.com/youtrack",
			"project": "ACME",
			"fieldMap": map[string]any{
				"status":   map[string]any{"field": "State", "values": map[string]string{"In Progress": "in_progress"}},
				"priority": "Priority",
			},
			"token": ytToken,
		},
	})
	var body ytSettingsBody
	decode(t, rec, http.StatusOK, &body)

	if body.FieldMap["status"].Field != "State" {
		t.Fatalf("fieldMap = %+v", body.FieldMap)
	}
	if body.FieldMap.ValuesFor("status")["In Progress"] != "in_progress" {
		t.Errorf("the value map did not come back: %+v", body.FieldMap)
	}
	if body.FieldMap["priority"].Field != "Priority" {
		t.Errorf("the flat entry did not survive: %+v", body.FieldMap)
	}
	if !body.Persisted {
		t.Error("persisted = false, but the server was given a configuration file")
	}

	link, err := config.LoadYouTrackLink(ytProjectYAML(root))
	if err != nil {
		t.Fatalf("reload project.yaml: %v", err)
	}
	if link.FieldMap.ValuesFor("status")["In Progress"] != "in_progress" {
		t.Errorf("project.yaml does not hold the value map: %+v", link.FieldMap)
	}
}

// TestYouTrackSettingsPatchRefusesABadValueMap covers the refusal: a value map
// on a field whose values are not enumerable is rejected with a field-level
// error, before anything is written.
func TestYouTrackSettingsPatchRefusesABadValueMap(t *testing.T) {
	t.Parallel()

	s, root, _ := newYouTrackServer(t, nil, "", true)
	rec := send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/youtrack/settings",
		body: map[string]any{
			"url":     "https://yt.example.com/youtrack",
			"project": "ACME",
			"fieldMap": map[string]any{
				"assignee": map[string]any{"field": "Assignee", "values": map[string]string{"jose": "jose"}},
			},
		},
	})
	if rec.Code == http.StatusOK {
		t.Fatalf("a value map on `assignee` was accepted: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "field_map.assignee.values") {
		t.Errorf("the problem does not name the offending key: %s", rec.Body.String())
	}
	link, err := config.LoadYouTrackLink(ytProjectYAML(root))
	if err != nil {
		t.Fatalf("reload project.yaml: %v", err)
	}
	if link != nil {
		t.Errorf("the refused write reached project.yaml: %+v", link)
	}
}

// TestYouTrackFieldMappingOverlaysTheDefaults covers the adapter: a configured
// value map adds to the shipped translations instead of replacing them, and the
// field names still come from the flat half.
func TestYouTrackFieldMappingOverlaysTheDefaults(t *testing.T) {
	t.Parallel()

	link := &config.YouTrackLink{
		URL: "https://yt.example.com", Project: "ACME",
		FieldMap: config.FieldMap{
			"status": {Field: "Estado", Values: map[string]string{"En curso": "in_progress"}},
			"type":   {Values: map[string]string{"Spike": "task"}},
		},
	}
	got := youtrackFieldMapping(link)
	if got.StateField != "Estado" {
		t.Errorf("stateField = %q, want the configured name", got.StateField)
	}
	if got.Statuses["en curso"] != core.Status("in_progress") {
		t.Errorf("the configured value was not applied: %v", got.Statuses)
	}
	if got.Statuses["fixed"] != core.Status("done") {
		t.Error("a partial value map wiped the shipped defaults")
	}
	if got.Types["spike"] != core.TypeTask {
		t.Errorf("a value map with no field name was ignored: %v", got.Types)
	}
	if got.Types["bug"] != core.TypeTask {
		t.Error("the shipped type translations were lost")
	}
	if youtrackFieldMapping(nil).StateField != "State" {
		t.Error("an unlinked project must still get the shipped defaults")
	}
}
