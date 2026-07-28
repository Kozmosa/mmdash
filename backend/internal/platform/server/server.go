// Package server runs the Core HTTP server with graceful shutdown.
package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/logging"
)

// Server owns the Core HTTP process lifecycle.
type Server struct {
	httpServer      *http.Server
	logger          *logging.Logger
	shutdownTimeout time.Duration
}

// New creates a hardened HTTP server.
func New(
	addr string,
	handler http.Handler,
	logger *logging.Logger,
	shutdownTimeout time.Duration,
) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler,
			IdleTimeout:       60 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      0,
		},
		logger:          logger,
		shutdownTimeout: shutdownTimeout,
	}
}

// Run listens until the context is cancelled or the server fails.
func (server *Server) Run(ctx context.Context) error {
	errorsChannel := make(chan error, 1)
	go func() {
		server.logger.Info("core.started", map[string]interface{}{"addr": server.httpServer.Addr})
		errorsChannel <- server.httpServer.ListenAndServe()
	}()

	select {
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), server.shutdownTimeout)
		defer cancel()
		server.logger.Info("core.stopping", map[string]interface{}{})
		if err := server.httpServer.Shutdown(shutdownContext); err != nil {
			return err
		}
		err := <-errorsChannel
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
