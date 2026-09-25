package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The drift tests of docs/03 section 18: the shipped JSON Schemas must say
// what the Go model says. Key sets and enumerations are derived from the Go
// code (struct tags, the parser's key list, the typed constants), patterns
// are compared by behavior on sample values, and documents the Go emitter
// writes must validate. A change on either side without the other fails here.

// schemaForType is the schema file of an item type.
func schemaForType(t *testing.T, typ ItemType) string {
	t.Helper()
	name, ok := JSONSchemaFor(typ)
	if !ok {
		t.Fatalf("no schema for %q", typ)
	}
	return name
}

// keysOf returns the sorted keys of a schema's properties.
func keysOf(t *testing.T, s *schemaSet, n node) []string {
	t.Helper()
	m, ok := s.deref(t, n).schema.(map[string]any)
	if !ok {
		t.Fatalf("%s: not an object schema", n.doc)
	}
	props, _ := m["properties"].(map[string]any)
	out := make([]string, 0, len(props))
	for k := range props {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// enumOf returns the sorted string values of a schema's enum.
func enumOf(t *testing.T, s *schemaSet, n node) []string {
	t.Helper()
	m, _ := s.deref(t, n).schema.(map[string]any)
	list, ok := m["enum"].([]any)
	if !ok {
		t.Fatalf("%s: no enum", n.doc)
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.(string))
	}
	sort.Strings(out)
	return out
}

func sortedSet(keys map[string]bool) []string {
	out := make([]string, 0, len(keys))
	for k, on := range keys {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func sameStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s drifted from the Go model:\n schema: %v\n     go: %v", what, got, want)
	}
}

// yamlTags returns the front-matter keys of a struct type: its yaml tag names,
// skipping untagged and "-" fields.
func yamlTags(typ reflect.Type) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("yaml")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		out[name] = true
	}
	return out
}

// goConstValues returns the values of every string constant of a named type
// declared in this package, read from the source so that a new constant — a
// new link kind, a new priority — shows up here without anyone listing it.
func goConstValues(t *testing.T, typeName string) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				ident, ok := vs.Type.(*ast.Ident)
				if !ok || ident.Name != typeName {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: a %s constant is not a string literal", f, typeName)
					}
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					seen[s] = true
				}
			}
		}
	}
	if len(seen) == 0 {
		t.Fatalf("no constant of type %s found", typeName)
	}
	return sortedSet(seen)
}

func TestJSONSchemaFilesAreWellFormed(t *testing.T) {
	s := loadSchemaSet(t)

	want := []string{"common.defs.json", "project.schema.json"}
	for _, typ := range ItemTypes() {
		want = append(want, schemaForType(t, typ))
	}
	sort.Strings(want)
	sameStrings(t, "the shipped schema files", JSONSchemaNames(), want)

	for _, name := range JSONSchemaNames() {
		t.Run(name, func(t *testing.T) {
			doc := s.docs[name]
			if doc["$schema"] != JSONSchemaDraft {
				t.Errorf("$schema = %v, want %s", doc["$schema"], JSONSchemaDraft)
			}
			if doc["$id"] != JSONSchemaBaseURI+name {
				t.Errorf("$id = %v, want %s", doc["$id"], JSONSchemaBaseURI+name)
			}
			walkSchema(t, s, node{doc: name, schema: doc}, name)
		})
	}
}

