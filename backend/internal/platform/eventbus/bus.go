// Package eventbus provides deterministic in-process event dispatch.
package eventbus

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

var (
	consumerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9.-]*$`)
	eventTypePattern    = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)
)

// Handler consumes one stable event envelope.
type Handler func(context.Context, contract.EventEnvelope) error

// Consumer declares one named idempotency boundary and its event patterns.
type Consumer struct {
	Handler  Handler
	Name     string
	Patterns []string
}

// Result is one in-process delivery outcome.
type Result struct {
	Consumer string
	Err      error
}

// Bus is a concurrency-safe registry of in-process consumers.
type Bus struct {
	consumers map[string]Consumer
	mu        sync.RWMutex
}

// New constructs an empty event bus.
func New() *Bus {
	return &Bus{consumers: map[string]Consumer{}}
}

// Register adds a uniquely named consumer.
func (bus *Bus) Register(consumer Consumer) error {
	consumer.Name = strings.TrimSpace(consumer.Name)
	if !consumerNamePattern.MatchString(consumer.Name) ||
		consumer.Handler == nil ||
		len(consumer.Patterns) == 0 {
		return fmt.Errorf("event consumer name, patterns, and handler are required")
	}
	seen := map[string]struct{}{}
	for index, pattern := range consumer.Patterns {
		pattern = strings.TrimSpace(pattern)
		if !validPattern(pattern) {
			return fmt.Errorf("invalid event pattern %q", pattern)
		}
		if _, duplicate := seen[pattern]; duplicate {
			return fmt.Errorf("duplicate event pattern %q", pattern)
		}
		seen[pattern] = struct{}{}
		consumer.Patterns[index] = pattern
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if _, exists := bus.consumers[consumer.Name]; exists {
		return fmt.Errorf("event consumer already registered: %s", consumer.Name)
	}
	consumer.Patterns = append([]string(nil), consumer.Patterns...)
	bus.consumers[consumer.Name] = consumer
	return nil
}

// Consumers returns a deterministic snapshot for discovery and diagnostics.
func (bus *Bus) Consumers() []Consumer {
	bus.mu.RLock()
	defer bus.mu.RUnlock()
	consumers := make([]Consumer, 0, len(bus.consumers))
	for _, consumer := range bus.consumers {
		consumer.Patterns = append([]string(nil), consumer.Patterns...)
		consumers = append(consumers, consumer)
	}
	sort.Slice(consumers, func(left int, right int) bool {
		return consumers[left].Name < consumers[right].Name
	})
	return consumers
}

// Matching returns stable consumer names interested in the event type.
func (bus *Bus) Matching(eventType string) []string {
	consumers := bus.Consumers()
	names := make([]string, 0, len(consumers))
	for _, consumer := range consumers {
		if matchesAny(consumer.Patterns, eventType) {
			names = append(names, consumer.Name)
		}
	}
	return names
}

// Deliver invokes one named matching consumer.
func (bus *Bus) Deliver(
	ctx context.Context,
	consumerName string,
	event contract.EventEnvelope,
) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("validate event envelope: %w", err)
	}
	bus.mu.RLock()
	consumer, exists := bus.consumers[consumerName]
	bus.mu.RUnlock()
	if !exists {
		return fmt.Errorf("event consumer is not registered: %s", consumerName)
	}
	if !matchesAny(consumer.Patterns, event.EventType) {
		return fmt.Errorf(
			"event consumer %s does not match %s",
			consumerName,
			event.EventType,
		)
	}
	return consumer.Handler(ctx, event)
}

// Publish immediately dispatches an ephemeral in-process event.
//
// Durable domain events should be written to system_outbox and delivered by
// the Outbox Processor instead.
func (bus *Bus) Publish(
	ctx context.Context,
	event contract.EventEnvelope,
) ([]Result, error) {
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("validate event envelope: %w", err)
	}
	names := bus.Matching(event.EventType)
	results := make([]Result, 0, len(names))
	for _, name := range names {
		results = append(results, Result{
			Consumer: name,
			Err:      bus.Deliver(ctx, name, event),
		})
	}
	return results, nil
}

func validPattern(pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return eventTypePattern.MatchString(prefix + ".placeholder")
	}
	return eventTypePattern.MatchString(pattern)
}

func matchesAny(patterns []string, eventType string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == eventType {
			return true
		}
		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(eventType, prefix) {
				return true
			}
		}
	}
	return false
}
