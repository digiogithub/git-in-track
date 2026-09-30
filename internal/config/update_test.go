package config

import "testing"

func TestUpdateCheckOnStart(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		yaml    string
		want    bool
		wantErr bool
	}{
		{name: "default is off", yaml: "version: 1\n", want: false},
		{name: "explicit on", yaml: "update:\n  checkOnStart: true\n", want: true},
		{name: "explicit off", yaml: "update:\n  checkOnStart: false\n", want: false},
		{name: "a non-boolean is refused", yaml: "update:\n  checkOnStart: sometimes\n", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Parse([]byte(tc.yaml))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Parse error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && cfg.Update.CheckOnStart != tc.want {
				t.Fatalf("CheckOnStart = %v, want %v", cfg.Update.CheckOnStart, tc.want)
			}
		})
	}
}