// walkSchema checks every schema position: only known keywords, resolvable
// $refs and patterns that compile.
func walkSchema(t *testing.T, s *schemaSet, n node, at string) {
	t.Helper()
	m, ok := n.schema.(map[string]any)
	if !ok {
		if _, isBool := n.schema.(bool); !isBool {
			t.Errorf("%s: a schema must be an object or a boolean, got %T", at, n.schema)
		}
		return
	}
	for k, v := range m {
		if !schemaKeywords[k] {
			t.Errorf("%s: keyword %q is not supported by the drift tests' evaluator", at, k)
		}
		switch k {
		case "$ref":
			if _, err := s.resolve(n.doc, v.(string)); err != nil {
				t.Errorf("%s: %v", at, err)
			}
		case "pattern":
			if _, err := regexp.Compile(v.(string)); err != nil {
				t.Errorf("%s: pattern %q is not portable: %v", at, v, err)
			}
		case "properties", "patternProperties", "$defs":
			for name, sub := range v.(map[string]any) {
				if k == "patternProperties" {
					if _, err := regexp.Compile(name); err != nil {
						t.Errorf("%s: pattern property %q: %v", at, name, err)
					}
				}
				walkSchema(t, s, node{doc: n.doc, schema: sub}, at+"/"+k+"/"+name)
			}
		case "additionalProperties", "propertyNames", "items":
			walkSchema(t, s, node{doc: n.doc, schema: v}, at+"/"+k)
		case "anyOf":
			for i, sub := range v.([]any) {
				walkSchema(t, s, node{doc: n.doc, schema: sub}, at+"/anyOf/"+strconv.Itoa(i))
			}
		}
	}
}

// itemSchemaKeys is the front-matter key set a type's schema must list,
// derived from the parser (canonicalKeyOrder) and the per-type rules the Go
// validator enforces:
//   - parent and its deprecated alias epic only on a type that has a parent
//     (parentCodes);
//   - start and owner only on a milestone (docs/03 section 10);
//   - requirements only on a spec (E-REQ-FIELD);
//   - none of specOnlyAbsentFields on a spec (E-FIELD-TYPE).
func itemSchemaKeys(typ ItemType) []string {
	commentOnly := map[string]bool{"item": true, "in_reply_to": true, "kind": true, "reactions": true}
	milestoneOnly := map[string]bool{"start": true, "owner": true}
	specAbsent := map[string]bool{}
	for _, f := range specOnlyAbsentFields {
		specAbsent[f] = true
	}
	keys := map[string]bool{}
	for _, k := range canonicalKeyOrder {
		switch {
		case commentOnly[k]:
		case (k == "parent" || k == "epic") && len(parentCodes(typ)) == 0:
		case milestoneOnly[k] && typ != TypeMilestone:
		case k == "requirements" && typ != TypeSpec:
		case specAbsent[k] && typ == TypeSpec:
		default:
			keys[k] = true
		}
	}
	return sortedSet(keys)
}

func TestJSONSchemaItemKeysMatchGo(t *testing.T) {
	s := loadSchemaSet(t)
	for _, typ := range ItemTypes() {
		if typ == TypeComment {
			continue
		}
		t.Run(string(typ), func(t *testing.T) {
			name := schemaForType(t, typ)
			sameStrings(t, name+" properties", keysOf(t, s, s.root(name)), itemSchemaKeys(typ))
			doc := s.docs[name]
			if doc["additionalProperties"] != false {
				t.Errorf("%s must be closed: unknown keys are rejected unless prefixed x- (R-CF-4)", name)
			}
			if c := s.child(t, s.root(name), "properties", "type").schema.(map[string]any)["const"]; c != string(typ) {
				t.Errorf("%s: type const = %v, want %s", name, c, typ)
			}
		})
	}
	t.Run("comment", func(t *testing.T) {
		want := yamlTags(reflect.TypeOf(Comment{}))
		want["type"] = true
		sameStrings(t, "comment.schema.json properties", keysOf(t, s, s.root("comment.schema.json")), sortedSet(want))
	})
}

