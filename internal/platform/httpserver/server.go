package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type Server struct {
	server          *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
}

func NewServer(addr string, handler http.Handler, logger *slog.Logger, readHeaderTimeout, readTimeout, writeTimeout, idleTimeout, shutdownTimeout time.Duration) *Server {
	return &Server{
		server: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
		},
		shutdownTimeout: shutdownTimeout,
		logger:          logger,
	}
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("HTTP server starting", "addr", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		s.logger.Info("HTTP server shutting down")
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			_ = s.server.Close()
			return err
		}
		return <-errCh
	}
}
