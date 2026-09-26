package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// FuzzLoad feeds arbitrary bytes through the real config.json path: Load
// must not panic and must return exactly one of config or error for a file
// that exists, Validate must not panic on whatever Load accepts, and a
// config that validates must survive a Save/Load round trip unchanged.
//
// XDG_CONFIG_HOME is set once for the whole target (Setenv is not allowed
// inside the fuzz function); fuzz inputs run one at a time per process, so
// rewriting the same file each iteration is safe.
func FuzzLoad(f *testing.F) {
	for _, seed := range []string{
		`{"client_id":"abc"}`,
		`{"client_id":"abc","bitrate":320,"enable_librespot":true,"audio_backend":"pipe","appearance":"dark"}`,
		`{"client_id":"abc","theme":{"primary":{"light":"#fff","dark":"#1DB954"},"gradient_end":{"dark":"#12"}}}`,
		`{"client_id":"abc","bitrate":128}`,
		`{"client_id":"abc","colour":"red"}`,
		`{"client_id":"abc"} trailing`,
		`{"theme":null,"bitrate":1e400}`,
		``,
		`[]`,
	} {
		f.Add([]byte(seed))
	}

	dir := f.TempDir()
	f.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "tuify", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if (cfg == nil) == (err == nil) {
			t.Fatalf("Load() = (%v, %v), want exactly one non-nil for an existing file", cfg, err)
		}
		if err != nil {
			return
		}
		if cfg.Validate() != nil {
			return
		}
		if err := Save(cfg); err != nil {
			t.Fatalf("Save of a valid config: %v", err)
		}
		again, err := Load()
		if err != nil {
			t.Fatalf("reload after Save: %v", err)
		}
		if !reflect.DeepEqual(again, cfg) {
			t.Fatalf("Save/Load round trip changed config:\n got %+v\nwant %+v", again, cfg)
		}
	})
}
