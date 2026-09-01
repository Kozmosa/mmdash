// Package provider resolves reviewed Git provider settings into safe connections.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

var (
	ErrAuthentication         = errors.New("repository authentication failed")
	ErrBranchMissing          = errors.New("repository branch not found")
	ErrInvalidConfig          = errors.New("invalid repository provider configuration")
	ErrInvalidResponse        = errors.New("repository provider returned an invalid response")
	ErrNetworkUnavailable     = errors.New("repository network is unavailable")
	ErrRemoteNotFound         = errors.New("repository remote not found")
	ErrTemporarilyUnavailable = errors.New("repository provider is temporarily unavailable")
	ErrTimeout                = errors.New("repository provider operation timed out")
	ErrUnsupported            = errors.New("repository provider unsupported")
	ErrUnavailable            = errors.New("repository provider unavailable")
	ErrWritePermission        = errors.New("repository write permission unavailable")
)

type classifiedError struct {
	cause error
	class error
}

func (err *classifiedError) Error() string {
	return err.class.Error()
}

func (err *classifiedError) Unwrap() []error {
	return []error{err.class, err.cause}
}

func classify(class, cause error) error {
	if cause == nil || errors.Is(cause, class) {
		return class
	}
	return &classifiedError{cause: cause, class: class}
}

// Config is resolved only inside Core and may contain a credential.
type Config struct {
	AccessToken   string
	ArticleBranch string
	CodeBranch    string
	Provider      string
	RemoteURL     string
	ResultBranch  string
}

// Connection contains a normalized provider identity plus ephemeral credentials.
type Connection struct {
	Branches           map[string]string
	CanonicalRemoteURL string
	Credentials        *gitcli.Credentials
	DefaultBranch      string
	DisplayName        string
	FetchURL           string
	Provider           string
}

// BranchNames returns deterministic remote branch names.
func (connection Connection) BranchNames() []string {
	names := make([]string, 0, len(connection.Branches))
	for name := range connection.Branches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Adapter tests provider metadata, credentials, and mapped branch existence.
type Adapter interface {
	Test(context.Context, Config) (Connection, error)
}

// Resolver builds a runtime connection from settings that were already tested
// when connected or remapped. It must not enumerate remote refs.
type Resolver interface {
	Resolve(context.Context, Config) (Connection, error)
}

// Registry maps stable provider names to reviewed adapters.
type Registry struct {
	adapters map[string]Adapter
	mutex    sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

func (registry *Registry) Register(name string, adapter Adapter) error {
	name = strings.TrimSpace(name)
	if name == "" || adapter == nil {
		return ErrInvalidConfig
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if _, exists := registry.adapters[name]; exists {
		return fmt.Errorf("provider already registered: %s", name)
	}
	registry.adapters[name] = adapter
	return nil
}

func (registry *Registry) Test(ctx context.Context, config Config) (Connection, error) {
	registry.mutex.RLock()
	adapter := registry.adapters[config.Provider]
	registry.mutex.RUnlock()
	if adapter == nil {
		return Connection{}, ErrUnsupported
	}
	if err := validateMappings(config); err != nil {
		return Connection{}, err
	}
	connection, err := adapter.Test(ctx, config)
	if err != nil {
		return Connection{}, err
	}
	for _, branch := range []string{
		config.CodeBranch,
		config.ArticleBranch,
		config.ResultBranch,
	} {
		if _, exists := connection.Branches[branch]; !exists {
			return Connection{}, ErrBranchMissing
		}
	}
	return connection, nil
}

func (registry *Registry) Resolve(
	ctx context.Context,
	config Config,
) (Connection, error) {
	registry.mutex.RLock()
	adapter := registry.adapters[config.Provider]
	registry.mutex.RUnlock()
	if adapter == nil {
		return Connection{}, ErrUnsupported
	}
	if err := validateMappings(config); err != nil {
		return Connection{}, err
	}
	if resolver, ok := adapter.(Resolver); ok {
		return resolver.Resolve(ctx, config)
	}
	// Compatibility for custom test/development adapters. Production adapters
	// implement Resolver so runtime operations never fall back to a ref listing.
	return adapter.Test(ctx, config)
}

func validateMappings(config Config) error {
	if config.CodeBranch == "" ||
		config.ArticleBranch == "" ||
		config.ResultBranch == "" ||
		gitcli.ValidateBranch(config.CodeBranch) != nil ||
		gitcli.ValidateBranch(config.ArticleBranch) != nil ||
		gitcli.ValidateBranch(config.ResultBranch) != nil ||
		config.CodeBranch == config.ArticleBranch ||
		config.CodeBranch == config.ResultBranch ||
		config.ArticleBranch == config.ResultBranch {
		return ErrInvalidConfig
	}
	return nil
}

func splitOnce(value, separator string) (string, string, bool) {
	index := strings.Index(value, separator)
	if index < 0 {
		return value, "", false
	}
	return value[:index], value[index+len(separator):], true
}
