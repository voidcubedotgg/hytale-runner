package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"go.uber.org/zap"
)

// buildArgs assembles the java argv: JVM memory flags, extra JVM args, the jar
// and assets, then extra server args.
func buildArgs(cfg config.Config) []string {
	args := []string{"-Xms" + cfg.MinMemory, "-Xmx" + cfg.MaxMemory}
	args = append(args, cfg.ExtraJVMArgs...)
	args = append(args, "-jar", cfg.ServerJarPath, "--assets", cfg.AssetsPath)
	args = append(args, cfg.ExtraServerArgs...)
	return args
}

// Supervisor owns the Hytale server child process. It launches the JVM, waits
// for it out of band, and exposes graceful stop, liveness, and signal
// forwarding so a resident runtime can drive the process across lifecycle
// hooks. A Supervisor may be restarted after the child exits.
type Supervisor struct {
	cfg config.Config

	mu     sync.Mutex
	cmd    *exec.Cmd
	done   chan struct{}
	code   int
	exited bool
}

// NewSupervisor returns a Supervisor for the given config. The child is not
// started until Start is called.
func NewSupervisor(cfg config.Config) *Supervisor {
	return &Supervisor{cfg: cfg}
}

// Start launches the server child, inheriting stdio. It returns once the
// process has started; use Wait or Stop to observe its exit. Calling Start
// while a child is already running is an error; after the child exits Start may
// be called again to restart it.
func (s *Supervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && !s.exited {
		return errors.New("server already running")
	}

	cmd := exec.Command(s.cfg.JavaBin, buildArgs(s.cfg)...)
	cmd.Dir = s.cfg.DataDir
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	done := make(chan struct{})
	s.cmd = cmd
	s.done = done
	s.exited = false
	s.code = 0
	go func() {
		code := waitCode(cmd)
		s.mu.Lock()
		s.code = code
		s.exited = true
		s.mu.Unlock()
		close(done)
	}()
	return nil
}

// Wait blocks until the current child exits and returns its exit code. It
// returns 1 if the process was never started.
func (s *Supervisor) Wait() int {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return 1
	}
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
}

// Stop asks the child to shut down gracefully (SIGTERM) and waits up to grace
// for it to exit, escalating to SIGKILL on timeout. It returns the exit code.
func (s *Supervisor) Stop(grace time.Duration) int {
	s.mu.Lock()
	cmd, done, exited, code := s.cmd, s.done, s.exited, s.code
	s.mu.Unlock()
	if cmd == nil {
		return 1
	}
	if exited {
		return code
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(grace):
		zap.S().Warnf("server did not stop within %s, killing", grace)
		_ = cmd.Process.Kill()
		<-done
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
}

// Signal forwards a signal to the running child, if any.
func (s *Supervisor) Signal(sig os.Signal) {
	s.mu.Lock()
	cmd, exited := s.cmd, s.exited
	s.mu.Unlock()
	if cmd != nil && !exited {
		_ = cmd.Process.Signal(sig)
	}
}

// Alive reports whether a child process is currently running.
func (s *Supervisor) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil && !s.exited
}

// waitCode waits for cmd and maps its result to an exit code, preserving the
// server's own code (8 = restart for update) and mapping non-exit failures to 1.
func waitCode(cmd *exec.Cmd) int {
	err := cmd.Wait()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	zap.S().Errorf("server wait failed: %v", err)
	return 1
}

// Run launches the Hytale server, streaming stdio, and returns its exit code.
// SIGINT/SIGTERM are forwarded to the child so it shuts down gracefully (saving
// the world) while this process survives to store state afterwards. It is the
// one-shot entry point used by the `run` command; the resident runtime drives a
// Supervisor directly.
func Run(cfg config.Config) int {
	s := NewSupervisor(cfg)
	if err := s.Start(); err != nil {
		zap.S().Errorf("server start failed: %v", err)
		return 1
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		for sig := range sigCh {
			s.Signal(sig)
		}
	}()

	return s.Wait()
}
