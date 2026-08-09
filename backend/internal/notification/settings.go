package notification

import (
	"context"
	"errors"
	"time"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

// SettingTester validates the saved, decrypted channel configuration without
// exposing the secret or making Progress responsible for provider I/O.
type SettingTester struct {
	Client  HTTPDoer
	Timeout time.Duration
	Clock   func() time.Time
}

func (tester SettingTester) Test(ctx context.Context, resolved settings.ResolvedSetting) ([]settings.ConnectionCheck, error) {
	var adapter ProviderAdapter
	switch resolved.TypeKey {
	case "notification.feishu_webhook":
		adapter = FeishuWebhook{Client: tester.Client, Timeout: tester.Timeout, Clock: tester.Clock}
	case "notification.generic_webhook":
		adapter = GenericWebhook{Client: tester.Client, Timeout: tester.Timeout, Clock: tester.Clock}
	default:
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
