package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"go.uber.org/zap"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
)

// fakeSup is a scripted supervisor. Wait blocks on waitCh so the monitor
// goroutine parks deterministically during a test.
type fakeSup struct {
	mu       sync.Mutex
	started  int
	alive    bool
	stopped  bool
	startErr error
	waitCh   chan int
	gotCfg   config.Config // config the factory built this supervisor with
}

func newFakeSup() *fakeSup { return &fakeSup{waitCh: make(chan int)} }

func (f *fakeSup) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.started++
	f.alive = true
	return nil
}

func (f *fakeSup) Wait() int { return <-f.waitCh }

func (f *fakeSup) Stop(time.Duration) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	f.alive = false
	return 0
}

func (f *fakeSup) Alive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive
}

func (f *fakeSup) starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started
}

func (f *fakeSup) config() config.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotCfg
}

func newTestServer(t *testing.T, sup *fakeSup, tgt func(config.Config) (oras.Target, error)) *Server {
	t.Helper()
	cfg := config.Default
	cfg.DataDir = t.TempDir()
	return &Server{
		cfg:    cfg,
		log:    zap.NewNop().Sugar(),
		grace:  time.Second,
		target: tgt,
		// The run hook builds the supervisor from the effective config; hand back
		// the shared fake and record the config it was built with.
		newSup: func(c config.Config) supervisor {
			sup.mu.Lock()
			sup.gotCfg = c
			sup.mu.Unlock()
			return sup
		},
		state: statePending,
	}
}

func staticTarget(tgt oras.Target) func(config.Config) (oras.Target, error) {
	return func(config.Config) (oras.Target, error) { return tgt, nil }
}

