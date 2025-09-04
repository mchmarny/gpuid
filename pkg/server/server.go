package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mchmarny/gpuid/pkg/logger"
)

// Server defines the interface for the server that handles metrics and health checks.
type Server interface {
	Serve(ctx context.Context, handlers map[string]http.Handler)
}

// Option is a functional option for configuring Server.
type Option func(*server)

// WithLogger sets the logger for the server.
func WithLogger(logger *slog.Logger) Option {
	return func(s *server) {
		s.logger = logger
	}
}

// WithPort sets the port for the server.
func WithPort(port int) Option {
	return func(s *server) {
		s.port = port
	}
}

// NewServer creates a new Server instance with the provided options.
func NewServer(opts ...Option) Server {
	s := &server{
		port: 8080,
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.logger == nil {
		s.logger = logger.NewProductionLogger(logger.Config{})
	}

	s.logger.Debug("server initialized", "port", s.port)

	return s
}

type server struct {
	logger *slog.Logger
	port   int
}

// Serve initializes and starts the HTTP server for metrics and health checks.
func (s *server) Serve(ctx context.Context, handlers map[string]http.Handler) {
	handler := s.buildHandler(handlers)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("server starting", slog.Int("port", s.port))

	go func() {
		<-ctx.Done()
		s.logger.Info("server shutdown initiated")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("server shutdown failed", slog.Any("err", err))
		} else {
			s.logger.Info("server shutdown completed")
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.logger.ErrorContext(ctx, "failed to start metrics server", slog.Any("err", err))
	}
}

// buildHandler constructs the HTTP handler mux for the server.
func (s *server) buildHandler(handlers map[string]http.Handler) http.Handler {
	mux := http.NewServeMux()

	okFunc := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	mux.HandleFunc("/healthz", okFunc)
	mux.HandleFunc("/readyz", okFunc)
	mux.HandleFunc("/", okFunc)

	for path, handler := range handlers {
		mux.Handle(path, handler)
		s.logger.Info("registered handler", "path", path)
	}

	return mux
}
