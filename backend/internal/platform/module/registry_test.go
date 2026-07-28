package module

import (
	"net/http"
	"reflect"
	"testing"
)

type testModule string

func (module testModule) Name() string {
	return string(module)
}

func (testModule) RegisterRoutes(*http.ServeMux) {}

func TestRegistryRejectsDuplicatesAndSortsNames(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(testModule("zeta")); err != nil {
		t.Fatalf("register zeta: %v", err)
	}
	if err := registry.Register(testModule("alpha")); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := registry.Register(testModule("alpha")); err == nil {
		t.Fatal("expected duplicate module to fail")
	}
	if !reflect.DeepEqual(registry.Names(), []string{"alpha", "zeta"}) {
		t.Fatalf("unexpected module order: %#v", registry.Names())
	}
}
