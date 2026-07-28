package datahub

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

// Projector applies one idempotent domain-event projection.
type Projector interface {
	Project(context.Context, contract.EventEnvelope) error
}

// ProjectorFunc adapts a projection function.
type ProjectorFunc func(context.Context, contract.EventEnvelope) error

func (projector ProjectorFunc) Project(
	ctx context.Context,
	event contract.EventEnvelope,
) error {
	return projector(ctx, event)
}

// ProjectionRegistry maps exact event types to projection owners.
type ProjectionRegistry struct {
	mu         sync.RWMutex
	projectors map[string]Projector
}

func NewProjectionRegistry() *ProjectionRegistry {
	return &ProjectionRegistry{projectors: map[string]Projector{}}
}

func (registry *ProjectionRegistry) Register(eventType string, projector Projector) error {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" || projector == nil {
		return fmt.Errorf("event type and projector are required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.projectors[eventType]; exists {
		return fmt.Errorf("event projector already registered: %s", eventType)
	}
	registry.projectors[eventType] = projector
	return nil
}

func (registry *ProjectionRegistry) Patterns() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	patterns := make([]string, 0, len(registry.projectors))
	for eventType := range registry.projectors {
		patterns = append(patterns, eventType)
	}
	sort.Strings(patterns)
	return patterns
}

func (registry *ProjectionRegistry) Handle(
	ctx context.Context,
	event contract.EventEnvelope,
) error {
	registry.mu.RLock()
	projector := registry.projectors[event.EventType]
	registry.mu.RUnlock()
	if projector == nil {
		return fmt.Errorf("event projector is not registered: %s", event.EventType)
	}
	return projector.Project(ctx, event)
}
