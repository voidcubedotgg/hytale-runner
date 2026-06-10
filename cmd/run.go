package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"github.com/voidcubedotgg/hytale-runner/internal/server"
	"github.com/voidcubedotgg/hytale-runner/internal/status"
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

		reporter := newStatusReporter(ctx, cfg)
		defer reporter.Close()

		if err := ensureDataDir(cfg.DataDir); err != nil {
			reporter.SetError(err, 1)
			return err
		}

		var failure error
		if err := loadState(ctx, cfg); err != nil {
			zap.S().Errorf("load state failed: %v", err)
			failure = errors.Join(failure, fmt.Errorf("load state: %w", err))
		}

		reporter.Set(status.PhaseRunning)
		exitCode = server.Run(cfg)

		reporter.Set(status.PhaseStopping)
		if err := storeState(ctx, cfg); err != nil {
			zap.S().Errorf("store state failed: %v", err)
			failure = errors.Join(failure, fmt.Errorf("store state: %w", err))
		}

		if failure != nil {
			reporter.SetError(failure, exitCode)
		} else {
			reporter.SetStopped(exitCode)
		}
		return nil
	},
}

// newStatusReporter builds the NATS status reporter, falling back to the noop
// one when NATS is unconfigured or unreachable — status must never block a run.
func newStatusReporter(ctx context.Context, cfg config.Config) status.Reporter {
	reporter, err := status.New(ctx, status.Options{
		URL:      cfg.NATSURL,
		ServerID: cfg.ServerID,
		Bucket:   cfg.StatusBucket,
	})
	if err != nil {
		zap.S().Warnf("status reporting disabled: %v", err)
		return status.Noop()
	}
	return reporter
}