func TestJSONSchemaNestedKeysMatchGo(t *testing.T) {
	s := loadSchemaSet(t)
	common := s.root("common.defs.json")
	def := func(name string) node { return s.child(t, common, "$defs", name) }

	sameStrings(t, "link", keysOf(t, s, def("link")), sortedSet(yamlTags(reflect.TypeOf(Link{}))))
	sameStrings(t, "external", keysOf(t, s, def("external")), sortedSet(yamlTags(reflect.TypeOf(External{}))))
	sameStrings(t, "inbox", keysOf(t, s, def("inbox")), sortedSet(inboxKnownKeys))
	sameStrings(t, "inbox (struct)", keysOf(t, s, def("inbox")), sortedSet(yamlTags(reflect.TypeOf(ItemInbox{}))))
	sameStrings(t, "requirement", keysOf(t, s, def("requirement")), sortedSet(requirementKnownKeys))
	sameStrings(t, "requirement.trace", keysOf(t, s, s.child(t, def("requirement"), "properties", "trace")), sortedSet(traceKnownKeys))
	sameStrings(t, "requirement.verified", keysOf(t, s, s.child(t, def("requirement"), "properties", "verified")), sortedSet(verifiedKnownKeys))

	// The open blocks preserve unknown keys (R-FMT-6), the fixed ones do not.
	for name, open := range map[string]bool{"inbox": true, "requirement": true, "link": false, "external": false} {
		if got := def(name).schema.(map[string]any)["additionalProperties"]; got != open {
			t.Errorf("$defs/%s additionalProperties = %v, want %v", name, got, open)
		}
	}

	// project.yaml: every struct level against the matching schema level.
	project := s.root("project.schema.json")
	checkStructKeys(t, s, project, reflect.TypeOf(ProjectConfig{}), "project.yaml", map[string]bool{"integrations": true})
}

// checkStructKeys compares the yaml keys of a struct with an object schema and
// recurses into nested structs, slices and maps. Types with their own YAML
// codec are leaves: their shape is checked by round trip instead. extra lists
// keys the schema may add because another package reads them.
func checkStructKeys(t *testing.T, s *schemaSet, n node, typ reflect.Type, at string, extra map[string]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	unmarshaler := reflect.TypeOf((*yaml.Unmarshaler)(nil)).Elem()
	if reflect.PointerTo(typ).Implements(unmarshaler) {
		return
	}
	switch typ.Kind() {
	case reflect.Struct:
		want := yamlTags(typ)
		for k := range extra {
			want[k] = true
		}
		sameStrings(t, at, keysOf(t, s, n), sortedSet(want))
		if s.deref(t, n).schema.(map[string]any)["additionalProperties"] != false {
			t.Errorf("%s: want additionalProperties false", at)
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			if name == "" || name == "-" {
				continue
			}
			checkStructKeys(t, s, s.child(t, n, "properties", name), f.Type, at+"."+name, nil)
		}
	case reflect.Slice:
		checkStructKeys(t, s, s.child(t, n, "items"), typ.Elem(), at+"[]", nil)
	case reflect.Map:
		checkStructKeys(t, s, s.child(t, n, "additionalProperties"), typ.Elem(), at+".*", nil)
	}
}

