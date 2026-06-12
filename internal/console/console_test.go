package console

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
)

// One NATS container shared by the whole package; the testcontainers reaper
// removes it after the test process exits. Tests stay independent by each
// using their own subject (the test name as server id).
var (
	natsOnce sync.Once
	natsURL  string
	natsErr  error
)

func runNATSServer(t *testing.T) string {
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

func connect(t *testing.T) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(runNATSServer(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// stdinBuffer is a goroutine-safe io.Writer standing in for the server's stdin.
type stdinBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *stdinBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *stdinBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newTestSubscriber wires a Subscriber for the test's own subject and returns
// the stdin it writes into plus a requester on the same server.
func newTestSubscriber(t *testing.T) (*stdinBuffer, *nats.Conn) {
	t.Helper()
	stdin := &stdinBuffer{}
	nc := connect(t)
	sub, err := New(nc, t.Name(), stdin)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(sub.Close)
	return stdin, connect(t)
}

func request(t *testing.T, nc *nats.Conn, serverID, command string) string {
	t.Helper()
	msg, err := nc.Request(Subject(serverID), []byte(command), 5*time.Second)
	if err != nil {
		t.Fatalf("request %q: %v", command, err)
	}
	return string(msg.Data)
}

func TestNewNilConnIsNoop(t *testing.T) {
	sub, err := New(nil, "x", &stdinBuffer{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if sub != nil {
		t.Fatalf("subscriber = %v, want nil", sub)
	}
	sub.Close()
}

func TestCommandReachesStdin(t *testing.T) {
	stdin, nc := newTestSubscriber(t)

	if reply := request(t, nc, t.Name(), "say hello"); reply != "ok" {
		t.Errorf("reply = %q, want ok", reply)
	}
	if got, want := stdin.String(), "say hello\n"; got != want {
		t.Errorf("stdin = %q, want %q", got, want)
	}
}

func TestCommandsArriveInOrder(t *testing.T) {
	stdin, nc := newTestSubscriber(t)

	for _, command := range []string{"first", "second", "third"} {
		request(t, nc, t.Name(), command)
	}
	if got, want := stdin.String(), "first\nsecond\nthird\n"; got != want {
		t.Errorf("stdin = %q, want %q", got, want)
	}
}

func TestEmptyCommandRejected(t *testing.T) {
	stdin, nc := newTestSubscriber(t)

	if reply := request(t, nc, t.Name(), "   "); reply != "error: empty command" {
		t.Errorf("reply = %q, want empty-command error", reply)
	}
	if stdin.String() != "" {
		t.Errorf("stdin = %q, want empty", stdin.String())
	}
}

func TestMultilineCommandRejected(t *testing.T) {
	stdin, nc := newTestSubscriber(t)

	if reply := request(t, nc, t.Name(), "say hi\nstop"); reply != "error: command must be a single line" {
		t.Errorf("reply = %q, want single-line error", reply)
	}
	if stdin.String() != "" {
		t.Errorf("stdin = %q, want empty", stdin.String())
	}
}

func TestSurroundingWhitespaceTrimmed(t *testing.T) {
	stdin, nc := newTestSubscriber(t)

	request(t, nc, t.Name(), "  stop \n")
	if got, want := stdin.String(), "stop\n"; got != want {
		t.Errorf("stdin = %q, want %q", got, want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

func TestStdinWriteFailureReported(t *testing.T) {
	nc := connect(t)
	sub, err := New(nc, t.Name(), failingWriter{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(sub.Close)

	reply := request(t, connect(t), t.Name(), "stop")
	if reply != "error: write to server stdin: pipe closed" {
		t.Errorf("reply = %q, want stdin write error", reply)
	}
}

func TestCloseStopsAccepting(t *testing.T) {
	stdin := &stdinBuffer{}
	nc := connect(t)
	sub, err := New(nc, t.Name(), stdin)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sub.Close()

	if _, err := connect(t).Request(Subject(t.Name()), []byte("stop"), 500*time.Millisecond); err == nil {
		t.Fatal("expected no responder after Close, got reply")
	}
	if stdin.String() != "" {
		t.Errorf("stdin = %q, want empty", stdin.String())
	}
}

func TestSubject(t *testing.T) {
	tests := map[string]string{
		"plain-host":  "hytale.cmd.plain-host",
		"host.domain": "hytale.cmd.host_domain",
		"has space*>": "hytale.cmd.has_space__",
		"":            "hytale.cmd._",
	}
	for in, want := range tests {
		if got := Subject(in); got != want {
			t.Errorf("Subject(%q) = %q, want %q", in, got, want)
		}
	}
}
