package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote/auth"
)

func testConfig(dataDir string) config.Config {
	cfg := config.Default
	cfg.DataDir = dataDir
	return cfg
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStoreThenLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	writeFile(t, src, "world.dat", "hello world")
	writeFile(t, src, "config.yaml", "tick: 20")

	store := memory.New()
	if err := Store(ctx, testConfig(src), store); err != nil {
		t.Fatalf("Store: %v", err)
	}

	dst := t.TempDir()
	if err := Load(ctx, testConfig(dst), store); err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, f := range []struct{ name, want string }{
		{"world.dat", "hello world"},
		{"config.yaml", "tick: 20"},
	} {
		got, err := os.ReadFile(filepath.Join(dst, f.name))
		if err != nil {
			t.Errorf("restored %s: %v", f.name, err)
			continue
		}
		if string(got) != f.want {
			t.Errorf("%s = %q, want %q", f.name, got, f.want)
		}
	}
}

func TestStoreEmptyDir(t *testing.T) {
	err := Store(context.Background(), testConfig(t.TempDir()), memory.New())
	if !errors.Is(err, ErrNoState) {
		t.Fatalf("err = %v, want ErrNoState", err)
	}
}

func TestStoreBadDataDir(t *testing.T) {
	// A regular file is not a valid file-store root.
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Store(context.Background(), testConfig(f), memory.New()); err == nil {
		t.Fatal("expected error for non-directory data dir, got nil")
	}
}

func TestRemoteTargetAnonymousByDefault(t *testing.T) {
	cfg := config.Default
	repo, err := RemoteTarget(cfg)
	if err != nil {
		t.Fatalf("RemoteTarget: %v", err)
	}
	if _, ok := repo.Client.(*auth.Client); ok {
		t.Error("expected anonymous client when no credentials set, got *auth.Client")
	}
	if repo.PlainHTTP != cfg.PlainHTTP {
		t.Errorf("PlainHTTP = %v, want %v", repo.PlainHTTP, cfg.PlainHTTP)
	}
}

func TestRemoteTargetAuthClientWhenCredentialed(t *testing.T) {
	cfg := config.Default
	cfg.Registry = "registry.example.com"
	cfg.RegistryUser = "alice"
	cfg.RegistryPass = "s3cret"

	repo, err := RemoteTarget(cfg)
	if err != nil {
		t.Fatalf("RemoteTarget: %v", err)
	}
	client, ok := repo.Client.(*auth.Client)
	if !ok {
		t.Fatalf("repo.Client = %T, want *auth.Client", repo.Client)
	}
	got, err := client.Credential(context.Background(), cfg.Registry)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if got.Username != cfg.RegistryUser || got.Password != cfg.RegistryPass {
		t.Errorf("credential = %+v, want user %q pass %q", got, cfg.RegistryUser, cfg.RegistryPass)
	}
}

func TestRemoteTargetPasswordOnlyEnablesAuth(t *testing.T) {
	// Token-style auth: password (token) set, no username.
	cfg := config.Default
	cfg.Registry = "registry.example.com"
	cfg.RegistryPass = "token123"

	repo, err := RemoteTarget(cfg)
	if err != nil {
		t.Fatalf("RemoteTarget: %v", err)
	}
	client, ok := repo.Client.(*auth.Client)
	if !ok {
		t.Fatalf("repo.Client = %T, want *auth.Client", repo.Client)
	}
	got, err := client.Credential(context.Background(), cfg.Registry)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if got.Username != "" || got.Password != cfg.RegistryPass {
		t.Errorf("credential = %+v, want empty user, pass %q", got, cfg.RegistryPass)
	}
}

func TestLoadMissingTagIsFreshStart(t *testing.T) {
	// Empty store has no tag -> first run -> nil error, no files written.
	dst := t.TempDir()
	if err := Load(context.Background(), testConfig(dst), memory.New()); err != nil {
		t.Fatalf("Load on empty store = %v, want nil", err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty data dir, got %d entries", len(entries))
	}
}