func TestJSONSchemaEnumsMatchGo(t *testing.T) {
	s := loadSchemaSet(t)
	common := s.root("common.defs.json")
	project := s.root("project.schema.json")

	// Link kinds: every known kind a file may write; the computed-only
	// inverses are absent, so a file carrying one is rejected (R-LINK-8).
	var writable []string
	for _, k := range goConstValues(t, "LinkKind") {
		if LinkKind(k).Writable() {
			writable = append(writable, k)
		}
	}
	sameStrings(t, "link kinds", enumOf(t, s, s.child(t, common, "$defs", "link", "properties", "kind")), writable)

	sameStrings(t, "priorities", enumOf(t, s, s.child(t, common, "$defs", "priority")), goConstValues(t, "Priority"))
	sameStrings(t, "inbox statuses", enumOf(t, s, s.child(t, common, "$defs", "inbox", "properties", "status")), goConstValues(t, "InboxStatus"))
	sameStrings(t, "comment kinds", enumOf(t, s, s.child(t, s.root("comment.schema.json"), "properties", "kind")), goConstValues(t, "CommentKind"))
	sameStrings(t, "status categories",
		enumOf(t, s, s.child(t, project, "properties", "workflow", "properties", "statuses", "items", "properties", "category")),
		goConstValues(t, "StatusCategory"))
	sameStrings(t, "lint levels",
		enumOf(t, s, s.child(t, project, "properties", "specs", "properties", "lint", "anyOf", "0")),
		goConstValues(t, "LintLevel"))
	var rules []string
	for _, r := range LintRules {
		rules = append(rules, string(r))
	}
	sort.Strings(rules)
	sameStrings(t, "lint rules",
		enumOf(t, s, s.child(t, project, "properties", "specs", "properties", "lint", "anyOf", "1", "properties", "rules", "propertyNames")),
		rules)

	var itemTypes []string
	for _, typ := range ItemTypes() {
		if typ != TypeComment {
			itemTypes = append(itemTypes, string(typ))
		}
	}
	sort.Strings(itemTypes)
	sameStrings(t, "project item types",
		enumOf(t, s, s.child(t, project, "properties", "id_allocation", "properties", "counters", "propertyNames")), itemTypes)

	// Enumerations the Go validator checks with a switch: every schema value
	// must be accepted by it.
	for _, kind := range enumOf(t, s, s.child(t, project, "properties", "custom_fields", "items", "properties", "type")) {
		if !validCustomType(kind) {
			t.Errorf("custom field type %q is in the schema but refused by validCustomType", kind)
		}
	}
	for _, scale := range enumOf(t, s, s.child(t, project, "properties", "estimation", "properties", "scale")) {
		d := &diagSet{}
		validateProjectEnums(d, &ProjectConfig{Estimation: Estimation{Scale: scale}})
		if len(d.out) > 0 {
			t.Errorf("estimation scale %q is in the schema but refused by the validator: %v", scale, d.out)
		}
	}

	// schema: a build writes up to SupportedSchema (R-EVO-2).
	schemaProp := s.child(t, project, "properties", "schema").schema.(map[string]any)
	if schemaProp["maximum"] != float64(SupportedSchema) || schemaProp["minimum"] != float64(InitialSchema) {
		t.Errorf("project schema range = [%v, %v], want [%d, %d]", schemaProp["minimum"], schemaProp["maximum"], InitialSchema, SupportedSchema)
	}
}

// accepts reports whether a string schema accepts v.
func accepts(s *schemaSet, n node, v string) bool { return len(s.validate(n, v, "$")) == 0 }

