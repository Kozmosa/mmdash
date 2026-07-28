package requestctx

import (
	"net/http"
	"regexp"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
var contextIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,127}$`)

// Middleware creates request IDs and propagates trusted gateway context.
func Middleware(generator identity.Generator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestID := request.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(requestID) {
			requestID = generator.MustNew()
		}
		values := Values{RequestID: requestID}
		if actorID := request.Header.Get("X-Mmdash-User-ID"); contextIDPattern.MatchString(actorID) {
			values.ActorID = actorID
		}
		if projectID := request.Header.Get("X-Mmdash-Project-ID"); contextIDPattern.MatchString(projectID) {
			values.ProjectID = projectID
		}

		response.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(response, request.WithContext(WithValues(request.Context(), values)))
	})
}
