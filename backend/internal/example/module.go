package example

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
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

// RegisterRoutes registers the example module's HTTP contract.
func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/example", module.handleCheck)
}

func (module Module) handleCheck(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	checkedAt, err := module.checker.Check(request.Context())
	if err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"code":    "STORAGE_UNAVAILABLE",
			"message": "PostgreSQL is unavailable",
		})
		return
	}

	writeJSON(response, http.StatusOK, map[string]string{
		"status":     "ok",
		"storage":    "postgres",
		"checked_at": checkedAt.UTC().Format(time.RFC3339Nano),
	})
}

func writeJSON(response http.ResponseWriter, status int, body interface{}) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
