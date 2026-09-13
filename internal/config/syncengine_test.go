package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestDefaultSyncEngineSection pins the shipped `sync.engine` values. They are
// the ones documented for `gintrack serve` and the ones the running engine
// falls back to, so a change here is a change to both.
func TestDefaultSyncEngineSection(t *testing.T) {
	t.Parallel()

	got := Default().Sync.Engine
	want := SyncEngine{Workers: 2, BatchSize: 20, Rate: 5, MaxAttempts: 5, Retention: 7 * 24 * time.Hour}
	if got != want {
		t.Errorf("sync.engine = %+v, want %+v", got, want)
	}
}

// TestSyncEngineSectionParses covers the file layer: a file that spells the
// section out keeps its values, and a file that omits it keeps the defaults.
func TestSyncEngineSectionParses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
		want SyncEngine
	}{
		{
			name: "an absent section keeps the defaults",
			yaml: "version: 1\n",
			want: DefaultSyncEngine(),
		},
		{
			name: "a partial section keeps the defaults of the keys it omits",
			yaml: "version: 1\nsync:\n  engine:\n    workers: 8\n",
			want: SyncEngine{Workers: 8, BatchSize: 20, Rate: 5, MaxAttempts: 5, Retention: 7 * 24 * time.Hour},
		},
		{
			name: "every key is read",
			yaml: "version: 1\nsync:\n  engine:\n    workers: 4\n    batchSize: 50\n" +
				"    rate: 2.5\n    maxAttempts: 3\n    retention: 24h\n",
			want: SyncEngine{Workers: 4, BatchSize: 50, Rate: 2.5, MaxAttempts: 3, Retention: 24 * time.Hour},
		},
		{
			name: "a negative rate removes the limit",
			yaml: "version: 1\nsync:\n  engine:\n    rate: -1\n",
			want: SyncEngine{Workers: 2, BatchSize: 20, Rate: -1, MaxAttempts: 5, Retention: 7 * 24 * time.Hour},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("Parse(): %v", err)
			}
			if cfg.Sync.Engine != tc.want {
				t.Errorf("sync.engine = %+v, want %+v", cfg.Sync.Engine, tc.want)
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate(): %v", err)
			}
		})
	}
}

// TestSyncEngineValidation checks that an out-of-range value is refused with a
// message naming the dotted key, which is the line the operator has to edit.
func TestSyncEngineValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		engine  SyncEngine
		wantKey string
	}{
		{name: "the shipped section is valid", engine: DefaultSyncEngine()},
		{name: "zero workers", engine: SyncEngine{Workers: -1}, wantKey: "sync.engine.workers"},
		{name: "too many workers", engine: SyncEngine{Workers: MaxSyncWorkers + 1}, wantKey: "sync.engine.workers"},
		{name: "a batch of none", engine: SyncEngine{BatchSize: -3}, wantKey: "sync.engine.batchSize"},
		{name: "an enormous batch", engine: SyncEngine{BatchSize: MaxSyncBatchSize + 1}, wantKey: "sync.engine.batchSize"},
		{name: "an impossible rate", engine: SyncEngine{Rate: MaxSyncRate + 1}, wantKey: "sync.engine.rate"},
		{name: "no attempts", engine: SyncEngine{MaxAttempts: -1}, wantKey: "sync.engine.maxAttempts"},
		{
			name:   "too many attempts",
			engine: SyncEngine{MaxAttempts: MaxSyncMaxAttempts + 1}, wantKey: "sync.engine.maxAttempts",
		},
		{name: "a negative retention", engine: SyncEngine{Retention: -time.Hour}, wantKey: "sync.engine.retention"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := Default()
			cfg.Sync.Engine = tc.engine
			err := cfg.Validate()
			if tc.wantKey == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want no error", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() accepted %+v", tc.engine)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() = %v, which does not unwrap to ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("Validate() = %v, want a message naming %q", err, tc.wantKey)
			}
		})
	}
}

// TestSyncEngineEnvironmentLayer checks that the four GINTRACK_SYNC_* variables
// override the file and that a value that is not a number is refused rather
// than ignored.
func TestSyncEngineEnvironmentLayer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		want    SyncEngine
		wantErr string
	}{
		{
			name: "every variable is read",
			env: map[string]string{
				EnvSyncWorkers: "4", EnvSyncBatch: "40",
				EnvSyncRate: "1.5", EnvSyncMaxAttempts: "2",
			},
			want: SyncEngine{Workers: 4, BatchSize: 40, Rate: 1.5, MaxAttempts: 2, Retention: 7 * 24 * time.Hour},
		},
		{
			name:    "a value that is not a number is refused",
			env:     map[string]string{EnvSyncWorkers: "many"},
			wantErr: EnvSyncWorkers,
		},
		{
			name:    "a rate that is not a number is refused",
			env:     map[string]string{EnvSyncRate: "fast"},
			wantErr: EnvSyncRate,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := Default()
			err := applyEnv(cfg, func(key string) string { return tc.env[key] })
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("applyEnv() = %v, want an error naming %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyEnv(): %v", err)
			}
			if cfg.Sync.Engine != tc.want {
				t.Errorf("sync.engine = %+v, want %+v", cfg.Sync.Engine, tc.want)
			}
		})
	}
}

// TestSyncEngineFlagLayer checks that a flag beats the environment and the
// file, and that an unset flag changes nothing.
func TestSyncEngineFlagLayer(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Sync.Engine.Workers = 7
	applyFlags(cfg, Flags{})
	if cfg.Sync.Engine.Workers != 7 {
		t.Errorf("an empty Flags changed workers to %d", cfg.Sync.Engine.Workers)
	}

	applyFlags(cfg, Flags{SyncWorkers: 3, SyncBatch: 9, SyncRate: 0.5, SyncMaxAttempts: 1})
	want := SyncEngine{Workers: 3, BatchSize: 9, Rate: 0.5, MaxAttempts: 1, Retention: 7 * 24 * time.Hour}
	if cfg.Sync.Engine != want {
		t.Errorf("sync.engine = %+v, want %+v", cfg.Sync.Engine, want)
	}
}

// TestSyncEngineRoundTripsThroughTheFile is what makes
// PATCH /api/v1/sync/settings able to answer `persisted: true`: the section the
// server writes has to come back unchanged on the next load.
func TestSyncEngineRoundTripsThroughTheFile(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/config.yaml"
	cfg := Default()
	cfg.Sync.Engine = SyncEngine{Workers: 6, BatchSize: 33, Rate: 2, MaxAttempts: 4, Retention: 48 * time.Hour}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if reloaded.Sync.Engine != cfg.Sync.Engine {
		t.Errorf("sync.engine = %+v, want %+v", reloaded.Sync.Engine, cfg.Sync.Engine)
	}
}
