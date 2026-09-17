package vault

// searchParams is the input of the "search" method, on a vault and on a
// workspace alike.
type searchParams struct {
	Q     string `json:"q"`
	Limit int    `json:"limit,omitempty"`
	// Project scopes the search to one project key. It is the original
	// spelling and keeps working next to Projects.
	Project string `json:"project,omitempty"`
	// Projects scopes the search to any of several project keys
	// (GIT-US-0102). Empty, together with Project, searches every project.
	Projects []string `json:"projects,omitempty"`
}

// ScopeKeys folds the single-project and the multi-project spelling of a
// search scope into one de-duplicated list. Nil means every project.
func ScopeKeys(project string, projects []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, key := range append([]string{project}, projects...) {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// InScope reports whether a hit of project key belongs to a search restricted
// to projects. An empty restriction admits everything, including a hit that
// names no project; a non-empty one admits only the keys it lists.
func InScope(projects []string, key string) bool {
	if len(projects) == 0 {
		return true
	}
	for _, p := range projects {
		if p == key {
			return true
		}
	}
	return false
}
