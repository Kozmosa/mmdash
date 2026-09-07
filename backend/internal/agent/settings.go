package agent

import "github.com/mmdash/mmdash/backend/internal/settings"

// SettingDefinition describes the resource-scoped Hermes secrets owned by one
// Agent instance. Values are only read through Settings.ResolveResource; the
// generic Settings routes use an empty resource ID and cannot enumerate or
// overwrite an instance's encrypted record.
func SettingDefinition() settings.TypeDefinition {
	return settings.TypeDefinition{
		Description: "Encrypted Hermes runtime and endpoint-management credentials for one Agent instance.",
		Fields: []settings.FieldDefinition{
			{
				Description: "Hermes API Server key used only by Core.",
				Key:         settingAPIKey,
				Kind:        settings.FieldSecret,
				Label:       "Hermes API key",
				Required:    true,
			},
			{
				Description: "Optional Hermes profile or Agent identifier; omitted selects the unscoped API.",
				Key:         settingProfile,
				Kind:        settings.FieldString,
				Label:       "Hermes profile",
			},
			{
				Description: "Per-instance request timeout capped by deployment policy.",
				Key:         settingRequestTimeout,
				Kind:        settings.FieldNumber,
				Label:       "Request timeout seconds",
				Required:    true,
			},
			{
				Description: "Hermes Dashboard session credential for auto management.",
				Key:         settingDashboardToken,
				Kind:        settings.FieldSecret,
				Label:       "Dashboard session token",
			},
			{
				Description: "Cloudflare Access service client identifier for the Hermes Runtime API and, in auto mode, the Dashboard.",
				Key:         settingCFClientID,
				Kind:        settings.FieldSecret,
				Label:       "Cloudflare Access client ID",
			},
			{
				Description: "Cloudflare Access service client secret for the Hermes Runtime API and, in auto mode, the Dashboard.",
				Key:         settingCFClientSecret,
				Kind:        settings.FieldSecret,
				Label:       "Cloudflare Access client secret",
			},
		},
		Key:    SettingTypeAgentHermes,
		Order:  50,
		Owner:  "agent",
		Scopes: []settings.Scope{settings.ScopeProject},
		Title:  "Hermes Agent",
	}
}
