package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Bundle $type discriminators as YouTrack spells them in a
// customFieldSettings response.
const (
	BundleTypeEnum    = "EnumBundle"
	BundleTypeState   = "StateBundle"
	BundleTypeVersion = "VersionBundle"
	BundleTypeOwned   = "OwnedBundle"
	BundleTypeBuild   = "BuildBundle"
	BundleTypeUser    = "UserBundle"
	BundleTypeGroup   = "GroupBundle"
)

// Field selectors for the bundle value endpoints. Each kind carries different
// keys, and asking for a key a kind does not have is an error on the YouTrack
// side, so the selector is chosen per kind rather than shared.
const (
	// BundleValueFields is the shape every element-shaped bundle has.
	BundleValueFields = "id,name,description,ordinal,archived,localizedName,color(id,background,foreground)"
	// EnumValueFields is one value of an enum bundle.
	EnumValueFields = BundleValueFields
	// StateValueFields is one value of a state bundle. isResolved is what tells
	// a "done" status from an open one, so a status map can be proposed.
	StateValueFields = BundleValueFields + ",isResolved"
	// OwnedValueFields is one value of an owned-field bundle, which carries an
	// owner on top of the common shape.
	OwnedValueFields = BundleValueFields + ",owner(id,login,fullName,email)"
	// BuildValueFields is one value of a build bundle.
	BuildValueFields = BundleValueFields + ",assembleDate"
	// UserValueFields is one user of a user bundle. Users have no ordinal and
	// no color.
	UserValueFields = "id,login,fullName,email,name"
	// GroupValueFields is one group of a group bundle.
	GroupValueFields = "id,name,description"
)

// BundleColor is the color YouTrack renders an enum or state value in. The
// values are CSS colors, and a settings screen can use them as they are.
type BundleColor struct {
	ID         string `json:"id"`
	Background string `json:"background"`
	Foreground string `json:"foreground"`
}

// BundleValue is one allowed value of a custom field, whatever kind of bundle
// backs it. The keys a given kind does not carry stay at their zero value:
// only enum and state values have a color, only state values have IsResolved,
// only version values have ReleaseDate and Released, only owned values have an
// Owner, and a user value carries Login, FullName and Email instead of Name.
// IsResolved is a pointer so "the server did not send it" is distinguishable
// from "false".
type BundleValue struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	LocalizedName string      `json:"localizedName"`
	Ordinal       int         `json:"ordinal"`
	Archived      bool        `json:"archived"`
	Color         BundleColor `json:"color"`
	IsResolved    *bool       `json:"isResolved"`
	Released      bool        `json:"released"`
	ReleaseDate   Millis      `json:"releaseDate"`
	AssembleDate  Millis      `json:"assembleDate"`
	Owner         User        `json:"owner"`
	Login         string      `json:"login"`
	FullName      string      `json:"fullName"`
	Email         string      `json:"email"`
	Type          string      `json:"$type"`
}

// Label is the string to show for the value: its localized name when the
// instance has one, then its name, then the login of a user value, then its id.
// A settings screen should map on Name and display Label.
func (v BundleValue) Label() string {
	for _, candidate := range []string{v.LocalizedName, v.Name, v.FullName, v.Login} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return v.ID
}

// Resolved reports whether a state value marks an issue as done, and whether
// the instance said so at all. Only state bundles carry the flag.
func (v BundleValue) Resolved() (resolved, known bool) {
	if v.IsResolved == nil {
		return false, false
	}
	return *v.IsResolved, true
}

// FieldValues is one custom field of a project together with the values it
// allows, which is what a field-mapping screen needs to render a row per field
// and a picker per value. Bundled is false for a field whose values are not
// enumerable — a text, date, integer or period field — and Values is then
// empty without that being an error. Warnings carries anything that was
// tolerated rather than failed on, so a caller can surface it without losing
// the rest of the response.
type FieldValues struct {
	SettingID      string
	Name           string
	FieldType      string
	BundleID       string
	BundleType     string
	Bundled        bool
	CanBeEmpty     bool
	EmptyFieldText string
	Values         []BundleValue
	Warnings       []string
}

// bundleEndpoint is the path segment and the field selector of one bundle kind.
type bundleEndpoint struct {
	segment  string
	resource string
	fields   string
}

// bundleEndpoints maps a bundle $type onto its values endpoint. A user bundle
// is the odd one out: its members hang off aggregatedUsers, not values.
var bundleEndpoints = map[string]bundleEndpoint{
	BundleTypeEnum:    {segment: "enum", resource: "values", fields: EnumValueFields},
	BundleTypeState:   {segment: "state", resource: "values", fields: StateValueFields},
	BundleTypeVersion: {segment: "version", resource: "values", fields: VersionValueFields},
	BundleTypeOwned:   {segment: "ownedField", resource: "values", fields: OwnedValueFields},
	BundleTypeBuild:   {segment: "build", resource: "values", fields: BuildValueFields},
	BundleTypeUser:    {segment: "user", resource: "aggregatedUsers", fields: UserValueFields},
	BundleTypeGroup:   {segment: "group", resource: "values", fields: GroupValueFields},
}