func TestJSONSchemaPatternsMatchGo(t *testing.T) {
	s := loadSchemaSet(t)
	def := func(name string) node { return s.child(t, s.root("common.defs.json"), "$defs", name) }

	cases := []struct {
		name    string
		n       node
		goOK    func(string) bool
		samples []string
	}{
		{"id", def("id"), func(v string) bool { return ItemID(v).Valid() },
			[]string{"ACME-US-0042", "ACME-T-10234", "ACME-SP-0003", "A-US-0001", "acme-US-0001", "ACME-US-042", "ACME-XX-0001", "ACMEACMEACM-US-0001", "ACME-SP-0003.R1"}},
		{"reqRef", def("reqRef"), func(v string) bool {
			key, bare, qualified := strings.Cut(v, "/")
			if !qualified {
				return IsRequirementRef(v)
			}
			return ValidProjectKey(ProjectKey(key)) && IsRequirementRef(bare)
		}, []string{"ACME-SP-0003.R2", "WEB/WEB-SP-0001.R4", "ACME-SP-0003.R0", "ACME-SP-0003.R02", "ACME-SP-0003.r2", "ACME-US-0003.R2", "ACME-SP-0003"}},
		{"handle", def("handle"), memberHandleRE.MatchString,
			[]string{"jose", "bot-ci", "claude-code", "Jose", "-x", "jose@digio.es", strings.Repeat("a", 33)}},
		{"commit", s.child(t, def("requirement"), "properties", "verified", "properties", "commit"), commitRE.MatchString,
			[]string{strings.Repeat("a", 40), strings.Repeat("0", 64), strings.Repeat("a", 39), strings.Repeat("A", 40)}},
		{"traceRef", def("traceRef"), func(v string) bool { return checkTraceRef(v) == nil },
			[]string{"internal/core/allocator.go", "internal/core/allocator.go#NextID", "a/b_test.go#TestX/sub", "./x.go", ".hidden/x.go", "..x/y", "a//b",
				"/abs/x.go", "../x.go", "a/../b", "a/..", "x.go#", `a\b.go`, "#Sym"}},
		{"project key", s.child(t, s.root("project.schema.json"), "properties", "key"), func(v string) bool { return ValidProjectKey(ProjectKey(v)) },
			[]string{"ACME", "GIT", "A", "acme", "A1234567890", "AB"}},
	}
	for _, c := range cases {
		for _, v := range c.samples {
			if got, want := accepts(s, c.n, v), c.goOK(v); got != want {
				t.Errorf("%s %q: schema accepts=%v, Go accepts=%v", c.name, v, got, want)
			}
		}
	}

	// Requirement keys of the requirements: map.
	names := s.child(t, s.root("spec.schema.json"), "properties", "requirements", "propertyNames")
	for _, k := range []string{"R1", "R10", "R0", "R02", "r2", "R", "X1"} {
		_, goOK := ParseRequirementKey(k)
		if got := accepts(s, names, k); got != goOK {
			t.Errorf("requirement key %q: schema accepts=%v, Go accepts=%v", k, got, goOK)
		}
	}

	// The id and parent of every item type follow its type code and parentCodes.
	codes := []TypeCode{CodeEpic, CodeStory, CodeTask, CodeMilestone, CodeSpec}
	for _, typ := range ItemTypes() {
		own, ok := TypeCodeFor(typ)
		if !ok {
			continue
		}
		name := schemaForType(t, typ)
		root := s.root(name)
		for _, c := range codes {
			id := "ACME-" + string(c) + "-0001"
			if got := accepts(s, s.child(t, root, "properties", "id"), id); got != (c == own) {
				t.Errorf("%s id %q: accepts=%v", name, id, got)
			}
			if parents := parentCodes(typ); len(parents) > 0 {
				if got := accepts(s, s.child(t, root, "properties", "parent"), id); got != containsTypeCode(parents, c) {
					t.Errorf("%s parent %q: accepts=%v, parentCodes=%v", name, id, got, parents)
				}
			}
		}
	}
}

