package example

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

// StorageChecker is the narrow persistence boundary used by the example module.
type StorageChecker interface {
	Check(context.Context) (time.Time, error)
}

// PostgresChecker verifies the example request through PostgreSQL.
type PostgresChecker struct {
	DB *sql.DB
}

// Check asks PostgreSQL for its current time.
func (checker PostgresChecker) Check(ctx context.Context) (time.Time, error) {
	var checkedAt time.Time
	err := checker.DB.QueryRowContext(ctx, "SELECT CURRENT_TIMESTAMP").Scan(&checkedAt)
	return checkedAt, err
}

// Module is the Web-to-database example slice used by the engineering baseline.
type Module struct {
	checker StorageChecker
}

// New creates an example module.
func New(checker StorageChecker) Module {
	return Module{checker: checker}
}

// Name returns the module registry name.
func (Module) Name() string {
	return "example"
}

// RegisterRoutes registers the example module's HTTP contract.
func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/example", module.handleCheck)
}

func (module Module) handleCheck(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}

	checkedAt, err := module.checker.Check(request.Context())
	if err != nil {
		httpx.WriteError(
			response,
			request,
			apperror.New(
				http.StatusServiceUnavailable,
				"STORAGE_UNAVAILABLE",
				"PostgreSQL is unavailable",
			).WithCause(err),
		)
		return
	}

	httpx.WriteJSON(response, http.StatusOK, map[string]string{
		"status":     "ok",
		"storage":    "postgres",
		"checked_at": checkedAt.UTC().Format(time.RFC3339Nano),
	})
}
