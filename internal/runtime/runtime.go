// Package runtime hosts the AWS Lambda MicroVM lifecycle hook server. It keeps
// the runner resident for the MicroVM's whole life, exposing the run, suspend,
// resume, and terminate hooks and mapping them onto the OCI state round-trip:
// pull the world on run, push it back on terminate.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"github.com/voidcubedotgg/hytale-runner/internal/server"
	"github.com/voidcubedotgg/hytale-runner/internal/state"
	"go.uber.org/zap"
	"oras.land/oras-go/v2"
)

// HookBase is the path prefix Lambda posts lifecycle hooks to.
const HookBase = "/aws/lambda-microvms/runtime/v1/"

// restartExitCode is the server exit code that requests an in-place restart.
const restartExitCode = 8

type lifecycleState int

const (
	statePending lifecycleState = iota
	stateRunning
	stateSuspended
	stateTerminated
)

// targetFunc builds the OCI target for a config. It is the seam tests replace
// with an in-memory store.
type targetFunc func(cfg config.Config) (oras.Target, error)

// supervisor is the subset of server.Supervisor the runtime drives; a narrow
// interface keeps the hook handlers testable with a fake process.
type supervisor interface {
	Start() error
	Wait() int
	Stop(grace time.Duration) int
	Alive() bool
}

// Server serves the MicroVM lifecycle hooks and owns the game process.
type Server struct {
	cfg    config.Config
	log    *zap.SugaredLogger
	grace  time.Duration
	target targetFunc
	sup    supervisor

	mu    sync.Mutex // guards state and serializes lifecycle transitions
	state lifecycleState
}

// New builds a Server from config, wiring the real registry target and process
// supervisor.
func New(cfg config.Config) (*Server, error) {
	grace, err := time.ParseDuration(cfg.TerminateGrace)
	if err != nil {
		return nil, fmt.Errorf("parse terminate-grace %q: %w", cfg.TerminateGrace, err)
	}
	s := &Server{
		cfg:    cfg,
		log:    zap.S(),
		grace:  grace,
		target: func(c config.Config) (oras.Target, error) { return state.RemoteTarget(c) },
		sup:    server.NewSupervisor(cfg),
		state:  statePending,
	}
	return s, nil
}

func (s *Server) lock()   { s.mu.Lock() }
func (s *Server) unlock() { s.mu.Unlock() }

// Handler returns the HTTP mux exposing the lifecycle hooks plus a health
// endpoint. Hooks are trusted (delivered by the Lambda control plane behind a
// JWE-authenticating proxy), so no application auth is applied here.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+HookBase+"run", s.handleRun)
	mux.HandleFunc("POST "+HookBase+"suspend", s.handleSuspend)
	mux.HandleFunc("POST "+HookBase+"resume", s.handleResume)
	mux.HandleFunc("POST "+HookBase+"terminate", s.handleTerminate)
	mux.HandleFunc("POST "+HookBase+"ready", s.handleReady)
	mux.HandleFunc("GET /health", s.handleHealth)
	return mux
}

// ListenAndServe serves the hooks on the configured port until ctx is canceled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{Addr: ":" + s.cfg.HookPort, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), s.grace)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type runRequest struct {
	MicrovmID      string `json:"microvmId"`
	RunHookPayload string `json:"runHookPayload"`
}

// handleRun pulls the tenant world from the registry and starts the server.
// Lambda only forwards external traffic after this returns 200.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	s.lock()
	defer s.unlock()
	if s.state == stateRunning {
		w.WriteHeader(http.StatusOK) // idempotent
		return
	}

	var req runRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // empty/absent body is fine
	s.log.Infow("run hook", "microvmId", req.MicrovmID)

	if err := state.EnsureDataDir(s.cfg.DataDir); err != nil {
		s.fail(w, "ensure data dir", err)
		return
	}
	src, err := s.target(s.cfg)
	if err != nil {
		s.fail(w, "registry target", err)
		return
	}
	if err := state.Load(r.Context(), s.cfg, src); err != nil {
		s.fail(w, "pull state", err)
		return
	}
	if err := s.sup.Start(); err != nil {
		s.fail(w, "start server", err)
		return
	}

	s.state = stateRunning
	go s.monitor()
	w.WriteHeader(http.StatusOK)
}

// handleTerminate stops the server gracefully and pushes the final state to the
// registry. This is the durability point: disk survives suspend/resume, but
// termination is terminal.
func (s *Server) handleTerminate(w http.ResponseWriter, r *http.Request) {
	s.lock()
	defer s.unlock()
	if s.state == stateTerminated {
		w.WriteHeader(http.StatusOK) // idempotent
		return
	}

	if s.state == stateRunning || s.state == stateSuspended {
		code := s.sup.Stop(s.grace)
		s.log.Infow("server stopped", "code", code)
	}
	s.state = stateTerminated

	dst, err := s.target(s.cfg)
	if err != nil {
		s.fail(w, "registry target", err)
		return
	}
	if err := state.Store(r.Context(), s.cfg, dst); err != nil {
		if errors.Is(err, state.ErrNoState) {
			s.log.Warn("no state to store on terminate")
		} else {
			s.fail(w, "push state", err)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleSuspend records the suspend transition. Disk and memory are checkpointed
// by Lambda, so the frozen server resumes as-is; there is nothing to flush in v1.
func (s *Server) handleSuspend(w http.ResponseWriter, _ *http.Request) {
	s.lock()
	defer s.unlock()
	if s.state == stateRunning {
		s.state = stateSuspended
	}
	s.log.Info("suspend hook")
	w.WriteHeader(http.StatusOK)
}

// handleResume records the resume transition. The MicroVM stays SUSPENDED until
// this returns 200.
func (s *Server) handleResume(w http.ResponseWriter, _ *http.Request) {
	s.lock()
	defer s.unlock()
	if s.state == stateSuspended {
		s.state = stateRunning
	}
	s.log.Info("resume hook")
	w.WriteHeader(http.StatusOK)
}

// handleReady gates snapshot capture at image-build time: the runner is ready as
// soon as it is serving, since the world is only loaded later in the run hook.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleHealth reports game-server liveness.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if s.sup.Alive() {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
}

// monitor watches the running server and restarts it in place on the
// restart-for-update exit code. It exits once the process is stopped
// intentionally (terminate) or exits for any other reason.
func (s *Server) monitor() {
	for {
		code := s.sup.Wait()
		s.lock()
		if s.state != stateRunning {
			s.unlock()
			return
		}
		if code == restartExitCode {
			s.log.Info("server requested restart (exit 8)")
			if err := s.sup.Start(); err != nil {
				s.log.Errorw("restart failed", "err", err)
				s.unlock()
				return
			}
			s.unlock()
			continue
		}
		s.log.Warnw("server exited unexpectedly", "code", code)
		s.unlock()
		return
	}
}

func (s *Server) fail(w http.ResponseWriter, msg string, err error) {
	s.log.Errorw(msg+" failed", "err", err)
	http.Error(w, msg+": "+err.Error(), http.StatusInternalServerError)
}
