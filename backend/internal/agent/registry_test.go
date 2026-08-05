package agent

import (
	"context"
	"errors"
	"testing"
)

func TestRegistryConstructsInstanceScopedAdapters(t *testing.T) {
	registry := NewRegistry()
	seen := make([]AdapterConfig, 0, 2)
	factory := func(_ context.Context, config AdapterConfig) (Adapter, error) {
		seen = append(seen, config)
		return stubAdapter{}, nil
	}
	descriptor := Descriptor{
		Key:         "hermes",
		DisplayName: "Hermes",
		Capabilities: DeclaredCapabilities{ProjectAccess: ProjectAccessCapabilities{
			Verify: true, Configure: true, Rotate: true,
		}},
	}
	if err := registry.Register(descriptor, factory); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(descriptor, factory); err == nil {
		t.Fatal("expected duplicate registration to fail")
	}

	first, err := registry.New(context.Background(), "hermes", AdapterConfig{InstanceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.New(context.Background(), "hermes", AdapterConfig{InstanceID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || second == nil || len(seen) != 2 || seen[0].InstanceID != "a" || seen[1].InstanceID != "b" {
		t.Fatalf("unexpected factory calls: %#v", seen)
	}
	if descriptors := registry.Descriptors(); len(descriptors) != 1 || descriptors[0].Key != "hermes" {
		t.Fatalf("unexpected descriptors: %#v", descriptors)
	}
}

func TestRegistryRejectsUnknownOrInvalidAdapters(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Descriptor{}, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	if _, err := registry.New(context.Background(), "missing", AdapterConfig{InstanceID: "a"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}

// stubAdapter only exists to prove Registry construction. Its methods should
// never be called by these tests.
type stubAdapter struct{ Adapter }
