package logger

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// Config holds optional knobs for the production logger.
type Config struct {
	// Service is the application name.
	Service string
	// Version is the application version (e.g., "1.2.3" or git SHA).
	Version string
	// Env is the deployment environment (e.g., "production", "staging", "development").
	Env string
	// Level is the minimum log level (e.g., "debug|info|warn|error"), overrides LOG_LEVEL if set.
	Level string
	// AddSource enables adding source file and line number to log records.
	AddSource bool
}

// NewProductionLogger returns a production JSON logger writing to stderr.
// Default level is INFO unless overridden by cfg.Level or $LOG_LEVEL.
func NewProductionLogger(cfg Config) *slog.Logger {
	lvl := parseLevel(firstNonEmpty(cfg.Level, os.Getenv("LOG_LEVEL"), "info"))

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: cfg.AddSource,
		// Keep output stable: RFC3339 timestamps; don’t rename keys.
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				// RFC3339 with milliseconds for humans + machines
				return slog.String(slog.TimeKey, time.Now().Format(time.RFC3339Nano))
			}
			return a
		},
	}

	h := slog.NewJSONHandler(os.Stderr, opts)

	base := []any{}
	if cfg.Service != "" {
		base = append(base, "service", cfg.Service)
	}
	if cfg.Version != "" {
		base = append(base, "version", cfg.Version)
	}
	if cfg.Env != "" {
		base = append(base, "env", cfg.Env)
	}

	return slog.New(h).With(base...)
}

// SetDefault installs a production logger as slog’s global default.
func SetDefault(cfg Config) *slog.Logger {
	l := NewProductionLogger(cfg)
	slog.SetDefault(l)
	return l
}

// NewTestLogger returns a slog.Logger that writes to t.Logf.
// It is useful for tests in other packages that want to log via slog.
func NewTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	w := testWriter{t: t}

	opts := &slog.HandlerOptions{
		Level:     slog.LevelDebug, // show everything in tests
		AddSource: true,            // handy in failures
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Drop timestamps for stable, concise test logs.
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}

	h := slog.NewTextHandler(w, opts)
	return slog.New(h)
}

type testWriter struct{ t *testing.T }

func (tw testWriter) Write(p []byte) (int, error) {
	tw.t.Helper()
	// Avoid extra blank lines: trim trailing newline added by handler.
	tw.t.Logf("%s", bytes.TrimRight(p, "\n"))
	return len(p), nil
}

func parseLevel(s string) slog.Leveler {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		// Unknown -> INFO
		return slog.LevelInfo
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
