package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
)

// mountSpecs registers the spec routes of one project (GIT-US-0127, docs/07
// section 5.5): the specs themselves, their requirements as units of their
// own, and the derived coverage, trace and impact answers. Every handler is a
// thin mapping onto one CoreApi method — "item.*" for a spec,
// "requirement.*", "trace.requirement", "coverage.list", "impact.query" and
// "impact.report" — so the companion serves the very values browser-only mode
// reads through the WASM core. The static segments are registered before the
// {spec} parameter; chi prefers them, and no spec id is ever spelled like one.
func (s *Server) mountSpecs(r chi.Router) {
	r.Get("/", s.handleSpecList)
	r.Get("/requirements", s.handleRequirementList)
	r.Get("/coverage", s.handleSpecCoverage)
	r.Get("/impact", s.handleSpecImpact)
	r.Get("/impact/report", s.handleSpecImpactReport)

	r.Get("/{spec}", s.handleSpecGet)
	r.Get("/{spec}/coverage", s.handleSpecCoverage)
	r.Get("/{spec}/requirements", s.handleRequirementList)
	r.Post("/{spec}/requirements", s.handleRequirementCreate)
	r.Get("/{spec}/requirements/{req}", s.handleRequirementGet)
	r.Patch("/{spec}/requirements/{req}", s.handleRequirementUpdate)
	r.Get("/{spec}/requirements/{req}/trace", s.handleRequirementTrace)
}

// specProject resolves the repository that exposes the project of the path.
func (s *Server) specProject(w http.ResponseWriter, r *http.Request) (*mount, string, bool) {
	key := chi.URLParam(r, "key")
	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes project "+key+".")
		return nil, "", false
	}
	return m, key, true
}

// specOf reads the {spec} segment and checks that it is a spec id of the
// project the path names. An empty segment (the project-wide routes) is
// returned as is.
func specOf(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	spec := chi.URLParam(r, "spec")
	if spec == "" {
		return "", true
	}
	projectKey, typ, _, err := core.ParseItemID(spec)
	if err != nil || typ != core.CodeSpec {
		failProblem(w, r, codeInvalidRequest, spec+" is not a spec id: use <KEY>-SP-<NNNN>.")
		return "", false
	}
	if string(projectKey) != key {
		failProblem(w, r, codeNotFound, "Spec "+spec+" does not belong to project "+key+".")
		return "", false
	}
	return spec, true
}

// requirementRefOf builds the requirement ref of the path: {req} is either the
// bare `R<n>` or the full `<SPEC-ID>.R<n>` ref of the spec the path names.
func requirementRefOf(w http.ResponseWriter, r *http.Request, spec string) (string, bool) {
	req := strings.TrimSpace(chi.URLParam(r, "req"))
	if !strings.Contains(req, ".") {
		return spec + "." + req, true
	}
	if !strings.HasPrefix(req, spec+".") {
		failProblem(w, r, codeInvalidRequest, "Requirement "+req+" is not a requirement of "+spec+".")
		return "", false
	}
	return req, true
}

// handleSpecList serves GET /projects/{key}/specs: item.list narrowed to the
// specs of the project, with every other item filter of GET /items.
func (s *Server) handleSpecList(w http.ResponseWriter, r *http.Request) {
	m, key, ok := s.specProject(w, r)
	if !ok {
		return
	}
	filter := parseItemFilter(r)
	filter.Project = key
	filter.Type = []string{string(core.TypeSpec)}
	page, ok := s.call(w, r, m, "item.list", filter)
	if !ok {
		return
	}
	if total, found := totalOf(page); found {
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
	}
	writeJSON(w, r, http.StatusOK, page)
}

// handleSpecGet serves GET /projects/{key}/specs/{spec}: the spec as item.get
// returns it, with its file rev as the ETag.
func (s *Server) handleSpecGet(w http.ResponseWriter, r *http.Request) {
	m, key, ok := s.specProject(w, r)
	if !ok {
		return
	}
	spec, ok := specOf(w, r, key)
	if !ok {
		return
	}
	item, ok := s.call(w, r, m, "item.get", map[string]any{"id": spec})
	if !ok {
		return
	}
	writeEntity(w, r, http.StatusOK, item, revOf(item))
}

