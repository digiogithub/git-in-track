package youtrack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestBundleValuesPerKind is the table over the bundle value endpoints: each
// kind has its own path segment, its own sub-resource and its own field
// selector, and each carries keys the others do not.
func TestBundleValuesPerKind(t *testing.T) {
	tests := []struct {
		name       string
		setting    CustomFieldSetting
		fixture    string
		wantPath   string
		wantFields string
		check      func(t *testing.T, field FieldValues)
	}{
		{
			name: "enum values carry an ordinal and a color",
			setting: CustomFieldSetting{
				ID:     "110-0",
				Field:  CustomFieldDefinition{Name: "Type", FieldType: FieldType{ID: "enum[1]"}},
				Bundle: Bundle{ID: "70-0", Type: BundleTypeEnum},
			},
			fixture:    "enum_bundle_values.json",
			wantPath:   "/api/admin/customFieldSettings/bundles/enum/70-0/values",
			wantFields: EnumValueFields,
			check: func(t *testing.T, field FieldValues) {
				if len(field.Values) != 2 {
					t.Fatalf("%d values, want 2", len(field.Values))
				}
				if field.Values[0].Name != "Bug" || field.Values[0].Ordinal != 0 {
					t.Fatalf("value[0] = %+v", field.Values[0])
				}
				if field.Values[0].Color.Background != "#E6F5FF" {
					t.Fatalf("color = %+v", field.Values[0].Color)
				}
				if !field.Values[1].Archived {
					t.Fatalf("value[1] should be archived: %+v", field.Values[1])
				}
				// A localized name wins over the raw name for display, while
				// the mapping key stays the raw name.
				if got := field.Values[1].Label(); got != "Funcionalidad" {
					t.Fatalf("label = %q", got)
				}
				if _, known := field.Values[0].Resolved(); known {
					t.Fatalf("an enum value has no isResolved flag")
				}
			},
		},
		{
			name: "state values carry isResolved",
			setting: CustomFieldSetting{
				Field:  CustomFieldDefinition{Name: "State", FieldType: FieldType{ID: "state[1]"}},
				Bundle: Bundle{ID: "72-x", Type: BundleTypeState},
			},
			fixture:    "state_bundle_values.json",
			wantPath:   "/api/admin/customFieldSettings/bundles/state/72-x/values",
			wantFields: StateValueFields,
			check: func(t *testing.T, field FieldValues) {
				if len(field.Values) != 3 {
					t.Fatalf("%d values, want 3", len(field.Values))
				}
				// The value a person must be able to map onto a local status.
				if field.Values[1].Name != "In Progress" {
					t.Fatalf("value[1] = %+v", field.Values[1])
				}
				resolved, known := field.Values[1].Resolved()
				if !known || resolved {
					t.Fatalf("In Progress: resolved = %v, known = %v", resolved, known)
				}
				resolved, known = field.Values[2].Resolved()
				if !known || !resolved {
					t.Fatalf("Fixed: resolved = %v, known = %v", resolved, known)
				}
			},
		},
		{
			name: "version values carry the release date",
			setting: CustomFieldSetting{
				Field:  CustomFieldDefinition{Name: "Fix versions", FieldType: FieldType{ID: "version[*]"}},
				Bundle: Bundle{ID: "75-2", Type: BundleTypeVersion},
			},
			fixture:    "version_bundle_values.json",
			wantPath:   "/api/admin/customFieldSettings/bundles/version/75-2/values",
			wantFields: VersionValueFields,
			check: func(t *testing.T, field FieldValues) {
				if len(field.Values) != 2 || !field.Values[0].Released {
					t.Fatalf("values = %+v", field.Values)
				}
				if field.Values[0].ReleaseDate.IsZero() || !field.Values[1].ReleaseDate.IsZero() {
					t.Fatalf("release dates = %v / %v", field.Values[0].ReleaseDate, field.Values[1].ReleaseDate)
				}
			},
		},
		{
			name: "a user bundle reads its aggregated users",
			setting: CustomFieldSetting{
				Field:  CustomFieldDefinition{Name: "Assignee", FieldType: FieldType{ID: "user[1]"}},
				Bundle: Bundle{ID: "80-1", Type: BundleTypeUser},
			},
			fixture:    "user_bundle_values.json",
			wantPath:   "/api/admin/customFieldSettings/bundles/user/80-1/aggregatedUsers",
			wantFields: UserValueFields,
			check: func(t *testing.T, field FieldValues) {
				if len(field.Values) != 2 {
					t.Fatalf("%d values, want 2", len(field.Values))
				}
				if field.Values[0].Login != "jose" || field.Values[0].Email != "jose@example.com" {
					t.Fatalf("value[0] = %+v", field.Values[0])
				}
				// A user has no name, so the label falls back to the full name.
				if got := field.Values[1].Label(); got != "Ana Ruiz" {
					t.Fatalf("label = %q", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tc.fixture)
			})
			client := newTestClient(t, srv.URL, newFakeClock(), nil)

			field, err := client.BundleValues(context.Background(), tc.setting)
			if err != nil {
				t.Fatalf("BundleValues: %v", err)
			}
			req := rec.all()[0]
			if req.URL.Path != tc.wantPath {
				t.Fatalf("path = %q, want %q", req.URL.Path, tc.wantPath)
			}
			if got := req.URL.Query().Get("fields"); got != tc.wantFields {
				t.Fatalf("fields = %q, want %q", got, tc.wantFields)
			}
			if got := req.URL.Query().Get("$top"); got != "100" {
				t.Fatalf("$top = %q: bundle values are paged like every other list", got)
			}
			if !field.Bundled {
				t.Fatalf("field %+v should be bundle-backed", field)
			}
			if field.Name != tc.setting.Field.Name || field.FieldType != tc.setting.Field.FieldType.ID {
				t.Fatalf("field = %+v", field)
			}
			if len(field.Warnings) != 0 {
				t.Fatalf("warnings = %v, want none", field.Warnings)
			}
			tc.check(t, field)
		})
	}
}

