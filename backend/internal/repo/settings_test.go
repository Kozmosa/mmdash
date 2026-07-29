package repo

import (
	"context"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

func TestRepoSettingDefinitionAndProviderConfig(t *testing.T) {
	definition := SettingDefinition(ConnectionTester{})
	if definition.Key != SettingType ||
		definition.Owner != "repo" ||
		len(definition.Fields) != 7 ||
		definition.Fields[2].Kind != settings.FieldSecret {
		t.Fatalf("unexpected Repo setting definition: %+v", definition)
	}
	config, err := providerConfig(settings.ResolvedSetting{
		Scope: settings.ScopeProject, ScopeID: "project", TypeKey: SettingType,
		Version: 3,
		Values: map[string]interface{}{
			"provider":       "local",
			"remote_url":     "C:/repos/project",
			"code_branch":    "main",
			"article_branch": "article",
			"result_branch":  "result",
		},
	})
	if err != nil || config.Provider != "local" || config.CodeBranch != "main" {
		t.Fatalf("resolve provider config: %+v, %v", config, err)
	}
}

func TestRepoConnectionTesterReturnsSafeChecks(t *testing.T) {
	registry := provider.NewRegistry()
	if err := registry.Register("local", repoAdapterFunc(func(
		context.Context,
		provider.Config,
	) (provider.Connection, error) {
		return provider.Connection{
			Branches: map[string]string{
				"main":    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"article": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"result":  "cccccccccccccccccccccccccccccccccccccccc",
			},
			DefaultBranch: "main",
		}, nil
	})); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	checks, err := (ConnectionTester{Providers: registry}).Test(
		context.Background(),
		settings.ResolvedSetting{Values: map[string]interface{}{
			"provider": "local", "remote_url": "C:/repos/project",
			"code_branch": "main", "article_branch": "article",
			"result_branch": "result",
		}},
	)
	if err != nil || len(checks) != 4 || checks[3].Status != "passed" {
		t.Fatalf("connection checks: %+v, %v", checks, err)
	}
}

type repoAdapterFunc func(context.Context, provider.Config) (provider.Connection, error)

func (adapter repoAdapterFunc) Test(
	ctx context.Context,
	config provider.Config,
) (provider.Connection, error) {
	return adapter(ctx, config)
}