// fullItem returns an item of a type with every field its schema allows set,
// the way the Go emitter would write it.
func fullItem(typ ItemType) *Item {
	num := func(f float64) *float64 { return &f }
	ts := func(s string) Timestamp { v, _ := ParseTimestamp(s); return v }
	day := func(s string) Date { v, _ := ParseDate(s); return v }
	code, _ := TypeCodeFor(typ)
	it := &Item{
		ID: ItemID("ACME-" + string(code) + "-0007"), Type: typ, Title: "Everything set",
		Status: "in_progress", Priority: PriorityHigh,
		Assignees: []string{"jose", "bot-ci"}, Author: "marta", Labels: []string{"backend", "tech-debt"},
		Created: ts("2026-09-01T10:00:00Z"), Updated: ts("2026-09-02T10:00:00Z"),
		Started: ts("2026-09-01T11:00:00Z"), Closed: ts("2026-09-03T10:00:00Z"),
		Links: []Link{
			{Kind: LinkBlocks, Target: "ACME-US-0042"},
			{Kind: LinkRelatesTo, Target: "WEB/WEB-US-0031", Note: "cross-project"},
		},
		External:    []External{{System: "youtrack", ID: "PRJ-42", URL: "https://yt.example.com/issue/PRJ-42", Key: "PRJ", SyncedAt: ts("2026-09-02T09:00:00Z")}},
		Attachments: []string{"screenshot.png"},
		Custom:      map[string]any{"risk": "high"},
		Extra:       map[string]any{"x-tool": "kept"},
		Deleted:     true,
	}
	if typ != TypeSpec {
		it.Milestone, it.Sprint = "ACME-M-0001", "ACME-TEAM-S-0004"
		it.Estimate, it.Effort, it.Spent = num(5), num(8), num(2.5)
		it.Due = day("2026-10-01")
		it.Inbox = &ItemInbox{Status: InboxSnoozed, SnoozedUntil: day("2026-10-01"), Source: "web", Received: ts("2026-09-01T09:00:00Z"),
			Extra: map[string]any{"reporter": "someone"}}
	}
	switch typ {
	case TypeStory:
		it.Parent = "ACME-EP-0001"
		it.Links = append(it.Links, Link{Kind: LinkImplements, Target: "ACME-SP-0003.R2"}, Link{Kind: LinkModifies, Target: "ACME-SP-0003"})
	case TypeTask:
		it.Parent = "ACME-US-0042"
	case TypeMilestone:
		it.Owner, it.Start = "jose", day("2026-09-01")
	case TypeSpec:
		it.Links = append(it.Links, Link{Kind: LinkSupersedes, Target: "ACME-SP-0001"})
		it.Requirements = Requirements{
			"R1": {Status: "done"},
			"R2": {
				Status: "in_progress",
				Trace:  &RequirementTrace{Code: []string{"internal/core/allocator.go#NextID"}, Tests: []string{"internal/core/allocator_test.go#TestNextID/stale_counter"}},
				Verified: &Verification{Rev: "sha256:4e1b9c0d7a3f2e61", Commit: strings.Repeat("9c1f0a2e", 5),
					At: ts("2026-10-01T09:12:00Z"), By: "claude"},
				Links: []Link{{Kind: LinkSupersedes, Target: "ACME-SP-0001.R7"}, {Kind: LinkRelatesTo, Target: "ACME-US-0042"}},
				Extra: map[string]any{"x-owner": "marta"},
			},
		}
	}
	return it
}