// handleRequirementList serves GET /projects/{key}/specs/requirements (every
// requirement of the project) and GET /projects/{key}/specs/{spec}/requirements.
func (s *Server) handleRequirementList(w http.ResponseWriter, r *http.Request) {
	m, key, ok := s.specProject(w, r)
	if !ok {
		return
	}
	spec, ok := specOf(w, r, key)
	if !ok {
		return
	}
	q := r.URL.Query()
	if spec == "" {
		spec = q.Get("spec")
	}
	params := map[string]any{"project": key}
	if spec != "" {
		params["spec"] = spec
	}
	if status := queryList(q["status"]); len(status) > 0 {
		params["status"] = status
	}
	if text := q.Get("q"); text != "" {
		params["q"] = text
	}
	for _, flag := range []string{"text", "includeDeleted"} {
		if v := q.Get(flag); v != "" {
			on, err := strconv.ParseBool(v)
			if err != nil {
				failProblem(w, r, codeInvalidRequest, flag+" must be true or false.")
				return
			}
			params[flag] = on
		}
	}
	result, ok := s.call(w, r, m, "requirement.list", params)
	if !ok {
		return
	}
	if total, found := mapInt(result, "total"); found {
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleRequirementGet serves GET /projects/{key}/specs/{spec}/requirements/{req}.
// The ETag is the requirement rev, the write token of PATCH; the body also
// carries the blockRev and the spec's file rev.
func (s *Server) handleRequirementGet(w http.ResponseWriter, r *http.Request) {
	m, ref, ok := s.requirementScope(w, r)
	if !ok {
		return
	}
	result, ok := s.call(w, r, m, "requirement.get", map[string]any{"ref": ref})
	if !ok {
		return
	}
	writeEntity(w, r, http.StatusOK, result, requirementRevOf(result))
}

// handleRequirementCreate serves POST /projects/{key}/specs/{spec}/requirements.
// A create needs no rev: R<n> is allocated by the core and never reused.
func (s *Server) handleRequirementCreate(w http.ResponseWriter, r *http.Request) {
	m, key, ok := s.specProject(w, r)
	if !ok {
		return
	}
	spec, ok := specOf(w, r, key)
	if !ok {
		return
	}
	var draft map[string]any
	if !decodeBody(w, r, &draft) {
		return
	}
	if draft == nil {
		draft = map[string]any{}
	}
	draft["spec"] = spec
	result, ok := s.call(w, r, m, "requirement.create", draft)
	if !ok {
		return
	}
	s.publishWrite(r, m, result, spec, "updated")
	if view, found := field(result, "requirement").(core.RequirementView); found {
		w.Header().Set("Location", apiPrefix+"/projects/"+key+"/specs/"+spec+"/requirements/"+view.Ref.String())
	}
	writeEntity(w, r, http.StatusCreated, requirementWriteBody(result), requirementRevOf(result))
}

// handleRequirementUpdate serves PATCH /projects/{key}/specs/{spec}/requirements/{req}.
// If-Match carries the requirement rev and is required; `*` is the explicit,
// unsafe waiver. The body is the sparse patch, either flat or as {"patch":…}.
func (s *Server) handleRequirementUpdate(w http.ResponseWriter, r *http.Request) {
	rev, present, wildcard := ifMatch(r)
	if !present {
		failProblem(w, r, codePreconditionRequired,
			"This write needs an If-Match header carrying the requirement rev you read (not its blockRev). Send If-Match: * to overwrite unconditionally.")
		return
	}
	if wildcard {
		rev = wildcardRev
	}
	var raw map[string]any
	if !decodeBody(w, r, &raw) {
		return
	}
	m, ref, ok := s.requirementScope(w, r)
	if !ok {
		return
	}
	patch := any(raw)
	if nested, found := raw["patch"].(map[string]any); found {
		patch = nested
	}
	result, ok := s.call(w, r, m, "requirement.update", map[string]any{"ref": ref, "rev": rev, "patch": patch})
	if !ok {
		return
	}
	s.publishWrite(r, m, result, chi.URLParam(r, "spec"), "updated")
	writeEntity(w, r, http.StatusOK, requirementWriteBody(result), requirementRevOf(result))
}

// handleRequirementTrace serves GET …/requirements/{req}/trace: the computed
// trace of one requirement, `503 unavailable` without a tracer.
func (s *Server) handleRequirementTrace(w http.ResponseWriter, r *http.Request) {
	m, ref, ok := s.requirementScope(w, r)
	if !ok {
		return
	}
	result, ok := s.call(w, r, m, "trace.requirement", map[string]any{"ref": ref})
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// requirementScope resolves the mount and the full requirement ref of a
// per-requirement route.
func (s *Server) requirementScope(w http.ResponseWriter, r *http.Request) (*mount, string, bool) {
	m, key, ok := s.specProject(w, r)
	if !ok {
		return nil, "", false
	}
	spec, ok := specOf(w, r, key)
	if !ok {
		return nil, "", false
	}
	ref, ok := requirementRefOf(w, r, spec)
	if !ok {
		return nil, "", false
	}
	return m, ref, true
}

// handleSpecCoverage serves GET /projects/{key}/specs/coverage and
// GET /projects/{key}/specs/{spec}/coverage: one compact coverage row per
// requirement, `503 unavailable` without a coverage backend.
func (s *Server) handleSpecCoverage(w http.ResponseWriter, r *http.Request) {
	m, key, ok := s.specProject(w, r)
	if !ok {
		return
	}
	spec, ok := specOf(w, r, key)
	if !ok {
		return
	}
	q := r.URL.Query()
	if spec == "" {
		spec = q.Get("spec")
	}
	params := map[string]any{"project": key}
	if spec != "" {
		params["spec"] = spec
	}
	if refs := queryList(q["ref"]); len(refs) > 0 {
		params["refs"] = refs
	}
	if status := queryList(q["status"]); len(status) > 0 {
		params["status"] = status
	}
	result, ok := s.call(w, r, m, "coverage.list", params)
	if !ok {
		return
	}
	if total, found := mapInt(result, "total"); found {
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleSpecImpact serves GET /projects/{key}/specs/impact?base=&head=: the
// requirements a diff of the project's repository affects, per tier.
func (s *Server) handleSpecImpact(w http.ResponseWriter, r *http.Request) {
	m, _, ok := s.specProject(w, r)
	if !ok {
		return
	}
	params, ok := impactParams(w, r)
	if !ok {
		return
	}
	result, ok := s.call(w, r, m, "impact.query", params)
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// handleSpecImpactReport serves GET /projects/{key}/specs/impact/report: the
// same query rendered as the ranked, token-budgeted report the MCP tool and
// the CLI print.
func (s *Server) handleSpecImpactReport(w http.ResponseWriter, r *http.Request) {
	m, _, ok := s.specProject(w, r)
	if !ok {
		return
	}
	params, ok := impactParams(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if v := q.Get("budget"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			failProblem(w, r, codeInvalidRequest, "budget must be a number of tokens.")
			return
		}
		params["budget"] = n
	}
	if v := q.Get("cursor"); v != "" {
		params["cursor"] = v
	}
	if v := q.Get("format"); v != "" {
		params["format"] = v
	}
	result, ok := s.call(w, r, m, "impact.report", params)
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, result)
}

// impactParams reads the query of impact.query from the query string: base,
// head, story, title, tiers (repeatable or comma-separated), depth and limit.
func impactParams(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	q := r.URL.Query()
	params := map[string]any{}
	for _, name := range []string{"base", "head", "story", "title"} {
		if v := strings.TrimSpace(q.Get(name)); v != "" {
			params[name] = v
		}
	}
	if raw := queryList(q["tier"]); len(raw) > 0 || len(queryList(q["tiers"])) > 0 {
		raw = append(raw, queryList(q["tiers"])...)
		tiers := make([]int, 0, len(raw))
		for _, v := range raw {
			n, err := strconv.Atoi(v)
			if err != nil {
				failProblem(w, r, codeInvalidRequest, "tiers must be 1, 2 or 3.")
				return nil, false
			}
			tiers = append(tiers, n)
		}
		params["tiers"] = tiers
	}
	for _, name := range []string{"depth", "limit"} {
		if v := q.Get(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				failProblem(w, r, codeInvalidRequest, name+" must be a number.")
				return nil, false
			}
			params[name] = n
		}
	}
	return params, true
}

// queryList flattens a repeatable query parameter whose values may also be
// comma-separated.
func queryList(values []string) []string {
	var out []string
	for _, v := range values {
		out = append(out, splitList(v)...)
	}
	return out
}

// mapInt reads an int member of the map a core method returned.
func mapInt(result any, name string) (int, bool) {
	m, ok := result.(map[string]any)
	if !ok {
		return 0, false
	}
	n, ok := m[name].(int)
	return n, ok
}

// requirementRevOf reads the requirement rev of a requirement.get or
// requirement write result.
func requirementRevOf(result any) string {
	if view, ok := field(result, "requirement").(core.RequirementView); ok {
		return string(view.Rev)
	}
	return ""
}

// requirementWriteBody is the answer of a requirement write: the result of the
// core without its WriteSet, whose file texts the client never needs.
func requirementWriteBody(result any) any {
	m, ok := result.(map[string]any)
	if !ok {
		return result
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k != "writes" {
			out[k] = v
		}
	}
	return out
}
