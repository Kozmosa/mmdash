package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Descriptor struct {
	Key          string
	DisplayName  string
	Capabilities DeclaredCapabilities
}

type registration struct {
	descriptor Descriptor
	factory    Factory
}

// Registry stores immutable adapter factories. It never stores a constructed
// adapter or a credential-bearing AdapterConfig.
type Registry struct {
	mutex         sync.RWMutex
	registrations map[string]registration
}

func NewRegistry() *Registry {
	return &Registry{registrations: map[string]registration{}}
}

func (registry *Registry) Register(descriptor Descriptor, factory Factory) error {
	if registry == nil {
		return ErrInvalidArgument
	}
	descriptor.Key = strings.TrimSpace(descriptor.Key)
	descriptor.DisplayName = strings.TrimSpace(descriptor.DisplayName)
	if descriptor.Key == "" || descriptor.DisplayName == "" || factory == nil {
		return ErrInvalidArgument
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if _, exists := registry.registrations[descriptor.Key]; exists {
		return fmt.Errorf("agent adapter already registered: %s", descriptor.Key)
	}
	registry.registrations[descriptor.Key] = registration{descriptor: descriptor, factory: factory}
	return nil
}

func (registry *Registry) Descriptor(key string) (Descriptor, bool) {
	if registry == nil {
		return Descriptor{}, false
	}
	registry.mutex.RLock()
	defer registry.mutex.RUnlock()
	registration, exists := registry.registrations[strings.TrimSpace(key)]
	return registration.descriptor, exists
}

func (registry *Registry) Descriptors() []Descriptor {
	if registry == nil {
		return nil
	}
	registry.mutex.RLock()
	descriptors := make([]Descriptor, 0, len(registry.registrations))
	for _, registration := range registry.registrations {
		descriptors = append(descriptors, registration.descriptor)
	}
	registry.mutex.RUnlock()
	sort.Slice(descriptors, func(left, right int) bool {
		return descriptors[left].Key < descriptors[right].Key
	})
	return descriptors
}

// New constructs an instance-scoped adapter. The Registry does not retain
// config or the resulting adapter.
func (registry *Registry) New(ctx context.Context, key string, config AdapterConfig) (Adapter, error) {
	if registry == nil {
		return nil, ErrUnsupported
	}
	registry.mutex.RLock()
	registration, exists := registry.registrations[strings.TrimSpace(key)]
	registry.mutex.RUnlock()
	if !exists {
		return nil, ErrUnsupported
	}
	if strings.TrimSpace(config.InstanceID) == "" {
		return nil, ErrInvalidArgument
	}
	return registration.factory(ctx, config)
}