// TestBundleValuesToleratesUnexpectedValues covers the defensive half: the
// bundle endpoints are documented from API knowledge, not from a verified
// client, so one odd value must cost that value and not the whole field.
func TestBundleValuesToleratesUnexpectedValues(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "state_bundle_values_odd.json")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	field, err := client.BundleValues(context.Background(), CustomFieldSetting{
		Field:  CustomFieldDefinition{Name: "State"},
		Bundle: Bundle{ID: "72-x", Type: BundleTypeState},
	})
	if err != nil {
		t.Fatalf("BundleValues: %v", err)
	}
	// Four elements in, three values out: the string element is dropped, the
	// one with the wrong types degrades to its id and name.
	var names []string
	for _, value := range field.Values {
		names = append(names, value.Name)
	}
	if strings.Join(names, ",") != "Open,Weird,Done" {
		t.Fatalf("values = %v", names)
	}
	if len(field.Warnings) != 2 {
		t.Fatalf("warnings = %v, want two", field.Warnings)
	}
	if resolved, known := field.Values[2].Resolved(); !known || !resolved {
		t.Fatalf("Done should still decode fully: %+v", field.Values[2])
	}
}

// TestBundleValuesWithoutABundle keeps a text or date field from looking like a
// failure: it simply has no enumerable values.
func TestBundleValuesWithoutABundle(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should have been made")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	tests := []struct {
		name    string
		setting CustomFieldSetting
	}{
		{
			name: "no bundle at all",
			setting: CustomFieldSetting{
				Field: CustomFieldDefinition{Name: "Due Date", FieldType: FieldType{ID: "date"}},
			},
		},
		{
			name: "a bundle kind this client does not know",
			setting: CustomFieldSetting{
				Field:  CustomFieldDefinition{Name: "Something", FieldType: FieldType{ID: "something[1]"}},
				Bundle: Bundle{ID: "99-9", Type: "SomethingBundle"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			field, err := client.BundleValues(context.Background(), tc.setting)
			if err != nil {
				t.Fatalf("BundleValues: %v", err)
			}
			if field.Bundled || len(field.Values) != 0 {
				t.Fatalf("field = %+v", field)
			}
			if len(field.Warnings) != 1 {
				t.Fatalf("warnings = %v, want one explaining why", field.Warnings)
			}
		})
	}
	if rec.count() != 0 {
		t.Fatalf("%d requests were made, want none", rec.count())
	}
	if SupportedBundleType("SomethingBundle") || !SupportedBundleType(BundleTypeState) {
		t.Fatal("SupportedBundleType disagrees with BundleValues")
	}
}

