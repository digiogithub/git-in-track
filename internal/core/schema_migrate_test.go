package core

import (
	"errors"
	"slices"
	"testing"
)

func TestPlanSchemaMigration(t *testing.T) {
	t.Parallel()

	const rest = "key: ACME\nname: Acme\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n    - {id: done, category: done}\n"
	tests := []struct {
		name      string
		in        string
		to        int
		want      string // empty: nothing to write
		from      int
		removed   []string
		added     []string
		err       error
		downgrade bool
	}{
		{name: "raises 1 to 2 in one line, comments kept", in: "# the project\nschema: 1 # layout\n" + rest, to: 2,
			want: "# the project\nschema: 2 # layout\n" + rest, from: 1,
			removed: []string{"schema: 1 # layout"}, added: []string{"schema: 2 # layout"}},
		{name: "already at the target is a no-op", in: "schema: 2\n" + rest, to: 2, from: 2},
		{name: "1 to 1 is a no-op", in: "schema: 1\n" + rest, to: 1, from: 1},
		{name: "downgrade refused", in: "schema: 2\n" + rest, to: 1, err: ErrSchemaDowngrade, downgrade: true},
		{name: "newer than supported refused", in: "schema: 9\n" + rest, to: 2, err: ErrSchemaDowngrade, downgrade: true},
		{name: "target above supported refused", in: "schema: 1\n" + rest, to: SupportedSchema + 1, err: ErrSchemaMigration},
		{name: "target zero refused", in: "schema: 1\n" + rest, to: 0, err: ErrSchemaMigration},
		{name: "no schema refused", in: rest, to: 2, err: ErrSchemaMigration},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, err := PlanSchemaMigration([]byte(tt.in), tt.to)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("err = %v, want %v", err, tt.err)
				}
				if got := errors.Is(err, ErrSchemaDowngrade); got != tt.downgrade {
					t.Errorf("downgrade = %v, want %v", got, tt.downgrade)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanSchemaMigration(): %v", err)
			}
			if m.From != tt.from || m.To != tt.to {
				t.Errorf("from/to = %d/%d, want %d/%d", m.From, m.To, tt.from, tt.to)
			}
			if tt.want == "" {
				if m.Changed() {
					t.Errorf("want no change, got %q", m.Data)
				}
				return
			}
			if string(m.Data) != tt.want {
				t.Errorf("got\n%s\nwant\n%s", m.Data, tt.want)
			}
			if !slices.Equal(m.Removed, tt.removed) || !slices.Equal(m.Added, tt.added) {
				t.Errorf("diff = -%q +%q, want -%q +%q", m.Removed, m.Added, tt.removed, tt.added)
			}
			// Idempotent: planning the result again changes nothing.
			again, err := PlanSchemaMigration(m.Data, tt.to)
			if err != nil || again.Changed() {
				t.Errorf("second run = %v, %v; want no change", again, err)
			}
		})
	}
}
