package trace

import (
	"reflect"
	"testing"
	"testing/fstest"
)

// declPkg is a package whose functions use its package-level names in every
// scoping shape the walker must tell apart.
const declPkg = `package page

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

var lo, hi = 1, 2

type Limit int

// Implements: ACME-SP-0004.R1
func boundedLimit(requested int) int {
	if requested > maxPageSize {
		return maxPageSize
	}
	return requested
}

func shadowed() int {
	maxPageSize := 5
	return maxPageSize
}

func shadowedInBlock() int {
	if maxPageSize := 5; maxPageSize > 1 {
		return maxPageSize
	}
	return maxPageSize
}

func param(maxPageSize int) int { return maxPageSize }

func selector(o struct{ maxPageSize int }) int { return o.maxPageSize }

func structKey() any { return struct{ maxPageSize int }{maxPageSize: 1} }

func mapKey() map[int]bool { return map[int]bool{maxPageSize: true} }

func closure() func() int { return func() int { return maxPageSize } }

func rangeVar(xs []int) (n int) {
	for _, maxPageSize := range xs {
		n += maxPageSize
	}
	return n
}

func typed(l Limit) Limit { return l }

func (l Limit) Clamp() Limit { return Limit(hi) }
`

const declOther = `package page

func elsewhere() int { return maxPageSize * 2 }
`

const declExternal = `package page_test

func TestExternal() { _ = maxPageSize }
`

func TestChangedDecls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		symbols []string
		want    []string
	}{
		{"one const of a block", []string{"maxPageSize"}, []string{"maxPageSize"}},
		{"a var spec with two names", []string{"lo"}, []string{"hi", "lo"}},
		{"a type", []string{"Limit"}, []string{"Limit"}},
		{"a function is not a declaration of this kind", []string{"boundedLimit"}, nil},
		{"nothing changed", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pkg, got := changedDecls("page.go", []byte(declPkg), tc.symbols)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("changedDecls() = %q, want %q", got, tc.want)
			}
			if got != nil && pkg != "page" {
				t.Errorf("package = %q, want page", pkg)
			}
		})
	}
	if pkg, got := changedDecls("bad.go", []byte("package page\nconst ("), []string{"x"}); pkg != "" || got != nil {
		t.Errorf("a file that does not parse = %q %q, want nothing", pkg, got)
	}
}

func TestDeclReferences(t *testing.T) {
	t.Parallel()
	tree := fstest.MapFS{
		"internal/page/page.go":       {Data: []byte(declPkg)},
		"internal/page/other.go":      {Data: []byte(declOther)},
		"internal/page/ext_test.go":   {Data: []byte(declExternal)},
		"internal/page/untraced.go":   {Data: []byte("package page\n\nfunc untraced() int { return maxPageSize }\n")},
		"internal/page/sub/sub.go":    {Data: []byte("package page\n\nfunc sub() int { return maxPageSize }\n")},
		"internal/page/broken.go":     {Data: []byte("package page\nfunc (")},
		"internal/page/README.md":     {Data: []byte("maxPageSize")},
		"internal/other/maxpage.go":   {Data: []byte("package page\n\nfunc far() int { return maxPageSize }\n")},
		"internal/page/testdata/x.go": {Data: []byte("package page\n\nfunc fixture() int { return maxPageSize }\n")},
	}
	traced := func(p string) bool { return p != "internal/page/untraced.go" }
	render := func(refs []declRef) []string {
		out := []string{}
		for _, r := range refs {
			out = append(out, r.path+"#"+r.symbol+" "+r.name)
		}
		return out
	}

	t.Run("a const", func(t *testing.T) {
		t.Parallel()
		got := render(declReferences(tree, "internal/page/page.go", "page", []string{"maxPageSize"}, traced))
		want := []string{
			"internal/page/other.go#elsewhere maxPageSize",
			"internal/page/page.go#boundedLimit maxPageSize",
			"internal/page/page.go#closure maxPageSize",
			"internal/page/page.go#mapKey maxPageSize",
			"internal/page/page.go#shadowedInBlock maxPageSize",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("declReferences() = %q\nwant %q", got, want)
		}
	})
	t.Run("a type and a var", func(t *testing.T) {
		t.Parallel()
		got := render(declReferences(tree, "internal/page/page.go", "page", []string{"Limit", "hi"}, traced))
		want := []string{
			"internal/page/page.go#Limit.Clamp Limit",
			"internal/page/page.go#Limit.Clamp hi",
			"internal/page/page.go#typed Limit",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("declReferences() = %q\nwant %q", got, want)
		}
	})
	t.Run("nothing to look for", func(t *testing.T) {
		t.Parallel()
		if got := declReferences(tree, "internal/page/page.go", "page", nil, traced); got != nil {
			t.Errorf("declReferences() = %v, want nil", got)
		}
		if got := declReferences(nil, "internal/page/page.go", "page", []string{"maxPageSize"}, traced); got != nil {
			t.Errorf("declReferences(nil tree) = %v, want nil", got)
		}
	})
}

func TestEnclosingBoth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ a, b, want string }{
		{"NextID", "NextID", "NextID"},
		{"NextID", "", ""},
		{"", "", ""},
		{"NextID", "helper", ""},
		{"Store.Load", "Store.Save", ""},
		{"TestX/a", "TestX/b", "TestX"},
		{"TestX/a", "TestX", "TestX"},
		{"TestX/a/deep", "TestX/a/other", "TestX/a"},
		{"alloc > allocates", "alloc > frees", "alloc"},
	} {
		if got := enclosingBoth(tc.a, tc.b); got != tc.want {
			t.Errorf("enclosingBoth(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
