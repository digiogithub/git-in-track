package trace

import (
	"reflect"
	"testing"
)

const docGo = `package a

// Implements: ACME-SP-0001.R1
// NextID returns the next free number of a type. It scans every file.
func NextID() int { return 1 }

// Implements: ACME-SP-0001.R2
func helper() int { return 2 }

// Store keeps
// items on disk.
type Store struct{}

// Put writes one item, refusing a stale rev
func (s *Store) Put() {}

const (
	// maxPage caps a page at fifty entries.
	maxPage = 50
	other   = 1
)

// Version is the format version of the index. It is bumped by hand.
var Version = 2
`

func TestDocSummaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, src string
		want            map[string]string
	}{
		{"go", "a.go", docGo, map[string]string{
			"NextID":    "NextID returns the next free number of a type.",
			"Store":     "Store keeps items on disk.",
			"Store.Put": "Put writes one item, refusing a stale rev",
			"maxPage":   "maxPage caps a page at fifty entries.",
			"Version":   "Version is the format version of the index.",
		}},
		{"not go", "a.py", "# F does it.\ndef F():\n    pass\n", nil},
		{"does not parse", "a.go", "package a\n\n// F does it.\nfunc F( {\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DocSummaries(tc.path, []byte(tc.src)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DocSummaries() = %#v\nwant %#v", got, tc.want)
			}
		})
	}
}
