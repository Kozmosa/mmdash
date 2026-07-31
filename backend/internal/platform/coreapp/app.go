// Package coreapp composes the modular Core HTTP application.
package coreapp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/health"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/logging"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/module"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
)

// Options contains explicitly composed Core dependencies.
type Options struct {
	Audit       func(context.Context, HTTPObservation) error
	Health      health.Handler
	Logger      *logging.Logger
	Modules     *module.Registry
	Metrics     *metrics.Registry
	OpenAPI     []byte
	IDGenerator identity.Generator
}

// HTTPObservation is the bounded, secret-free request audit surface.
type HTTPObservation struct {
	DurationMS int64
	Method     string
	Path       string
	Status     int
}

// NewHandler creates the complete Core HTTP handler.
func NewHandler(options Options) http.Handler {
	mux := http.NewServeMux()
	options.Health.RegisterRoutes(mux)
	if options.Metrics != nil {
		options.Metrics.RegisterRoutes(mux)
	}
	registerOpenAPI(mux, options.OpenAPI)
	options.Modules.Mount(mux)
	mux.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		httpx.WriteError(
			response,
			request,
			apperror.New(http.StatusNotFound, "NOT_FOUND", "Route not found"),
		)
	})

	handler := recoveryMiddleware(options.Logger, mux)
	handler = accessLogMiddleware(options.Logger, options.Metrics, options.Audit, handler)
	handler = requestctx.Middleware(options.IDGenerator, handler)
	return handler
}

func registerOpenAPI(mux *http.ServeMux, contract []byte) {
	mux.HandleFunc("/openapi.yaml", func(response http.ResponseWriter, request *http.Request) {
		if !httpx.RequireMethod(response, request, http.MethodGet) {
			return
		}
		response.Header().Set("Content-Type", "application/yaml")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(contract)
	})
}

func recoveryMiddleware(logger *logging.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("http.request.panicked", map[string]interface{}{
					"request_id": requestctx.RequestID(request.Context()),
					"stack":      string(debug.Stack()),
				})
				httpx.WriteError(
					response,
					request,
					apperror.New(
						http.StatusInternalServerError,
						"INTERNAL_ERROR",
						"An unexpected error occurred",
					),
				)
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func accessLogMiddleware(
	logger *logging.Logger,
	metricRegistry *metrics.Registry,
	audit func(context.Context, HTTPObservation) error,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: response}
		next.ServeHTTP(recorder, request)
		duration := time.Since(startedAt)
		values := requestctx.TrustedSnapshot(request.Context())
		path := observablePath(request.URL.Path)
		logFields := map[string]interface{}{
			"duration_ms": duration.Milliseconds(),
			"method":      request.Method,
			"path":        path,
			"project_id":  values.ProjectID,
			"request_id":  values.RequestID,
			"status":      recorder.statusCode(),
			"user_id":     values.ActorID,
		}
		if recorder.statusCode() >= http.StatusInternalServerError {
			logger.Error("http.request.completed", logFields)
		} else {
			logger.Info("http.request.completed", logFields)
		}
		metricRegistry.ObserveHTTP(request.Method, recorder.statusCode(), duration)
		if audit != nil && strings.HasPrefix(request.URL.Path, "/v1/") {
			auditContext, cancel := detachedAuditContext(request.Context())
			defer cancel()
			_ = audit(auditContext, HTTPObservation{
				DurationMS: duration.Milliseconds(), Method: request.Method,
				Path: path, Status: recorder.statusCode(),
			})
		}
	})
}

func detachedAuditContext(requestContext context.Context) (context.Context, context.CancelFunc) {
	values := requestctx.TrustedSnapshot(requestContext)
	auditContext := requestctx.WithValues(context.Background(), values)
	if values.ActorID != "" {
		requestctx.SetActor(auditContext, values.ActorID, values.ActorKind)
	}
	if values.ProjectID != "" {
		requestctx.SetProject(auditContext, values.ProjectID)
	}
	return context.WithTimeout(auditContext, 5*time.Second)
}

func observablePath(path string) string {
	const transferPrefix = "/v1/artifact-transfers/"
	if strings.HasPrefix(path, transferPrefix) {
		return transferPrefix + "{redacted}"
	}
	return path
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	if recorder.status != 0 {
		return
	}
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Write(contents []byte) (int, error) {
	if recorder.status == 0 {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.ResponseWriter.Write(contents)
}

// Flush preserves streaming responses such as SSE.
func (recorder *statusRecorder) Flush() {
	if recorder.status == 0 {
		recorder.WriteHeader(http.StatusOK)
	}
	if flusher, ok := recorder.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack preserves WebSocket upgrade support.
func (recorder *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := recorder.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("HTTP hijacking is unavailable")
	}
	return hijacker.Hijack()
}

// ReadFrom preserves efficient file streaming.
func (recorder *statusRecorder) ReadFrom(reader io.Reader) (int64, error) {
	if recorder.status == 0 {
		recorder.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := recorder.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(reader)
	}
	return io.Copy(recorder.ResponseWriter, reader)
}

// Push preserves HTTP/2 server push support when available.
func (recorder *statusRecorder) Push(target string, options *http.PushOptions) error {
	pusher, ok := recorder.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}

func (recorder *statusRecorder) statusCode() int {
	if recorder.status == 0 {
		return http.StatusOK
	}
	return recorder.status
}
