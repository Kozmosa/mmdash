package article

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

type zoteroRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip zoteroRoundTripper) Do(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestZoteroSettingDefinitionRegistersCompleteEditableContract(t *testing.T) {
	definition := SettingDefinitionZotero(nil)
	if definition.Key != SettingTypeZotero || definition.Tester == nil || definition.Validator == nil {
		t.Fatalf("incomplete Zotero setting definition: %#v", definition)
	}
	want := []string{"library_type", "library_id", "collection_key", "api_key"}
	if len(definition.Fields) != len(want) {
		t.Fatalf("unexpected Zotero fields: %#v", definition.Fields)
	}
	for index, key := range want {
		if definition.Fields[index].Key != key {
			t.Fatalf("field %d = %q, want %q", index, definition.Fields[index].Key, key)
		}
	}
	if definition.Fields[3].Kind != settings.FieldSecret {
		t.Fatal("Zotero API key must remain encrypted")
	}
}

func TestZoteroSettingConnectionUsesReadOnlyLibraryRequest(t *testing.T) {
	adapter := ZoteroSettingAdapter{Client: zoteroRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != "https://api.zotero.org/groups/42/items?limit=1&format=json" {
			t.Fatalf("unexpected Zotero probe: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Zotero-API-Key") != "read-only-key" {
			t.Fatal("Zotero probe did not use the resolved encrypted key")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	})}
	checks, err := adapter.Test(context.Background(), settings.ResolvedSetting{Values: map[string]interface{}{
		"library_type": "group", "library_id": "42", "api_key": "read-only-key",
	}})
	if err != nil || len(checks) != 2 || checks[0].Status != "passed" || checks[1].Status != "passed" {
		t.Fatalf("unexpected Zotero checks: %#v, %v", checks, err)
	}
}
