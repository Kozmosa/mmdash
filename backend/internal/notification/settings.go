package notification

import (
	"context"
	"errors"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

// SettingTester validates the saved, decrypted channel configuration without
// exposing the secret or making Progress responsible for provider I/O.
type SettingTester struct {
	Adapter ProviderAdapter
}

func (tester SettingTester) Test(ctx context.Context, resolved settings.ResolvedSetting) ([]settings.ConnectionCheck, error) {
	adapter := tester.Adapter
	if adapter == nil || adapter.Key() != resolved.TypeKey {
		return []settings.ConnectionCheck{{Name: "adapter", Status: "failed", Message: "Unsupported notification adapter"}}, nil
	}
	if err := adapter.ValidateConfig(resolved.Values); err != nil {
		return []settings.ConnectionCheck{{Name: "configuration", Status: "failed", Message: "Invalid notification channel configuration"}}, nil
	}
	if err := adapter.Test(ctx, resolved.Values); err != nil {
		var providerErr ProviderError
		if errors.As(err, &providerErr) {
			return []settings.ConnectionCheck{{Name: "provider", Status: "failed", Message: "Notification provider connection failed"}}, nil
		}
		return []settings.ConnectionCheck{{Name: "provider", Status: "failed", Message: "Notification provider connection failed"}}, nil
	}
	return []settings.ConnectionCheck{
		{Name: "configuration", Status: "passed"},
		{Name: "provider", Status: "passed"},
	}, nil
}
