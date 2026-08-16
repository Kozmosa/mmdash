package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
	"github.com/mmdash/mmdash/box/contracts"
)

func TestConfiguredRuntimesAdvertiseOnlyConfiguredProviders(t *testing.T) {
	t.Setenv("E2B_API_KEY", "")
	limits := contracts.ResourceLimits{CPUMillis: 1000, MemoryBytes: 1 << 30, TimeoutSecond: 300, DiskBytes: 1 << 30, PIDs: 128, Network: "disabled"}
	reported, factory, probeErrors, err := configuredRuntimes(context.Background(), limits, acceptRuntime)
	if err != nil || len(reported) != 1 || reported[0].Name != "local-docker" {
		t.Fatalf("local runtime configuration: %#v %v", reported, err)
	}
	if len(probeErrors) != 0 {
		t.Fatalf("unexpected probe errors: %v", probeErrors)
	}
	if _, err := factory(contracts.RunSpec{Runtime: "e2b"}); err == nil {
		t.Fatal("unconfigured E2B runtime was returned")
	}
}

func TestConfiguredRuntimesWireE2BFromOfficialEnvironment(t *testing.T) {
	t.Setenv("E2B_API_KEY", "test-key")
	t.Setenv("E2B_API_URL", "http://api.example.test")
	t.Setenv("E2B_SANDBOX_URL", "http://sandbox.example.test")
	t.Setenv("MMDASH_E2B_TEMPLATE", "project/template")
	limits := contracts.ResourceLimits{CPUMillis: 1000, MemoryBytes: 1 << 30, TimeoutSecond: 300, DiskBytes: 1 << 30, PIDs: 128, Network: "enabled"}
	reported, factory, _, err := configuredRuntimes(context.Background(), limits, acceptRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if len(reported) != 2 || reported[1].Name != "e2b" || reported[1].Image != "project/template" {
		t.Fatalf("reported runtimes: %#v", reported)
	}
	if runtime, err := factory(contracts.RunSpec{Runtime: "e2b", Limits: contracts.ResourceLimits{CPUMillis: 1000, MemoryBytes: 512 << 20, TimeoutSecond: 30, DiskBytes: 1 << 30, PIDs: 64, Network: "disabled"}}); err != nil || runtime == nil {
		t.Fatalf("E2B runtime factory: %#v %v", runtime, err)
	}
}

func TestConfiguredRuntimesRejectTasksBeyondAdvertisedCapacity(t *testing.T) {
	t.Setenv("E2B_API_KEY", "")
	limits := contracts.ResourceLimits{CPUMillis: 1000, MemoryBytes: 512 << 20, TimeoutSecond: 60, DiskBytes: 1 << 30, PIDs: 64, Network: "restricted"}
	_, factory, _, err := configuredRuntimes(context.Background(), limits, acceptRuntime)
	if err != nil {
		t.Fatal(err)
	}
	for _, requested := range []contracts.ResourceLimits{
		{CPUMillis: 1001, MemoryBytes: 512 << 20, TimeoutSecond: 60, DiskBytes: 1 << 30, PIDs: 64, Network: "restricted"},
		{CPUMillis: 1000, MemoryBytes: 512 << 20, TimeoutSecond: 60, DiskBytes: 1 << 30, PIDs: 64, Network: "enabled"},
	} {
		if _, err := factory(contracts.RunSpec{Runtime: "local-docker", Limits: requested}); err == nil {
			t.Fatalf("unsupported limits were accepted: %#v", requested)
		}
	}
}

func acceptRuntime(context.Context, sandbox.Runtime) error { return nil }

func TestConfiguredRuntimesDoNotAdvertiseFailedAdapters(t *testing.T) {
	t.Setenv("E2B_API_KEY", "")
	limits := contracts.ResourceLimits{CPUMillis: 1000, MemoryBytes: 1 << 30, TimeoutSecond: 300, DiskBytes: 1 << 30, PIDs: 128, Network: "disabled"}
	reported, factory, probeErrors, err := configuredRuntimes(context.Background(), limits, func(context.Context, sandbox.Runtime) error {
		return errors.New("dependency missing")
	})
	if err == nil || len(reported) != 0 || factory != nil || len(probeErrors) != 1 {
		t.Fatalf("failed adapter was advertised: %#v %#v %v", reported, probeErrors, err)
	}
}
