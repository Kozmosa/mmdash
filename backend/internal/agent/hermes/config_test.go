package hermes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

func TestFactoryRequestTimeoutCanOnlyNarrowDeploymentPolicy(t *testing.T) {
	factory := NewFactory(FactoryOptions{
		RuntimePolicy:    NetworkPolicy{RequestTimeout: 20 * time.Second},
		ManagementPolicy: NetworkPolicy{RequestTimeout: 8 * time.Second},
	})

	created, err := factory(context.Background(), agent.AdapterConfig{
		InstanceID: "instance-1",
		Values: map[string]string{
			ConfigRuntimeURL:            "https://runtime.example.test",
			ConfigAPIKey:                "runtime-secret",
			ConfigProfile:               "default",
			ConfigManagementURL:         "https://management.example.test",
			ConfigDashboardSessionToken: "dashboard-secret",
			ConfigRequestTimeoutSeconds: "5",
		},
	})
	if err != nil {
		t.Fatalf("create narrowed adapter: %v", err)
	}
	if timeout := created.(*Adapter).runtime.connector.policy.RequestTimeout; timeout != 5*time.Second {
		t.Fatalf("expected five-second timeout, got %s", timeout)
	}
	if timeout := created.(*Adapter).management.api.connector.policy.RequestTimeout; timeout != 5*time.Second {
		t.Fatalf("expected five-second management timeout, got %s", timeout)
	}

	created, err = factory(context.Background(), agent.AdapterConfig{
		InstanceID: "instance-2",
		Values: map[string]string{
			ConfigRuntimeURL:            "https://runtime.example.test",
			ConfigAPIKey:                "runtime-secret",
			ConfigProfile:               "default",
			ConfigManagementURL:         "https://management.example.test",
			ConfigDashboardSessionToken: "dashboard-secret",
			ConfigRequestTimeoutSeconds: "120",
		},
	})
	if err != nil {
		t.Fatalf("create clamped adapter: %v", err)
	}
	if timeout := created.(*Adapter).runtime.connector.policy.RequestTimeout; timeout != 20*time.Second {
		t.Fatalf("expected deployment timeout clamp, got %s", timeout)
	}
	if timeout := created.(*Adapter).management.api.connector.policy.RequestTimeout; timeout != 8*time.Second {
		t.Fatalf("expected management deployment timeout clamp, got %s", timeout)
	}
}

func TestFactoryRejectsInvalidRequestTimeout(t *testing.T) {
	factory := NewFactory(FactoryOptions{})
	for _, value := range []string{"not-a-number", "0", "301", "9223372036854775807"} {
		_, err := factory(context.Background(), agent.AdapterConfig{
			InstanceID: "instance-1",
			Values: map[string]string{
				ConfigRuntimeURL:            "https://runtime.example.test",
				ConfigAPIKey:                "runtime-secret",
				ConfigProfile:               "default",
				ConfigRequestTimeoutSeconds: value,
			},
		})
		if !errors.Is(err, agent.ErrInvalidArgument) {
			t.Fatalf("expected invalid argument for %q, got %v", value, err)
		}
	}
}
