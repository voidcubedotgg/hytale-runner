package status

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
)

// One NATS container (JetStream on by default) shared by the whole package;
// the testcontainers reaper removes it after the test process exits. Tests
// stay independent by each using their own bucket (the test name).
var (
	natsOnce sync.Once
	natsURL  string
	natsErr  error
)

func runJetStreamServer(t *testing.T) string {
	t.Helper()
	natsOnce.Do(func() {
		ctx := context.Background()
		ctr, err := natscontainer.Run(ctx, "nats:2.11-alpine")
		if err != nil {
			natsErr = err
			return
		}
		natsURL, natsErr = ctr.ConnectionString(ctx)
	})
	if natsErr != nil {
		t.Fatalf("start NATS container: %v", natsErr)
	}
	return natsURL
}

// getStatus reads and unmarshals the status stored under key in bucket.
func getStatus(t *testing.T, url, bucket, key string) (Status, uint64) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kv, err := js.KeyValue(ctx, bucket)
	if err != nil {
		t.Fatalf("bucket %q: %v", bucket, err)
	}
	entry, err := kv.Get(ctx, key)
	if err != nil {
		t.Fatalf("get %q: %v", key, err)
	}
	var s Status
	if err := json.Unmarshal(entry.Value(), &s); err != nil {
		t.Fatalf("unmarshal %q: %v", entry.Value(), err)
	}
	return s, entry.Revision()
}

// newTestReporter builds a Reporter against the shared container, using the
// test name as the bucket so tests cannot interfere with each other.
func newTestReporter(t *testing.T, url string, heartbeat time.Duration) Reporter {
	t.Helper()
	rep, err := New(context.Background(), Options{
		URL:       url,
		ServerID:  "test-server",
		Bucket:    t.Name(),
		Heartbeat: heartbeat,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(rep.Close)
	return rep
}

func TestNewEmptyURLIsNoop(t *testing.T) {
	rep, err := New(context.Background(), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := rep.(noop); !ok {
		t.Fatalf("reporter = %T, want noop", rep)
	}
	rep.Set(PhaseRunning)
	rep.SetStopped(0)
	rep.SetError(errors.New("boom"), 1)
	rep.Close()
}

func TestNewUnreachableURL(t *testing.T) {
	_, err := New(context.Background(), Options{URL: "nats://127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected error for unreachable NATS, got nil")
	}
}

func TestNewPublishesStarting(t *testing.T) {
	url := runJetStreamServer(t)
	newTestReporter(t, url, time.Second)

	s, _ := getStatus(t, url, t.Name(), "test-server")
	if s.Phase != PhaseStarting {
		t.Errorf("phase = %q, want %q", s.Phase, PhaseStarting)
	}
	if s.ServerID != "test-server" {
		t.Errorf("server id = %q, want %q", s.ServerID, "test-server")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("updated-at is zero")
	}
}

func TestSetPhases(t *testing.T) {
	url := runJetStreamServer(t)
	rep := newTestReporter(t, url, time.Second)

	for _, phase := range []Phase{PhaseRunning, PhaseStopping} {
		rep.Set(phase)
		s, _ := getStatus(t, url, t.Name(), "test-server")
		if s.Phase != phase {
			t.Errorf("phase = %q, want %q", s.Phase, phase)
		}
		if s.ExitCode != nil {
			t.Errorf("exit code = %d, want nil", *s.ExitCode)
		}
		if s.Error != "" {
			t.Errorf("error = %q, want empty", s.Error)
		}
	}
}

func TestSetErrorCarriesMessageAndExitCode(t *testing.T) {
	url := runJetStreamServer(t)
	rep := newTestReporter(t, url, time.Second)

	rep.SetError(errors.New("store state: registry unreachable"), 8)
	s, _ := getStatus(t, url, t.Name(), "test-server")
	if s.Phase != PhaseError {
		t.Errorf("phase = %q, want %q", s.Phase, PhaseError)
	}
	if s.Error != "store state: registry unreachable" {
		t.Errorf("error = %q, want %q", s.Error, "store state: registry unreachable")
	}
	if s.ExitCode == nil || *s.ExitCode != 8 {
		t.Errorf("exit code = %v, want 8", s.ExitCode)
	}
}

func TestSetClearsPreviousError(t *testing.T) {
	url := runJetStreamServer(t)
	rep := newTestReporter(t, url, time.Second)

	rep.SetError(errors.New("transient"), 1)
	rep.Set(PhaseRunning)
	s, _ := getStatus(t, url, t.Name(), "test-server")
	if s.Error != "" {
		t.Errorf("error = %q, want empty after Set", s.Error)
	}
	if s.ExitCode != nil {
		t.Errorf("exit code = %d, want nil after Set", *s.ExitCode)
	}
}

func TestSetStoppedCarriesExitCode(t *testing.T) {
	url := runJetStreamServer(t)
	rep := newTestReporter(t, url, time.Second)

	rep.SetStopped(8)
	s, _ := getStatus(t, url, t.Name(), "test-server")
	if s.Phase != PhaseStopped {
		t.Errorf("phase = %q, want %q", s.Phase, PhaseStopped)
	}
	if s.ExitCode == nil || *s.ExitCode != 8 {
		t.Errorf("exit code = %v, want 8", s.ExitCode)
	}
}

func TestHeartbeatRepublishes(t *testing.T) {
	url := runJetStreamServer(t)
	newTestReporter(t, url, 50*time.Millisecond)

	_, first := getStatus(t, url, t.Name(), "test-server")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, rev := getStatus(t, url, t.Name(), "test-server"); rev > first {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("heartbeat never republished the status")
}

func TestKeyExpiresAfterClose(t *testing.T) {
	url := runJetStreamServer(t)
	rep := newTestReporter(t, url, 100*time.Millisecond)
	rep.SetStopped(0)
	rep.Close()

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kv, err := js.KeyValue(ctx, t.Name())
	if err != nil {
		t.Fatalf("bucket: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := kv.Get(ctx, "test-server"); errors.Is(err, jetstream.ErrKeyNotFound) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("key never expired after Close")
}

func TestCloseIsIdempotent(t *testing.T) {
	url := runJetStreamServer(t)
	rep := newTestReporter(t, url, time.Second)
	rep.Close()
	rep.Close()
}

func TestSanitizeKey(t *testing.T) {
	tests := map[string]string{
		"plain-host":     "plain-host",
		"host.domain":    "host.domain",
		"host:5001/odd ": "host_5001/odd_",
		".hidden":        "_.hidden",
		"":               "_",
	}
	for in, want := range tests {
		if got := sanitizeKey(in); got != want {
			t.Errorf("sanitizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}
