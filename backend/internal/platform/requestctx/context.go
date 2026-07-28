// Package requestctx propagates request, actor, and project context through Core.
package requestctx

import (
	"context"
	"sync"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	actorIDKey   contextKey = "actor_id"
	projectIDKey contextKey = "project_id"
	stateKey     contextKey = "observation_state"
)

// Values is the normalized inbound request context.
type Values struct {
	ActorID   string
	ActorKind string
	ProjectID string
	RequestID string
}

type observationState struct {
	actorVerified   bool
	mu              sync.RWMutex
	projectVerified bool
	values          Values
}

// WithValues attaches normalized request values.
func WithValues(ctx context.Context, values Values) context.Context {
	ctx = context.WithValue(ctx, stateKey, &observationState{values: values})
	ctx = context.WithValue(ctx, requestIDKey, values.RequestID)
	if values.ActorID != "" {
		ctx = context.WithValue(ctx, actorIDKey, values.ActorID)
	}
	if values.ProjectID != "" {
		ctx = context.WithValue(ctx, projectIDKey, values.ProjectID)
	}
	return ctx
}

// SetActor replaces untrusted forwarded identity with an authenticated user.
func SetActor(ctx context.Context, actorID string, actorKind ...string) {
	if state, ok := ctx.Value(stateKey).(*observationState); ok {
		state.mu.Lock()
		state.values.ActorID = actorID
		if len(actorKind) > 0 {
			state.values.ActorKind = actorKind[0]
		}
		state.actorVerified = true
		state.mu.Unlock()
	}
}

// SetProject records a project after domain authorization has begun.
func SetProject(ctx context.Context, projectID string) {
	if state, ok := ctx.Value(stateKey).(*observationState); ok {
		state.mu.Lock()
		state.values.ProjectID = projectID
		state.projectVerified = true
		state.mu.Unlock()
	}
}

// TrustedSnapshot excludes forwarded IDs until Auth/Project verifies them.
func TrustedSnapshot(ctx context.Context) Values {
	if state, ok := ctx.Value(stateKey).(*observationState); ok {
		state.mu.RLock()
		defer state.mu.RUnlock()
		values := state.values
		if !state.actorVerified {
			values.ActorID = ""
			values.ActorKind = ""
		}
		if !state.projectVerified {
			values.ProjectID = ""
		}
		return values
	}
	return Snapshot(ctx)
}

// Snapshot returns a race-safe observation context.
func Snapshot(ctx context.Context) Values {
	if state, ok := ctx.Value(stateKey).(*observationState); ok {
		state.mu.RLock()
		defer state.mu.RUnlock()
		return state.values
	}
	return Values{
		ActorID:   stringValue(ctx, actorIDKey),
		ProjectID: stringValue(ctx, projectIDKey),
		RequestID: stringValue(ctx, requestIDKey),
	}
}

// RequestID returns the current request ID.
func RequestID(ctx context.Context) string {
	return Snapshot(ctx).RequestID
}

// ActorID returns the propagated actor ID when present.
func ActorID(ctx context.Context) string {
	return Snapshot(ctx).ActorID
}

// ProjectID returns the propagated project ID when present.
func ProjectID(ctx context.Context) string {
	return Snapshot(ctx).ProjectID
}

func stringValue(ctx context.Context, key contextKey) string {
	value, _ := ctx.Value(key).(string)
	return value
}