func TestJSONSchemaAcceptsWhatGoWrites(t *testing.T) {
	s := loadSchemaSet(t)
	for _, typ := range ItemTypes() {
		if typ == TypeComment {
			continue
		}
		t.Run(string(typ), func(t *testing.T) {
			data, err := SerializeItem(fullItem(typ))
			if err != nil {
				t.Fatal(err)
			}
			if errs := s.validate(s.root(schemaForType(t, typ)), frontMatterJSON(t, data), "$"); len(errs) > 0 {
				t.Errorf("the schema refuses what SerializeItem writes:\n%s\n%s", strings.Join(errs, "\n"), data)
			}
			// Every property the schema lists must be one the emitter wrote.
			fm := frontMatterJSON(t, data).(map[string]any)
			for _, k := range keysOf(t, s, s.root(schemaForType(t, typ))) {
				if _, ok := fm[k]; !ok && k != "epic" && k != "blocks" && k != "depends_on" {
					t.Errorf("%s: fullItem does not exercise %q", typ, k)
				}
			}
		})
	}
	t.Run("comment", func(t *testing.T) {
		ts, _ := ParseTimestamp("2026-09-01T10:45:12Z")
		c := &Comment{Item: "ACME-US-0042", Author: "marta", AuthorName: "Marta", AuthorEmail: "marta@example.com",
			Created: ts, Updated: ts, InReplyTo: "ACME-US-0042#20260901T093300Z-jose", Kind: CommentKindComment,
			Reactions: map[string][]string{"+1": {"jose"}}, External: []External{{System: "youtrack", ID: "4-17"}},
			Attachments: []string{"idp-error.png"}, Extra: map[string]any{"x-bot": true}, Body: "Hi.\n"}
		data, err := SerializeComment(c)
		if err != nil {
			t.Fatal(err)
		}
		if errs := s.validate(s.root("comment.schema.json"), frontMatterJSON(t, data), "$"); len(errs) > 0 {
			t.Errorf("the schema refuses what SerializeComment writes:\n%s\n%s", strings.Join(errs, "\n"), data)
		}
	})
	t.Run("project", func(t *testing.T) {
		cfg := NewProjectConfig(NewProject{Key: "ACME", Name: "ACME"}, "docs")
		cfg.Labels = []Label{{Name: "backend", Color: "#2563eb", Description: "Server"}}
		cfg.Priorities = []Priority{PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow}
		cfg.Estimation = Estimation{Scale: "fibonacci", Values: []float64{1, 2, 3}, TrackHours: true}
		cfg.Defaults = map[ItemType]ItemDefaults{TypeStory: {Status: "backlog", Priority: PriorityMedium, Assignees: []string{"jose"}, Labels: []string{"backend"}}}
		cfg.CustomFields = []CustomField{{Key: "risk", Type: "enum", Values: []string{"low", "high"}, AppliesTo: []ItemType{TypeStory}, Default: "low", Description: "Risk"}}
		cfg.People = []Person{{Handle: "jose", Name: "Jose", Email: "jose@example.com", Kind: "human"}}
		cfg.Team = &TeamLink{Repo: "https://example.com/team.git", Key: "ACME-TEAM"}
		cfg.Links = &LinksConfig{Host: "github", WebURL: "https://github.com/acme/platform"}
		cfg.IDAllocation = IDAllocation{Strategy: "ranges", WriteCounters: true,
			Counters:  map[ItemType]int{TypeStory: 43, TypeSpec: 3},
			Reserved:  map[ItemType][]IDRange{TypeTask: {{From: 200, To: 249}}},
			Redirects: map[ItemID]ItemID{"ACME-US-0043": "ACME-US-0044"},
			Ranges:    map[string]map[ItemType][]IDRange{"jose": {TypeTask: {{From: 1000, To: 1999}}}}}
		cfg.Workflow.Transitions = map[Status][]Status{"todo": {"in_progress"}}
		words := []string{"fast"}
		cfg.Specs = &SpecsConfig{Lint: &SpecLintConfig{Severity: LintError, Rules: map[Code]LintLevel{LintReqVague: LintOff}, VagueWords: words}}
		data, err := marshalProjectConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if errs := s.validate(s.root("project.schema.json"), yamlJSON(t, data), "$"); len(errs) > 0 {
			t.Errorf("the schema refuses what the core writes:\n%s\n%s", strings.Join(errs, "\n"), data)
		}
		shorthand := "schema: 2\nkey: ACME\nname: ACME\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\nspecs:\n  lint: warning\n"
		if errs := s.validate(s.root("project.schema.json"), yamlJSON(t, []byte(shorthand)), "$"); len(errs) > 0 {
			t.Errorf("the specs.lint scalar shorthand is refused: %v", errs)
		}
	})
}

func TestJSONSchemaValidatesFixtures(t *testing.T) {
	s := loadSchemaSet(t)
	files := []string{
		"golden/comment.md", "golden/external-story.md", "golden/inbox-item.md",
		"golden/messy-story.md", "golden/spec-item.md", "golden/spec-links-story.md",
		"messy-story.md", "spec-links-story.md", "inbox-item.md",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", f))
			if err != nil {
				t.Fatal(err)
			}
			fm := frontMatterJSON(t, data)
			typ, _ := fm.(map[string]any)["type"].(string)
			name, ok := JSONSchemaFor(ItemType(typ))
			if !ok {
				t.Fatalf("unknown type %q", typ)
			}
			if errs := s.validate(s.root(name), fm, "$"); len(errs) > 0 {
				t.Errorf("%s:\n%s", f, strings.Join(errs, "\n"))
			}
		})
	}
	t.Run("validate/project.yaml", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("testdata", "validate", ProjectFileName))
		if err != nil {
			t.Fatal(err)
		}
		if errs := s.validate(s.root("project.schema.json"), yamlJSON(t, data), "$"); len(errs) > 0 {
			t.Errorf("%s", strings.Join(errs, "\n"))
		}
	})
}

