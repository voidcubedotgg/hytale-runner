package state

import (
	"context"
	"errors"
	"fmt"
	"os"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"go.uber.org/zap"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
)

// ErrNoState is returned by Store when the data dir holds nothing to push.
var ErrNoState = errors.New("no state to store")

// RemoteTarget returns the registry handle for the server state artifact.
// Commands pass the result to Load/Store; tests pass an in-memory target instead.
func RemoteTarget(cfg config.Config) (*remote.Repository, error) {
	repo, err := remote.NewRepository(cfg.Registry + "/" + cfg.StateRepo)
	if err != nil {
		return nil, fmt.Errorf("init repository: %w", err)
	}
	repo.PlainHTTP = cfg.PlainHTTP
	return repo, nil
}

// Store packs the mutable state under cfg.DataDir and pushes it to dst tagged
// cfg.StateTag. An empty data dir yields ErrNoState.
func Store(ctx context.Context, cfg config.Config, dst oras.Target) error {
	fs, err := file.New(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open file store: %w", err)
	}
	defer fs.Close()

	entries, err := os.ReadDir(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("read data dir %s: %w", cfg.DataDir, err)
	}

	var layers []ocispec.Descriptor
	for _, entry := range entries {
		desc, err := fs.Add(ctx, entry.Name(), "", "")
		if err != nil {
			return fmt.Errorf("add %q: %w", entry.Name(), err)
		}
		layers = append(layers, desc)
	}
	if len(layers) == 0 {
		return ErrNoState
	}

	manifestDesc, err := oras.PackManifest(ctx, fs, oras.PackManifestVersion1_1, cfg.StateArtifact, oras.PackManifestOptions{
		Layers: layers,
	})
	if err != nil {
		return fmt.Errorf("pack manifest: %w", err)
	}
	if err := fs.Tag(ctx, manifestDesc, cfg.StateTag); err != nil {
		return fmt.Errorf("tag manifest: %w", err)
	}

	if _, err := oras.Copy(ctx, fs, cfg.StateTag, dst, cfg.StateTag, oras.DefaultCopyOptions); err != nil {
		return fmt.Errorf("push %s:%s: %w", cfg.StateRepo, cfg.StateTag, err)
	}

	zap.S().Infof("stored state: %s/%s:%s (%d layers)", cfg.Registry, cfg.StateRepo, cfg.StateTag, len(layers))
	return nil
}

// Load pulls cfg.StateTag from src into cfg.DataDir, restoring the mutable
// state. A missing tag (first run) is not an error.
func Load(ctx context.Context, cfg config.Config, src oras.ReadOnlyTarget) error {
	fs, err := file.New(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open file store: %w", err)
	}
	defer fs.Close()

	desc, err := oras.Copy(ctx, src, cfg.StateTag, fs, cfg.StateTag, oras.DefaultCopyOptions)
	if err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			zap.S().Infof("no stored state at %s/%s:%s, starting fresh", cfg.Registry, cfg.StateRepo, cfg.StateTag)
			return nil
		}
		return fmt.Errorf("pull %s:%s: %w", cfg.StateRepo, cfg.StateTag, err)
	}

	zap.S().Infof("loaded state: %s/%s:%s (%s)", cfg.Registry, cfg.StateRepo, cfg.StateTag, desc.Digest)
	return nil
}
