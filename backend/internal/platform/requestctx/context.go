// Package requestctx propagates request, actor, and project context through Core.
package requestctx

import "context"

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	actorIDKey   contextKey = "actor_id"
	projectIDKey contextKey = "project_id"
)

// Values is the normalized inbound request context.
type Values struct {
	ActorID   string
	ProjectID string
	RequestID string
}

// WithValues attaches normalized request values.
func WithValues(ctx context.Context, values Values) context.Context {
	ctx = context.WithValue(ctx, requestIDKey, values.RequestID)
	if values.ActorID != "" {
		ctx = context.WithValue(ctx, actorIDKey, values.ActorID)
	}
	if values.ProjectID != "" {
		ctx = context.WithValue(ctx, projectIDKey, values.ProjectID)
	}
	return ctx
}

// RequestID returns the current request ID.
func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

// ActorID returns the propagated actor ID when present.
func ActorID(ctx context.Context) string {
	value, _ := ctx.Value(actorIDKey).(string)
	return value
}

// ProjectID returns the propagated project ID when present.
func ProjectID(ctx context.Context) string {
	value, _ := ctx.Value(projectIDKey).(string)
	return value
}
