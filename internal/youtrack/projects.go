package youtrack

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Field selectors for the user, project and custom-field endpoints.
const (
	// MeFields identifies the account behind the token.
	MeFields = "id,login,fullName,email,avatarUrl"
	// ProjectFields is the project shape this package requests.
	ProjectFields = "id,shortName,name,description,archived"
	// CustomFieldSettingsFields discovers which custom fields a project uses
	// and which bundle backs each of them.
	CustomFieldSettingsFields = "id,field(id,name,fieldType(id)),bundle(id,$type),canBeEmpty,emptyFieldText"
	// VersionValueFields is one value of a version bundle.
	VersionValueFields = "id,name,description,releaseDate,released,archived"
)

// Me probes the token: it returns the account it authenticates as. A 401 means
// the token was rejected, a 403 that it lacks permission, and a 404 on this
// path almost always means the base URL is missing its instance context path,
// as in https://host/youtrack. Each maps to its own sentinel error.
func (c *Client) Me(ctx context.Context) (User, error) {
	var out User
	if err := c.get(ctx, "/api/users/me", fieldsQuery(MeFields, nil), &out); err != nil {
		return User{}, err
	}
	return out, nil
}

// Projects lists or searches projects. query is a substring matched against the
// project name and short name; it is not the issue query language, so it never
// takes an "order by:" clause. Like every YouTrack list endpoint this returns a
// bare JSON array.
func (c *Client) Projects(ctx context.Context, query string, page Page) ([]Project, error) {
	params := page.values()
	if query != "" {
		params.Set("query", query)
	}
	var out []Project
	if err := c.get(ctx, "/api/admin/projects", fieldsQuery(ProjectFields, params), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AllProjects walks every page of Projects. Instances hold projects in the
// tens, so fetching them once at connect time and filtering client-side is the
// cheap way to drive a project picker.
func (c *Client) AllProjects(ctx context.Context, query string) ([]Project, error) {
	return walkAll(ctx, Page{}, func(ctx context.Context, page Page) ([]Project, error) {
		return c.Projects(ctx, query, page)
	})
}

// Project reads one project. key accepts either the short name, as in "ACME",
// or the internal id, as in "0-1".
func (c *Client) Project(ctx context.Context, key string) (Project, error) {
	if key == "" {
		return Project{}, fmt.Errorf("%w: project key is empty", ErrInvalidInput)
	}
	var out Project
	path := "/api/admin/projects/" + url.PathEscape(key)
	if err := c.get(ctx, path, fieldsQuery(ProjectFields, nil), &out); err != nil {
		return Project{}, err
	}
	return out, nil
}

// entityID matches YouTrack's internal entity ids, as in "0-17". Every write
// endpoint addresses a project by one of these; the short name is a display
// name and is rejected with "Invalid structure of entity id".
var entityID = regexp.MustCompile(`^\d+-\d+$`)

// ProjectID resolves a project onto the internal entity id the write endpoints
// insist on.
//
// The short name — the "ACME" of ACME-42 — is what a project is configured as
// and what every read here is scoped by, but `POST /api/articles` refuses it:
// the project of a new article has to be `{"id": "0-17"}`. A key that already
// is an entity id is returned untouched, so a caller may pass either and a
// resolved id can be cached and passed back in.
func (c *Client) ProjectID(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: project key is empty", ErrInvalidInput)
	}
	if entityID.MatchString(key) {
		return key, nil
	}
	// The search matches name and short name as a substring, so the exact
	// short name still has to be picked out of what comes back.
	found, err := c.AllProjects(ctx, key)
	if err != nil {
		return "", err
	}
	for _, project := range found {
		if strings.EqualFold(project.ShortName, key) {
			return project.ID, nil
		}
	}
	return "", fmt.Errorf("%w: no project has the short name %q", ErrNotFound, key)
}

// CustomFieldSettings lists the custom fields a project uses, so a caller can
// check a field exists and find its bundle before writing to it.
func (c *Client) CustomFieldSettings(ctx context.Context, projectID string) ([]CustomFieldSetting, error) {
	if projectID == "" {
		return nil, fmt.Errorf("%w: project id is empty", ErrInvalidInput)
	}
	params := Page{}.values()
	var out []CustomFieldSetting
	path := "/api/admin/projects/" + url.PathEscape(projectID) + "/customFieldSettings"
	if err := c.get(ctx, path, fieldsQuery(CustomFieldSettingsFields, params), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// VersionBundleValues lists the values of a version bundle, which is the
// closest YouTrack analog to a git-in-track milestone. Find the bundle id
// through CustomFieldSettings: it is the bundle of the "Fix versions" field.
func (c *Client) VersionBundleValues(ctx context.Context, bundleID string) ([]VersionValue, error) {
	if bundleID == "" {
		return nil, fmt.Errorf("%w: bundle id is empty", ErrInvalidInput)
	}
	params := Page{}.values()
	var out []VersionValue
	path := "/api/admin/customFieldSettings/bundles/version/" + url.PathEscape(bundleID) + "/values"
	if err := c.get(ctx, path, fieldsQuery(VersionValueFields, params), &out); err != nil {
		return nil, err
	}
	return out, nil
}
