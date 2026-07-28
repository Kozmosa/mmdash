package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type requestFixture struct {
	Name string `json:"name"`
}

func (fixture requestFixture) Validate() error {
	if fixture.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func TestDecodeJSONRejectsUnknownTrailingAndInvalidValues(t *testing.T) {
	tests := []string{
		`{"name":"ok","unknown":true}`,
		`{"name":"ok"} {"name":"again"}`,
		`{"name":""}`,
	}
	for _, body := range tests {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		response := httptest.NewRecorder()
		var fixture requestFixture
		if DecodeJSON(response, request, &fixture) {
			t.Fatalf("expected invalid body to fail: %s", body)
		}
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unexpected status for %s: %d", body, response.Code)
		}
	}
}

func TestDecodeJSONAcceptsOneValidatedValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
	response := httptest.NewRecorder()
	var fixture requestFixture
	if !DecodeJSON(response, request, &fixture) {
		t.Fatalf("expected valid body, got %s", response.Body.String())
	}
	if fixture.Name != "ok" {
		t.Fatalf("unexpected decoded fixture: %#v", fixture)
	}
}