func TestJSONSchemaRejects(t *testing.T) {
	s := loadSchemaSet(t)
	const head = "id: ACME-US-0042\ntype: story\ntitle: T\nstatus: todo\ncreated: 2026-09-01T10:00:00Z\nupdated: 2026-09-01T10:00:00Z\n"
	const spec = "id: ACME-SP-0003\ntype: spec\ntitle: T\nstatus: todo\ncreated: 2026-09-01T10:00:00Z\nupdated: 2026-09-01T10:00:00Z\n"
	cases := []struct {
		name, schema, doc string
		valid             bool
	}{
		{"baseline story", "story.schema.json", head, true},
		{"x- key is preserved and never validated (R-CF-4)", "story.schema.json", head + "x-anything: {a: [1]}\n", true},
		{"unknown top-level key", "story.schema.json", head + "shade: red\n", false},
		{"implemented_by is computed only (R-LINK-8)", "story.schema.json", head + "links:\n  - {kind: implemented_by, target: ACME-SP-0003.R2}\n", false},
		{"modified_by is computed only (R-LINK-8)", "story.schema.json", head + "links:\n  - {kind: modified_by, target: ACME-SP-0003}\n", false},
		{"implements a requirement", "story.schema.json", head + "links:\n  - {kind: implements, target: WEB/WEB-SP-0001.R4}\n", true},
		{"unknown link kind", "story.schema.json", head + "links:\n  - {kind: depends, target: ACME-US-0001}\n", false},
		{"story parent must be an epic", "story.schema.json", head + "parent: ACME-US-0001\n", false},
		{"requirements only on a spec", "story.schema.json", head + "requirements:\n  R1: {status: todo}\n", false},
		{"timestamp with an offset", "story.schema.json", strings.Replace(head, "10:00:00Z", "10:00:00+02:00", 1), false},
		{"missing updated", "story.schema.json", strings.Replace(head, "updated: 2026-09-01T10:00:00Z\n", "", 1), false},
		{"baseline spec", "spec.schema.json", spec, true},
		{"spec has no parent", "spec.schema.json", spec + "parent: ACME-EP-0001\n", false},
		{"spec has no milestone", "spec.schema.json", spec + "milestone: ACME-M-0001\n", false},
		{"spec has no inbox", "spec.schema.json", spec + "inbox: {status: pending}\n", false},
		{"requirement key R02", "spec.schema.json", spec + "requirements:\n  R02: {status: todo}\n", false},
		{"requirement unknown keys are kept", "spec.schema.json", spec + "requirements:\n  R1: {status: todo, x-owner: marta, trace: {code: [a.go], extra: 1}}\n", true},
		{"requirement trace leaving the repository", "spec.schema.json", spec + "requirements:\n  R1: {trace: {tests: [../x_test.go]}}\n", false},
		{"requirement verified missing commit", "spec.schema.json", spec + "requirements:\n  R1: {verified: {rev: \"sha256:4e1b9c0d7a3f2e61\", at: 2026-10-01T09:12:00Z, by: claude}}\n", false},
		{"requirement link implemented_by", "spec.schema.json", spec + "requirements:\n  R1:\n    links: [{kind: implemented_by, target: ACME-US-0042}]\n", false},
		{"comment needs an item", "comment.schema.json", "type: comment\nauthor: jose\ncreated: 2026-09-01T10:00:00Z\n", false},
		{"project unknown status category", "project.schema.json", "schema: 2\nkey: ACME\nname: A\nworkflow:\n  statuses:\n    - {id: todo, category: open}\n", false},
		{"project schema newer than supported", "project.schema.json", "schema: 3\nkey: ACME\nname: A\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n", false},
		{"project unknown lint rule", "project.schema.json", "schema: 2\nkey: ACME\nname: A\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\nspecs:\n  lint: {rules: {LINT-REQ-NOPE: error}}\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := s.validate(s.root(c.schema), yamlJSON(t, []byte(c.doc)), "$")
			if got := len(errs) == 0; got != c.valid {
				t.Errorf("valid = %v, want %v (%v)", got, c.valid, errs)
			}
		})
	}
}
