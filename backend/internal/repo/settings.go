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
func SettingDefinition(tester settings.ConnectionTester) settings.TypeDefinition {
	return settings.TypeDefinition{
		Description: "Connects one managed Git repository and maps the code, article, and result workspaces.",
		Fields: []settings.FieldDefinition{
			{
				Key: "provider", Kind: settings.FieldSelect, Label: "Provider",
				Options: []string{"github", "local"}, Required: true,
			},
			{
				Description: "GitHub HTTPS URL or an administrator-allowlisted server path.",
				Key:         "remote_url", Kind: settings.FieldString,
				Label: "Repository", Required: true,
			},
			{
				Description: "Fine-grained GitHub PAT. Not used by Local Git.",
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
	return []settings.ConnectionCheck{
		{Name: "provider", Status: "passed"},
		{Name: "authentication", Status: "passed"},
		{
			Message: connection.DefaultBranch,
			Name:    "default_branch",
			Status:  "passed",
		},
		{
			Message: strings.Join(connection.BranchNames(), ", "),
			Name:    "workspace_branches",
			Status:  "passed",
		},
	}, nil
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
		config.RemoteURL == "" ||
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
	default:
		return "Repository connection test failed"
	}
}
