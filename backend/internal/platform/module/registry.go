// Package module registers Core domain modules explicitly.
package module

import (
	"fmt"
	"net/http"
	"sort"
)

// Module is the standard domain module HTTP boundary.
type Module interface {
	Name() string
	RegisterRoutes(*http.ServeMux)
}

// Registry keeps module ownership and route installation explicit.
type Registry struct {
	modules map[string]Module
}

// NewRegistry creates an empty module registry.
func NewRegistry() *Registry {
	return &Registry{modules: make(map[string]Module)}
}

// Register adds one uniquely named module.
func (registry *Registry) Register(domainModule Module) error {
	name := domainModule.Name()
	if name == "" {
		return fmt.Errorf("module name must not be empty")
	}
	if _, exists := registry.modules[name]; exists {
		return fmt.Errorf("module %q is already registered", name)
	}
	registry.modules[name] = domainModule
	return nil
}

// Mount installs all routes in deterministic name order.
func (registry *Registry) Mount(mux *http.ServeMux) {
	names := registry.Names()
	for _, name := range names {
		registry.modules[name].RegisterRoutes(mux)
	}
}

// Names returns registered module names in deterministic order.
func (registry *Registry) Names() []string {
	names := make([]string, 0, len(registry.modules))
	for name := range registry.modules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
