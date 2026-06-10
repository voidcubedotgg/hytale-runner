// Package status publishes the runner's lifecycle phase to a NATS JetStream
// key-value bucket so a control plane can watch a fleet of servers. Each
// runner owns one key (its server id); a heartbeat re-puts the value so the
// bucket TTL acts as a liveness check — a key that expires means the runner
// died without reporting.
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

// Phase is a step in the runner lifecycle.
type Phase string

const (
	PhaseStarting Phase = "starting"
	PhaseRunning  Phase = "running"
	PhaseStopping Phase = "stopping"
	PhaseStopped  Phase = "stopped"
	PhaseError    Phase = "error"
)

// Status is the JSON value stored under the server's key.
type Status struct {
	ServerID  string    `json:"server-id"`
	Phase     Phase     `json:"phase"`
	ExitCode  *int      `json:"exit-code,omitempty"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated-at"`
}

// Reporter publishes lifecycle phases. Implementations never fail the run:
// publish errors are logged, not returned. SetStopped and SetError are the
// terminal phases; SetError carries both the failure and the server's exit code.
type Reporter interface {
	Set(phase Phase)
	SetStopped(exitCode int)
	SetError(err error, exitCode int)
	Close()
}

// Noop returns a Reporter that does nothing, for when NATS is not configured.
func Noop() Reporter { return noop{} }

type noop struct{}

func (noop) Set(Phase)           {}
func (noop) SetStopped(int)      {}
func (noop) SetError(error, int) {}
func (noop) Close()              {}

// DefaultHeartbeat is how often the current status is re-published when
// Options.Heartbeat is unset. The bucket TTL is three heartbeats.
const DefaultHeartbeat = 5 * time.Second

const opTimeout = 5 * time.Second

// Options configures a NATS-backed Reporter.
type Options struct {
	URL       string
	ServerID  string
	Bucket    string
	Heartbeat time.Duration
}

type natsReporter struct {
	nc        *nats.Conn
	kv        jetstream.KeyValue
	key       string
	mu        sync.Mutex
	cur       Status
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// New connects to NATS and returns a Reporter that immediately publishes
// PhaseStarting and keeps the key alive via heartbeats. An empty URL yields
// the noop Reporter so callers don't have to special-case "NATS disabled".
func New(ctx context.Context, opts Options) (Reporter, error) {
	if opts.URL == "" {
		return Noop(), nil
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = DefaultHeartbeat
	}

	nc, err := nats.Connect(opts.URL, nats.Name("hytale-runner/"+opts.ServerID))
	if err != nil {
		return nil, fmt.Errorf("connect to NATS %s: %w", opts.URL, err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	kvCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	kv, err := js.CreateOrUpdateKeyValue(kvCtx, jetstream.KeyValueConfig{
		Bucket: opts.Bucket,
		TTL:    3 * opts.Heartbeat,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("key-value bucket %q: %w", opts.Bucket, err)
	}

	r := &natsReporter{
		nc:   nc,
		kv:   kv,
		key:  sanitizeKey(opts.ServerID),
		cur:  Status{ServerID: opts.ServerID, Phase: PhaseStarting},
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	r.publish()
	go r.heartbeat(opts.Heartbeat)
	return r, nil
}

func (r *natsReporter) Set(phase Phase) {
	r.mu.Lock()
	r.cur.Phase = phase
	r.cur.ExitCode = nil
	r.cur.Error = ""
	r.mu.Unlock()
	r.publish()
}

func (r *natsReporter) SetStopped(exitCode int) {
	r.mu.Lock()
	r.cur.Phase = PhaseStopped
	r.cur.ExitCode = &exitCode
	r.cur.Error = ""
	r.mu.Unlock()
	r.publish()
}

func (r *natsReporter) SetError(err error, exitCode int) {
	r.mu.Lock()
	r.cur.Phase = PhaseError
	r.cur.ExitCode = &exitCode
	r.cur.Error = err.Error()
	r.mu.Unlock()
	r.publish()
}

// Close stops the heartbeat and drains the connection. The last published
// status stays visible until the bucket TTL expires it.
func (r *natsReporter) Close() {
	r.closeOnce.Do(func() {
		close(r.stop)
		<-r.done
		if err := r.nc.Drain(); err != nil {
			zap.S().Warnf("status: drain NATS connection: %v", err)
		}
	})
}

func (r *natsReporter) heartbeat(interval time.Duration) {
	defer close(r.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.publish()
		case <-r.stop:
			return
		}
	}
}

func (r *natsReporter) publish() {
	r.mu.Lock()
	r.cur.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(r.cur)
	r.mu.Unlock()
	if err != nil {
		zap.S().Warnf("status: marshal: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if _, err := r.kv.Put(ctx, r.key, data); err != nil {
		zap.S().Warnf("status: publish: %v", err)
	}
}

var invalidKeyChars = regexp.MustCompile(`[^-/_=.a-zA-Z0-9]`)

// sanitizeKey maps a server id onto the characters NATS KV allows in a key.
func sanitizeKey(id string) string {
	key := invalidKeyChars.ReplaceAllString(id, "_")
	if key == "" || strings.HasPrefix(key, ".") {
		key = "_" + key
	}
	return key
}
