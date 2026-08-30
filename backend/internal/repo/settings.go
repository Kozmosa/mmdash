package repo

import (
	"context"
	"errors"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const SettingType = "repo.connection"

// SettingsResolver is the trusted, in-process settings boundary used by Repo.
type SettingsResolver interface {
	Resolve(context.Context, settings.Scope, string, string) (settings.ResolvedSetting, error)
}

// ConnectionTester adapts Repo provider checks to the shared Settings registry.
type ConnectionTester struct {
	Providers *provider.Registry
}

// SettingDefinition is registered by Repo during Core composition.
func SettingDefinition(
	tester settings.ConnectionTester,
	serverExistingEnabled bool,
) settings.TypeDefinition {
	return settings.TypeDefinition{
		Description: "Creates an mmdash-managed repository or connects GitHub or an administrator-enabled server repository.",
		Fields: []settings.FieldDefinition{
			{
				Key: "provider", Kind: settings.FieldSelect, Label: "Provider",
				Options: []string{"managed", "github", "server_existing"}, Required: true,
			},
			{
				Description: "GitHub HTTPS URL or an administrator-allowlisted Core container path. Stored as a secret so server paths are never returned by Settings APIs.",
				Key:         "remote_url", Kind: settings.FieldSecret,
				Label: "Repository location", Required: false,
			},
			{
				Description: "Fine-grained GitHub PAT. Not used by managed or server-existing repositories.",
				Key:         "access_token", Kind: settings.FieldSecret,
				Label: "Access token", Required: false,
			},
			{
				Key: "code_branch", Kind: settings.FieldString,
				Label: "Code branch", Required: true,
			},
			{
				Key: "article_branch", Kind: settings.FieldString,
				Label: "Article branch", Required: true,
			},
			{
				Key: "result_branch", Kind: settings.FieldString,
				Label: "Result branch", Required: true,
			},
			{
				Description: "GitHub HMAC secret. Generated and shown once when omitted.",
				Key:         "webhook_secret", Kind: settings.FieldSecret,
				Label: "Webhook secret", Required: false,
			},
		},
		Key: SettingType, Order: 20, Owner: "repo",
		Scopes: []settings.Scope{settings.ScopeProject},
		Tester: tester, Title: "Repository",
		Validator: ConnectionConfigValidator{
			ServerExistingEnabled: serverExistingEnabled,
		},
	}
}

// ConnectionConfigValidator applies provider-conditional requirements that
// the generic Settings field registry cannot express.
type ConnectionConfigValidator struct {
	ServerExistingEnabled bool
}

func (validator ConnectionConfigValidator) ValidateConfig(
	values map[string]interface{},
) error {
	resolved := settings.ResolvedSetting{Values: values}
	config, err := providerConfig(resolved)
	if err != nil {
		return err
	}
	switch config.Provider {
	case "managed":
		return nil
	case "github":
		if config.RemoteURL == "" || config.AccessToken == "" {
			return provider.ErrInvalidConfig
		}
		return nil
	case "server_existing":
		if !validator.ServerExistingEnabled {
			return provider.ErrUnavailable
		}
		if config.RemoteURL == "" {
			return provider.ErrInvalidConfig
		}
		return nil
	default:
		return provider.ErrUnsupported
	}
}

func (tester ConnectionTester) Test(
	ctx context.Context,
	resolved settings.ResolvedSetting,
) ([]settings.ConnectionCheck, error) {
	config, err := providerConfig(resolved)
	if err != nil {
		return []settings.ConnectionCheck{{
			Message: "Repository settings are incomplete",
			Name:    "configuration",
			Status:  "failed",
		}}, err
	}
	connection, err := tester.Providers.Test(ctx, config)
	if err != nil {
		return []settings.ConnectionCheck{{
			Message: safeProviderMessage(err),
			Name:    "provider",
			Status:  "failed",
		}}, err
	}
	checks := []settings.ConnectionCheck{
		{Name: "provider", Status: "passed"},
	}
	if config.Provider == "github" {
		checks = append(checks, settings.ConnectionCheck{
			Name: "authentication", Status: "passed",
		})
	} else if config.Provider == "managed" {
		checks = append(checks, settings.ConnectionCheck{
			Name: "managed_storage", Status: "passed",
		})
	} else {
		checks = append(checks, settings.ConnectionCheck{
			Name: "server_mount_allowlist", Status: "passed",
		})
	}
	checks = append(checks,
		settings.ConnectionCheck{
			Message: connection.DefaultBranch,
			Name:    "default_branch",
			Status:  "passed",
		},
		settings.ConnectionCheck{
			Message: strings.Join(connection.BranchNames(), ", "),
			Name:    "workspace_branches",
			Status:  "passed",
		},
	)
	return checks, nil
}

func providerConfig(resolved settings.ResolvedSetting) (provider.Config, error) {
	stringValue := func(key string) string {
		value, _ := resolved.Values[key].(string)
		return strings.TrimSpace(value)
	}
	config := provider.Config{
		AccessToken:   stringValue("access_token"),
		ArticleBranch: stringValue("article_branch"),
		CodeBranch:    stringValue("code_branch"),
		Provider:      stringValue("provider"),
		RemoteURL:     stringValue("remote_url"),
		ResultBranch:  stringValue("result_branch"),
	}
	if config.Provider == "" ||
		config.CodeBranch == "" ||
		config.ArticleBranch == "" ||
		config.ResultBranch == "" {
		return provider.Config{}, ErrInvalid
	}
	return config, nil
}

func safeProviderMessage(err error) string {
	switch {
	case errors.Is(err, provider.ErrAuthentication):
		return "Repository authentication failed"
	case errors.Is(err, provider.ErrBranchMissing):
		return "One or more mapped branches do not exist"
	case errors.Is(err, provider.ErrWritePermission):
		return "Repository contents write permission is required"
	case errors.Is(err, provider.ErrRemoteNotFound):
		return "Repository was not found"
	case errors.Is(err, provider.ErrUnavailable):
		return "Server repository access is not enabled for this deployment"
	default:
		return "Repository connection test failed"
	}
}