func post(t *testing.T, h http.Handler, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRunHookStartsAndPullsState(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))
	t.Cleanup(func() { close(sup.waitCh) })

	rec := post(t, s.Handler(), HookBase+"run", `{"microvmId":"mvm-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("run = %d, want 200", rec.Code)
	}
	if sup.starts() != 1 {
		t.Errorf("started = %d, want 1", sup.starts())
	}
	if s.state != stateRunning {
		t.Errorf("state = %d, want running", s.state)
	}
}

func TestRunHookIdempotent(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))
	t.Cleanup(func() { close(sup.waitCh) })

	post(t, s.Handler(), HookBase+"run", "")
	rec := post(t, s.Handler(), HookBase+"run", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("second run = %d, want 200", rec.Code)
	}
	if sup.starts() != 1 {
		t.Errorf("started = %d, want 1 (idempotent)", sup.starts())
	}
}

func TestRunHookPullFailure(t *testing.T) {
	sup := newFakeSup()
	wantErr := errors.New("registry down")
	s := newTestServer(t, sup, func(config.Config) (oras.Target, error) { return nil, wantErr })

	rec := post(t, s.Handler(), HookBase+"run", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("run = %d, want 500", rec.Code)
	}
	if sup.starts() != 0 {
		t.Errorf("started = %d, want 0 when pull fails", sup.starts())
	}
	if s.state != statePending {
		t.Errorf("state = %d, want pending", s.state)
	}
}

func TestRunHookStartFailure(t *testing.T) {
	sup := newFakeSup()
	sup.startErr = errors.New("no jvm")
	s := newTestServer(t, sup, staticTarget(memory.New()))

	rec := post(t, s.Handler(), HookBase+"run", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("run = %d, want 500", rec.Code)
	}
	if s.state != statePending {
		t.Errorf("state = %d, want pending after start failure", s.state)
	}
}

func TestTerminateHookStopsAndPushes(t *testing.T) {
	sup := newFakeSup()
	store := memory.New()
	s := newTestServer(t, sup, staticTarget(store))
	t.Cleanup(func() { close(sup.waitCh) })

	post(t, s.Handler(), HookBase+"run", "")
	// A world file so Store has something to push.
	if err := os.WriteFile(filepath.Join(s.cfg.DataDir, "world.dat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := post(t, s.Handler(), HookBase+"terminate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate = %d, want 200", rec.Code)
	}
	if !sup.stopped {
		t.Error("supervisor not stopped on terminate")
	}
	if s.state != stateTerminated {
		t.Errorf("state = %d, want terminated", s.state)
	}
	if _, err := store.Resolve(context.Background(), s.cfg.StateTag); err != nil {
		t.Errorf("state not pushed: resolve %q: %v", s.cfg.StateTag, err)
	}
}

func TestTerminateEmptyStateIsOK(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))
	t.Cleanup(func() { close(sup.waitCh) })

	post(t, s.Handler(), HookBase+"run", "")
	rec := post(t, s.Handler(), HookBase+"terminate", "") // empty data dir -> ErrNoState tolerated
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate empty = %d, want 200", rec.Code)
	}
	if s.state != stateTerminated {
		t.Errorf("state = %d, want terminated", s.state)
	}
}

func TestTerminateIdempotent(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))
	t.Cleanup(func() { close(sup.waitCh) })

	post(t, s.Handler(), HookBase+"run", "")
	post(t, s.Handler(), HookBase+"terminate", "")
	rec := post(t, s.Handler(), HookBase+"terminate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("second terminate = %d, want 200", rec.Code)
	}
}

func TestHealthReflectsLiveness(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))
	t.Cleanup(func() { close(sup.waitCh) })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("health before run = %d, want 503", rec.Code)
	}

	post(t, s.Handler(), HookBase+"run", "")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("health after run = %d, want 200", rec.Code)
	}
}

func TestReadyAlwaysOK(t *testing.T) {
	s := newTestServer(t, newFakeSup(), staticTarget(memory.New()))
	rec := post(t, s.Handler(), HookBase+"ready", "")
	if rec.Code != http.StatusOK {
		t.Errorf("ready = %d, want 200", rec.Code)
	}
}

func TestSuspendResumeTransitions(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))
	t.Cleanup(func() { close(sup.waitCh) })

	post(t, s.Handler(), HookBase+"run", "")
	post(t, s.Handler(), HookBase+"suspend", "")
	if s.state != stateSuspended {
		t.Errorf("state after suspend = %d, want suspended", s.state)
	}
	post(t, s.Handler(), HookBase+"resume", "")
	if s.state != stateRunning {
		t.Errorf("state after resume = %d, want running", s.state)
	}
}

func TestMonitorRestartsOnExit8(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))

	post(t, s.Handler(), HookBase+"run", "")
	sup.waitCh <- restartExitCode // first exit: request restart

	deadline := time.After(2 * time.Second)
	for sup.starts() < 2 {
		select {
		case <-deadline:
			t.Fatalf("started = %d, want 2 (restart on exit 8)", sup.starts())
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Let the monitor park again, then unblock it for cleanup.
	close(sup.waitCh)
}

func TestRunHookAppliesPayloadOverrides(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))
	t.Cleanup(func() { close(sup.waitCh) })

	payload := `{"microvmId":"mvm-1","runHookPayload":"{\"maxMemory\":\"8G\",\"extraJvmArgs\":[\"-XX:+UseZGC\"]}"}`
	rec := post(t, s.Handler(), HookBase+"run", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("run = %d, want 200", rec.Code)
	}
	got := sup.config()
	if got.MaxMemory != "8G" {
		t.Errorf("MaxMemory = %q, want 8G", got.MaxMemory)
	}
	if len(got.ExtraJVMArgs) != 1 || got.ExtraJVMArgs[0] != "-XX:+UseZGC" {
		t.Errorf("ExtraJVMArgs = %v, want [-XX:+UseZGC]", got.ExtraJVMArgs)
	}
	// Unset fields keep the base default.
	if got.MinMemory != config.Default.MinMemory {
		t.Errorf("MinMemory = %q, want base %q", got.MinMemory, config.Default.MinMemory)
	}
}

func TestRunHookEmptyPayloadUsesBaseConfig(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))
	t.Cleanup(func() { close(sup.waitCh) })

	rec := post(t, s.Handler(), HookBase+"run", `{"microvmId":"mvm-1","runHookPayload":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("run = %d, want 200", rec.Code)
	}
	if got := sup.config(); got.MaxMemory != config.Default.MaxMemory {
		t.Errorf("MaxMemory = %q, want base %q", got.MaxMemory, config.Default.MaxMemory)
	}
}

func TestRunHookMalformedPayloadFails(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))

	payload := `{"microvmId":"mvm-1","runHookPayload":"{not json"}`
	rec := post(t, s.Handler(), HookBase+"run", payload)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("run = %d, want 500", rec.Code)
	}
	if sup.starts() != 0 {
		t.Errorf("started = %d, want 0 on bad payload", sup.starts())
	}
	if s.state != statePending {
		t.Errorf("state = %d, want pending", s.state)
	}
}

func TestRunHookRejectsOutOfScopeField(t *testing.T) {
	sup := newFakeSup()
	s := newTestServer(t, sup, staticTarget(memory.New()))

	// javaBin is not an overridable field -> DisallowUnknownFields rejects it.
	payload := `{"microvmId":"mvm-1","runHookPayload":"{\"javaBin\":\"/evil\"}"}`
	rec := post(t, s.Handler(), HookBase+"run", payload)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("run = %d, want 500", rec.Code)
	}
	if sup.starts() != 0 {
		t.Errorf("started = %d, want 0", sup.starts())
	}
}

func TestNewParsesGrace(t *testing.T) {
	cfg := config.Default
	cfg.TerminateGrace = "not-a-duration"
	if _, err := New(cfg); err == nil {
		t.Fatal("New with bad terminate-grace = nil error, want error")
	}
	cfg.TerminateGrace = "10s"
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.grace != 10*time.Second {
		t.Errorf("grace = %s, want 10s", s.grace)
	}
}
