// Package console exposes the game server's console over NATS request-reply.
// Each runner subscribes to its own subject; a request's payload is one
// console command, which is written to the server's stdin. The reply is "ok"
// once the command was handed to the server, or "error: ..." when it wasn't —
// the server itself gives no per-command answer, so this acks delivery, not
// execution.
package console

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// SubjectPrefix is the namespace for per-server command subjects.
const SubjectPrefix = "hytale.cmd."

// Subject returns the request-reply subject for a server id.
func Subject(serverID string) string {
	return SubjectPrefix + sanitizeToken(serverID)
}

// Subscriber feeds NATS-delivered console commands into the server's stdin.
// A nil Subscriber is valid and does nothing, for when NATS is not configured.
type Subscriber struct {
	sub *nats.Subscription
}

// New subscribes to the server's command subject and writes each request's
// payload to stdin. A nil connection yields a nil (noop) Subscriber.
func New(nc *nats.Conn, serverID string, stdin io.Writer) (*Subscriber, error) {
	if nc == nil {
		return nil, nil
	}
	subject := Subject(serverID)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		respond(msg, handle(stdin, msg.Data))
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe %q: %w", subject, err)
	}
	return &Subscriber{sub: sub}, nil
}

// Close stops accepting commands. Safe on a nil Subscriber.
func (s *Subscriber) Close() {
	if s == nil {
		return
	}
	if err := s.sub.Unsubscribe(); err != nil {
		zap.S().Warnf("console: unsubscribe: %v", err)
	}
}

// handle validates one command and writes it to stdin, returning the reply.
func handle(stdin io.Writer, data []byte) string {
	command := strings.TrimSpace(string(data))
	if command == "" {
		return "error: empty command"
	}
	// One request is one command; an embedded newline would smuggle extras.
	if strings.ContainsAny(command, "\r\n") {
		return "error: command must be a single line"
	}
	if _, err := io.WriteString(stdin, command+"\n"); err != nil {
		return "error: write to server stdin: " + err.Error()
	}
	return "ok"
}

// respond replies when the request expects one; fire-and-forget publishes don't.
func respond(msg *nats.Msg, reply string) {
	if msg.Reply == "" {
		return
	}
	if err := msg.Respond([]byte(reply)); err != nil {
		zap.S().Warnf("console: respond: %v", err)
	}
}

var invalidTokenChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// sanitizeToken maps a server id onto a single valid NATS subject token
// (no dots, wildcards, or whitespace).
func sanitizeToken(id string) string {
	token := invalidTokenChars.ReplaceAllString(id, "_")
	if token == "" {
		return "_"
	}
	return token
}
