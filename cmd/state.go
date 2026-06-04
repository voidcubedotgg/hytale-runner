package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/voidcubedotgg/hytale-server-runner/internal/config"
	"github.com/voidcubedotgg/hytale-server-runner/internal/state"
)

var stateCmd = &cobra.Command{
	Use:   "state",
	Short: "Manage server state in the OCI registry",
}

var statePullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull state from the registry into the data dir",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(v)
		if err != nil {
			return err
		}
		if err := ensureDataDir(cfg.DataDir); err != nil {
			return err
		}
		return loadState(cmd.Context(), cfg)
	},
}

var statePushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push the data dir to the registry as state",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(v)
		if err != nil {
			return err
		}
		return storeState(cmd.Context(), cfg)
	},
}

func init() {
	stateCmd.AddCommand(statePullCmd, statePushCmd)
}

// ensureDataDir creates the data dir if missing.
func ensureDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return fmt.Errorf("create data dir %s: %w", dir, err)
	}
	return nil
}

// loadState pulls state from the configured registry into the data dir.
func loadState(ctx context.Context, cfg config.Config) error {
	src, err := state.RemoteTarget(cfg)
	if err != nil {
		return err
	}
	return state.Load(ctx, cfg, src)
}

// storeState pushes the data dir to the configured registry.
func storeState(ctx context.Context, cfg config.Config) error {
	dst, err := state.RemoteTarget(cfg)
	if err != nil {
		return err
	}
	return state.Store(ctx, cfg, dst)
}