// SupportedBundleType reports whether this client knows the values endpoint of
// a bundle $type. A caller that wants to explain why a field has no values can
// ask before calling BundleValues.
func SupportedBundleType(bundleType string) bool {
	_, ok := bundleEndpoints[strings.TrimSpace(bundleType)]
	return ok
}

// BundleValues reads the values a project custom field allows, choosing the
// endpoint from the bundle kind: enum, state, version, ownedField, build, user
// or group. A field with no bundle, or with a bundle kind this client does not
// know, is not an error: the returned FieldValues has Bundled false, no values
// and a warning saying why. Decoding is tolerant — a value carrying an
// unexpected shape is reported in Warnings and skipped instead of failing the
// whole field.
func (c *Client) BundleValues(ctx context.Context, setting CustomFieldSetting) (FieldValues, error) {
	out := FieldValues{
		SettingID:      setting.ID,
		Name:           setting.Field.Name,
		FieldType:      setting.Field.FieldType.ID,
		BundleID:       setting.Bundle.ID,
		BundleType:     setting.Bundle.Type,
		CanBeEmpty:     setting.CanBeEmpty,
		EmptyFieldText: setting.EmptyFieldText,
	}
	if setting.Bundle.ID == "" {
		out.Warnings = append(out.Warnings, fmt.Sprintf("field %q has no bundle, so it has no enumerable values", out.Name))
		return out, nil
	}
	endpoint, ok := bundleEndpoints[strings.TrimSpace(setting.Bundle.Type)]
	if !ok {
		out.Warnings = append(out.Warnings, fmt.Sprintf("field %q is backed by an unsupported bundle kind %q", out.Name, setting.Bundle.Type))
		return out, nil
	}
	out.Bundled = true

	path := "/api/admin/customFieldSettings/bundles/" + endpoint.segment + "/" +
		url.PathEscape(setting.Bundle.ID) + "/" + endpoint.resource
	var raws []json.RawMessage
	if err := c.get(ctx, path, fieldsQuery(endpoint.fields, Page{}.values()), &raws); err != nil {
		return out, err
	}
	values, warnings := decodeBundleValues(raws, out.Name)
	out.Values = values
	out.Warnings = append(out.Warnings, warnings...)
	return out, nil
}

// ProjectFieldValues lists a project's custom fields together with the values
// each of them allows, which is the one call a field-mapping screen needs. The
// fields come back in the order YouTrack lists them. A bundle that cannot be
// read — a restricted one, say — leaves its field in the result with a warning
// and no values rather than failing the whole call; a rejected token still
// fails, because nothing else would succeed either.
func (c *Client) ProjectFieldValues(ctx context.Context, projectID string) ([]FieldValues, error) {
	settings, err := c.CustomFieldSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]FieldValues, 0, len(settings))
	for _, setting := range settings {
		field, err := c.BundleValues(ctx, setting)
		if err != nil {
			if errors.Is(err, ErrUnauthorized) {
				return nil, err
			}
			field.Warnings = append(field.Warnings,
				fmt.Sprintf("the values of field %q could not be read: %s", field.Name, err.Error()))
			out = append(out, field)
			continue
		}
		out = append(out, field)
	}
	return out, nil
}

// decodeBundleValues decodes the elements one by one so a single unexpected
// value cannot cost the whole bundle. An element that does not decode into the
// full shape is retried as an id-and-name pair, and only an element that is not
// even a JSON object is dropped.
func decodeBundleValues(raws []json.RawMessage, fieldName string) (values []BundleValue, warnings []string) {
	values = make([]BundleValue, 0, len(raws))
	for i, raw := range raws {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			warnings = append(warnings, fmt.Sprintf("value %d of field %q is not an object and was skipped", i, fieldName))
			continue
		}
		var value BundleValue
		if err := json.Unmarshal(trimmed, &value); err == nil {
			values = append(values, value)
			continue
		}
		var minimal struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"$type"`
		}
		if err := json.Unmarshal(trimmed, &minimal); err != nil {
			warnings = append(warnings, fmt.Sprintf("value %d of field %q could not be decoded and was skipped", i, fieldName))
			continue
		}
		warnings = append(warnings, fmt.Sprintf("value %q of field %q carried an unexpected shape; only its id and name were kept", minimal.Name, fieldName))
		values = append(values, BundleValue{ID: minimal.ID, Name: minimal.Name, Type: minimal.Type})
	}
	return values, warnings
}
