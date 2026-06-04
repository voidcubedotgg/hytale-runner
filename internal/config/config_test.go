package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// unset removes env vars for the duration of the test, restoring them after
// (so ambient values like the container's HYRUN_REGISTRY don't skew defaults).
func unset(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, v)
			os.Unsetenv(k)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	unset(t, "HYRUN_REGISTRY")
	cfg, err := Load(New())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg, Default) {
		t.Errorf("defaults mismatch:\n got %+v\nwant %+v", cfg, Default)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("HYRUN_MAX_MEMORY", "12G")
	cfg, err := Load(New())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxMemory != "12G" {
		t.Errorf("MaxMemory = %q, want 12G", cfg.MaxMemory)
	}
}

func TestRegistryEnv(t *testing.T) {
	t.Setenv("HYRUN_REGISTRY", "registry:5000")
	cfg, err := Load(New())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Registry != "registry:5000" {
		t.Errorf("Registry = %q, want registry:5000", cfg.Registry)
	}
}

func TestConfigFileLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(path, []byte("max-memory: 8G\nstate-tag: stable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}
	cfg, err := Load(v)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxMemory != "8G" || cfg.StateTag != "stable" {
		t.Errorf("file values not applied: MaxMemory=%q StateTag=%q", cfg.MaxMemory, cfg.StateTag)
	}
	if cfg.DataDir != Default.DataDir {
		t.Errorf("unspecified key lost its default: DataDir=%q", cfg.DataDir)
	}
}

func TestConfigFileMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("max-memory: [unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err == nil {
		t.Fatal("expected error reading malformed YAML, got nil")
	}
}
