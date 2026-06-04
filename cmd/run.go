package cmd

import (
	"github.com/spf13/cobra"
	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"github.com/voidcubedotgg/hytale-runner/internal/server"
	"go.uber.org/zap"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Load state, run the server, then store state",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(v)
		if err != nil {
			return err
		}
		ctx := cmd.Context()

		if err := ensureDataDir(cfg.DataDir); err != nil {
			return err
		}
		if err := loadState(ctx, cfg); err != nil {
			zap.S().Errorf("load state failed: %v", err)
		}

		exitCode = server.Run(cfg)

		if err := storeState(ctx, cfg); err != nil {
			zap.S().Errorf("store state failed: %v", err)
		}
		return nil
	},
}
