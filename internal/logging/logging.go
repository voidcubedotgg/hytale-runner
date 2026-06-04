package logging

import (
	"fmt"

	"go.uber.org/zap"
)

// New builds a production-preset zap logger (JSON, to stderr) at the given
// level (debug/info/warn/error/...). An unknown level is an error.
func New(level string) (*zap.Logger, error) {
	lvl, err := zap.ParseAtomicLevel(level)
	if err != nil {
		return nil, fmt.Errorf("parse log level %q: %w", level, err)
	}
	cfg := zap.NewProductionConfig()
	cfg.Level = lvl
	return cfg.Build()
}
