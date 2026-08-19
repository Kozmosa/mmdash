package article

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/repo"
)

func TestArticleRepoReadinessErrorsRemainActionable(t *testing.T) {
	for _, test := range []struct {
		code string
		err  error
	}{
		{code: "ARTICLE_REPOSITORY_NOT_CONFIGURED", err: repo.ErrNotConfigured},
		{code: "ARTICLE_REPOSITORY_NOT_READY", err: repo.ErrNotReady},
	} {
		request := httptest.NewRequest(http.MethodPost, "/v1/projects/project-1/article/commits", nil)
		response := httptest.NewRecorder()
		writeError(response, request, test.err)
		if response.Code != http.StatusConflict {
			t.Fatalf("%s returned %d", test.code, response.Code)
		}
		var body map[string]interface{}
		if json.Unmarshal(response.Body.Bytes(), &body) != nil || body["code"] != test.code {
			t.Fatalf("unexpected Article error response: %s", response.Body.String())
		}
	}
}

func TestPublicationRetryRouteUsesPublicationWorkflow(t *testing.T) {
	store := &articleTestStore{
		builds:       []Build{{BuildID: "build-1", BuildKind: BuildFormal, CommitID: "commit-1", ProjectID: "project-1", Status: BuildFailed, TemplateID: "template-1"}},
		commit:       Commit{CommitID: "commit-1", ProjectID: "project-1"},
		publications: []Publication{{PublicationID: "publication-1", BuildID: "build-1", CommitID: "commit-1", ProjectID: "project-1", Status: "failed"}},
		template:     Template{TemplateID: "template-1", ArtifactID: "artifact-1", VersionID: "version-1", Status: "ready"},
	}
	service := testService(store, &articleTestWorkspace{})
	request := httptest.NewRequest(http.MethodPost, "/v1/projects/project-1/article/publications/publication-1/retry", nil)
	response := httptest.NewRecorder()
	(Module{Service: service}).handleProject(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("publication retry returned %d: %s", response.Code, response.Body.String())
	}
	if len(store.publications) != 1 || store.publications[0].Status != "building" || len(store.builds) != 2 {
		t.Fatalf("publication retry did not queue a replacement Build: %#v %#v", store.publications, store.builds)
	}
}
