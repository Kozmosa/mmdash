package repo

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestModuleServesImmutableReadRoutesAndValidatesQueries(t *testing.T) {
	reader, repository, head := readerFixture(t)
	repository.ProjectID = "project-1"
	store := &serviceStore{value: repository}
	module := Module{Service: Service{
		Access: &serviceAccess{}, Reads: &reader, Store: store,
	}}
	handler := module.ProjectHandler()

	for _, testCase := range []struct {
		path     string
		contains string
		status   int
	}{
		{
			path:     "/v1/projects/project-1/repository/branches",
			contains: `"name":"main"`, status: http.StatusOK,
		},
		{
			path: "/v1/projects/project-1/repository/commits" +
				"?workspace=code&limit=1",
			contains: `"resolved_revision":"` + head + `"`,
			status:   http.StatusOK,
		},
		{
			path: "/v1/projects/project-1/repository/tree?" +
				url.Values{
					"workspace": {"code"}, "revision": {head}, "path": {"dir"},
				}.Encode(),
			contains: `"path":"dir/中文 #.txt"`,
			status:   http.StatusOK,
		},
		{
			path: "/v1/projects/project-1/repository/content?" +
				url.Values{
					"workspace": {"code"}, "revision": {head},
					"path": {"dir/中文 #.txt"},
				}.Encode(),
			contains: `"preview_status":"text"`,
			status:   http.StatusOK,
		},
		{
			path: "/v1/projects/project-1/repository/commits" +
				"?workspace=code&limit=101",
			contains: `"code":"INVALID_REQUEST"`,
			status:   http.StatusBadRequest,
		},
	} {
		request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
		request.Header.Set("Authorization", "Bearer test")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != testCase.status ||
			!strings.Contains(response.Body.String(), testCase.contains) {
			t.Fatalf(
				"%s: got %d %s, want %d containing %s",
				testCase.path, response.Code, response.Body.String(),
				testCase.status, testCase.contains,
			)
		}
	}

	rawRequest := httptest.NewRequest(
		http.MethodGet,
		"/v1/projects/project-1/repository/raw?"+url.Values{
			"workspace": {"code"}, "revision": {head},
			"path": {"binary.bin"},
		}.Encode(),
		nil,
	)
	rawRequest.Header.Set("Authorization", "Bearer test")
	rawResponse := httptest.NewRecorder()
	handler.ServeHTTP(rawResponse, rawRequest)
	if rawResponse.Code != http.StatusOK ||
		!bytes.Equal(rawResponse.Body.Bytes(), []byte{'a', 0, 'b'}) {
		t.Fatalf("raw binary: got %d %v", rawResponse.Code, rawResponse.Body.Bytes())
	}
	if got := rawResponse.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("raw binary content type: got %q", got)
	}
	if got := rawResponse.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("raw binary CSP: got %q", got)
	}
	if got := rawResponse.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("raw binary nosniff: got %q", got)
	}

	lfsRequest := httptest.NewRequest(
		http.MethodGet,
		"/v1/projects/project-1/repository/raw?"+url.Values{
			"workspace": {"code"}, "revision": {head},
			"path": {"pointer.lfs"},
		}.Encode(),
		nil,
	)
	lfsRequest.Header.Set("Authorization", "Bearer test")
	lfsResponse := httptest.NewRecorder()
	handler.ServeHTTP(lfsResponse, lfsRequest)
	if lfsResponse.Code != http.StatusNotFound {
		t.Fatalf("LFS pointer raw response: got %d %s", lfsResponse.Code, lfsResponse.Body.String())
	}
}