// TestProjectFieldValuesProjectsFieldsAndValuesTogether covers the one call the
// settings screen needs: the project's fields, each with the values it allows.
func TestProjectFieldValuesProjectsFieldsAndValuesTogether(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/admin/projects/0-1/customFieldSettings":
			writeJSON(t, w, "custom_field_settings.json")
		case "/api/admin/customFieldSettings/bundles/enum/70-0/values":
			writeJSON(t, w, "enum_bundle_values.json")
		case "/api/admin/customFieldSettings/bundles/version/75-2/values":
			writeJSON(t, w, "version_bundle_values.json")
		default:
			http.NotFound(w, r)
		}
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	fields, err := client.ProjectFieldValues(context.Background(), "0-1")
	if err != nil {
		t.Fatalf("ProjectFieldValues: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("%d fields, want 2", len(fields))
	}
	if fields[0].Name != "Type" || len(fields[0].Values) != 2 || fields[0].Values[0].Name != "Bug" {
		t.Fatalf("field[0] = %+v", fields[0])
	}
	if fields[0].EmptyFieldText != "No type" || fields[0].CanBeEmpty {
		t.Fatalf("field[0] = %+v", fields[0])
	}
	if fields[1].Name != "Fix versions" || fields[1].BundleType != BundleTypeVersion {
		t.Fatalf("field[1] = %+v", fields[1])
	}
	if len(fields[1].Values) != 2 || !fields[1].Values[0].Released {
		t.Fatalf("field[1] values = %+v", fields[1].Values)
	}
	// One listing plus one bundle read per bundle-backed field.
	if rec.count() != 3 {
		t.Fatalf("%d requests, want 3", rec.count())
	}
}

// TestProjectFieldValuesSurvivesOneUnreadableBundle keeps a restricted bundle
// from emptying the whole settings screen, while a rejected token still fails.
func TestProjectFieldValuesSurvivesOneUnreadableBundle(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		wantErr  error
		wantWarn bool
	}{
		{name: "a forbidden bundle degrades to a warning", status: http.StatusForbidden, wantWarn: true},
		{name: "a rejected token fails the call", status: http.StatusUnauthorized, wantErr: ErrUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/admin/projects/0-1/customFieldSettings":
					writeJSON(t, w, "custom_field_settings.json")
				case "/api/admin/customFieldSettings/bundles/enum/70-0/values":
					w.WriteHeader(tc.status)
					// The body echoes the credential; the warning must not.
					_, _ = io.WriteString(w, "denied for Bearer "+testToken)
				default:
					writeJSON(t, w, "version_bundle_values.json")
				}
			})
			client := newTestClient(t, srv.URL, newFakeClock(), func(o *Options) { o.MaxRetries = -1 })

			fields, err := client.ProjectFieldValues(context.Background(), "0-1")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if strings.Contains(err.Error(), testToken) {
					t.Fatalf("the error carries the token: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ProjectFieldValues: %v", err)
			}
			if len(fields) != 2 {
				t.Fatalf("%d fields, want both of them", len(fields))
			}
			if len(fields[0].Warnings) == 0 || len(fields[0].Values) != 0 {
				t.Fatalf("field[0] = %+v", fields[0])
			}
			if strings.Contains(strings.Join(fields[0].Warnings, " "), testToken) {
				t.Fatalf("the warning carries the token: %v", fields[0].Warnings)
			}
			// The field that could be read is untouched by its neighbor.
			if len(fields[1].Values) != 2 {
				t.Fatalf("field[1] = %+v", fields[1])
			}
		})
	}
}
