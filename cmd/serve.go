package cmd

import (
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"github.com/voidcubedotgg/hytale-runner/internal/runtime"
	"go.uber.org/zap"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the AWS MicroVM lifecycle hooks (starts the server on the run hook)",
	Long: "serve runs the resident lifecycle hook server for AWS Lambda MicroVMs. " +
		"It stays up for the MicroVM's lifetime and drives the game process from the " +
		"run/suspend/resume/terminate hooks, pulling state on run and pushing it on terminate. " +
		"The game server is not started until the run hook fires.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(v)
		if err != nil {
			return err
		}
		srv, err := runtime.New(cfg)
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		zap.S().Infof("serving MicroVM lifecycle hooks on :%s", cfg.HookPort)
		return srv.ListenAndServe(ctx)
	},
}
