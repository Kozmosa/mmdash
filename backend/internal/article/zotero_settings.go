package article

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

type zoteroHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type ZoteroSettingAdapter struct {
	Client zoteroHTTPDoer
}

func SettingDefinitionZotero(client zoteroHTTPDoer) settings.TypeDefinition {
	adapter := ZoteroSettingAdapter{Client: client}
	return settings.TypeDefinition{
		Description: "Binds one read-only Zotero library and optional collection for frozen Article citations.",
		Fields: []settings.FieldDefinition{
			{Key: "library_type", Kind: settings.FieldSelect, Label: "Library type", Options: []string{"user", "group"}, Required: true},
			{Key: "library_id", Kind: settings.FieldString, Label: "Library ID", Required: true},
			{Key: "collection_key", Kind: settings.FieldString, Label: "Collection key", Description: "Optional Zotero collection restriction."},
			{Key: "api_key", Kind: settings.FieldSecret, Label: "Zotero API key", Description: "Read-only key stored encrypted by Core.", Required: true},
		},
		Key: SettingTypeZotero, Order: 65, Owner: "article",
		Scopes: []settings.Scope{settings.ScopeProject}, Tester: adapter,
		Title: "Article Zotero", Validator: adapter,
	}
}

func (ZoteroSettingAdapter) ValidateConfig(values map[string]interface{}) error {
	libraryType, _ := values["library_type"].(string)
	libraryID, _ := values["library_id"].(string)
	apiKey, _ := values["api_key"].(string)
	if (libraryType != "user" && libraryType != "group") || strings.TrimSpace(libraryID) == "" || strings.TrimSpace(apiKey) == "" {
		return ErrInvalid
	}
	if collection, exists := values["collection_key"]; exists {
		text, ok := collection.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return ErrInvalid
		}
	}
	return nil
}

func (adapter ZoteroSettingAdapter) Test(ctx context.Context, resolved settings.ResolvedSetting) ([]settings.ConnectionCheck, error) {
	if err := adapter.ValidateConfig(resolved.Values); err != nil {
		return []settings.ConnectionCheck{{Name: "configuration", Status: "failed", Message: "Zotero settings are incomplete"}}, err
	}
	libraryType := resolved.Values["library_type"].(string)
	libraryID := strings.TrimSpace(resolved.Values["library_id"].(string))
	apiKey := resolved.Values["api_key"].(string)
	endpoint := "https://api.zotero.org/" + libraryType + "s/" + url.PathEscape(libraryID) + "/items?limit=1&format=json"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Zotero-API-Key", apiKey)
	client := adapter.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return []settings.ConnectionCheck{{Name: "zotero", Status: "failed", Message: "Zotero is unavailable"}}, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusOK {
		return []settings.ConnectionCheck{{Name: "authentication", Status: "failed", Message: "Zotero rejected the library or API key"}}, fmt.Errorf("zotero connection status %d", response.StatusCode)
	}
	return []settings.ConnectionCheck{
		{Name: "authentication", Status: "passed"},
		{Name: "library", Status: "passed", Message: libraryType + "/" + libraryID},
	}, nil
}
