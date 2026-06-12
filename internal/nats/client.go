package nats

import (
	"errors"

	"github.com/nats-io/nats.go"
	"github.com/voidcubedotgg/hytale-runner/internal/config"
	"go.uber.org/zap"
)

type NatsClient struct {
	cfg config.Config
}

func NewNatsClient(cfg config.Config) *NatsClient {
	return &NatsClient{
		cfg,
	}
}

// connectNATS opens the shared NATS connection. NATS is mandatory, so a
// missing URL or failed connect aborts the run.
func (c *NatsClient) Connect() (*nats.Conn, error) {
	if c.cfg.NATSURL == "" {
		return nil, errors.New("NATS is required: set --nats-url (or HYRUN_NATS_URL)")
	}
	return nats.Connect(c.cfg.NATSURL, nats.Name("hytale-runner/"+c.cfg.ServerID))
}

// drainNATS flushes pending publishes and closes the connection.
func (c *NatsClient) Drain(nc *nats.Conn) {
	if err := nc.Drain(); err != nil {
		zap.S().Warnf("drain NATS connection: %v", err)
	}
}
