package logging

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestNewValidLevels(t *testing.T) {
	tests := map[string]zapcore.Level{
		"debug": zapcore.DebugLevel,
		"info":  zapcore.InfoLevel,
		"warn":  zapcore.WarnLevel,
		"error": zapcore.ErrorLevel,
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			l, err := New(name)
			if err != nil {
				t.Fatalf("New(%q): %v", name, err)
			}
			defer l.Sync()
			if got := l.Level(); got != want {
				t.Errorf("level = %v, want %v", got, want)
			}
		})
	}
}

func TestNewInvalidLevel(t *testing.T) {
	if _, err := New("loud"); err == nil {
		t.Fatal("expected error for invalid level, got nil")
	}
}
