// Package health implements Core liveness and dependency readiness endpoints.
package health

import (
	"context"
	"net/http"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

// Checker is a named readiness dependency.
type Checker interface {
	Check(context.Context) error
	Name() string
}

// Handler serves process and dependency health.
type Handler struct {
	Checkers []Checker
	Timeout  time.Duration
}

// RegisterRoutes mounts health endpoints.
func (handler Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health/live", handler.live)
	mux.HandleFunc("/health/ready", handler.ready)
}

func (handler Handler) live(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]string{
		"service": "core",
		"status":  "ok",
		"version": "0.1.0",
	})
}

func (handler Handler) ready(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	timeout := handler.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()

	dependencies := make(map[string]string, len(handler.Checkers))
	status := "ready"
	httpStatus := http.StatusOK
	for _, checker := range handler.Checkers {
		if err := checker.Check(ctx); err != nil {
			dependencies[checker.Name()] = "unavailable"
			status = "not_ready"
			httpStatus = http.StatusServiceUnavailable
		} else {
			dependencies[checker.Name()] = "ready"
		}
	}
	httpx.WriteJSON(response, httpStatus, map[string]interface{}{
		"dependencies": dependencies,
		"status":       status,
	})
}
