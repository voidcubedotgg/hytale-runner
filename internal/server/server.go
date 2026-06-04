package server

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"go.uber.org/zap"
)

// Run launches the Hytale server, streaming stdio, and returns its exit code.
// SIGINT/SIGTERM are forwarded to the child so it shuts down gracefully (saving
// the world) while this process survives to store state afterwards.
func Run(cfg config.Config) int {
	args := []string{"-Xms" + cfg.MinMemory, "-Xmx" + cfg.MaxMemory}
	args = append(args, cfg.ExtraJVMArgs...)
	args = append(args, "-jar", cfg.ServerJarPath, "--assets", cfg.AssetsPath)
	args = append(args, cfg.ExtraServerArgs...)

	cmd := exec.Command(cfg.JavaBin, args...)
	cmd.Dir = cfg.DataDir
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		zap.S().Errorf("server start failed: %v", err)
		return 1
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		for sig := range sigCh {
			_ = cmd.Process.Signal(sig)
		}
	}()

	err := cmd.Wait()
	if err == nil {
		return 0
	}
	// ExitError = server ran and returned non-zero (8 = restart for update). Keep its code.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	zap.S().Errorf("server wait failed: %v", err)
	return 1
}
