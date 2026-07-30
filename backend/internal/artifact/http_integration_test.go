package artifact

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

func TestCoreHTTPArtifactLifecycle(t *testing.T) {
	coreURL := strings.TrimRight(os.Getenv("MMDASH_TEST_CORE_URL"), "/")
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	password := os.Getenv("MMDASH_TEST_ADMIN_PASSWORD")
	if coreURL == "" || databaseURL == "" || password == "" {
		t.Skip("Core HTTP integration settings are not configured")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	client := &http.Client{Timeout: 30 * time.Second}
	loginResponse := struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}{}
	status, err := requestJSON(
		ctx, client, http.MethodPost, coreURL+"/v1/auth/login", "",
		map[string]interface{}{
			"email": "admin@mmdash.local", "password": password,
		},
		&loginResponse,
	)
	if err != nil || status != http.StatusOK || loginResponse.AccessToken == "" {
		t.Fatalf("login to Core: status=%d err=%v", status, err)
	}
	token := loginResponse.AccessToken
	t.Cleanup(func() {
		request, _ := http.NewRequest(
			http.MethodPost, coreURL+"/v1/auth/logout", nil,
		)
		request.Header.Set("Authorization", "Bearer "+token)
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
		}
	})

	projectID := identity.Generator{}.MustNew()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(
			project_id,name,created_by,created_at,updated_at
		) VALUES($1,'Artifact HTTP integration',$2,$3,$3)
	`, projectID, loginResponse.User.ID, now); err != nil {
		t.Fatalf("insert HTTP test project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members(
			project_id,user_id,role,created_at,updated_at
		) VALUES($1,$2,'owner',$3,$3)
	`, projectID, loginResponse.User.ID, now); err != nil {
		t.Fatalf("insert HTTP test membership: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM artifact_artifacts WHERE project_id=$1`,
			projectID,
		)
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM artifact_blobs WHERE project_id=$1`,
			projectID,
		)
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM projects WHERE project_id=$1`,
			projectID,
		)
	})

	unauthorized, err := http.Get(
		coreURL + "/v1/projects/" + projectID + "/artifacts",
	)
	if err != nil {
		t.Fatalf("unauthorized Artifact list: %v", err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.StatusCode)
	}

	contents := []byte("Core HTTP Artifact lifecycle " + projectID)
	var upload PublicUploadSession
	status, err = requestJSON(
		ctx, client, http.MethodPost,
		coreURL+"/v1/projects/"+projectID+"/artifacts/uploads", token,
		map[string]interface{}{
			"filename": "http.txt", "name": "HTTP Artifact",
			"size_bytes": len(contents), "sha256": digest(contents),
			"mime_type": "text/plain", "kind": KindAttachment,
			"tags":            []string{"http", "acceptance"},
			"idempotency_key": "http-" + projectID,
		},
		&upload,
	)
	if err != nil || status != http.StatusCreated ||
		upload.UploadID == "" || upload.PartCount != 1 {
		t.Fatalf("initialize HTTP upload: %#v status=%d err=%v", upload, status, err)
	}
	var grants PartGrantList
	status, err = requestJSON(
		ctx, client, http.MethodPost,
		coreURL+"/v1/projects/"+projectID+"/artifacts/uploads/"+
			upload.UploadID+"/parts/sign",
		token, map[string]interface{}{"part_numbers": []int{1}}, &grants,
	)
	if err != nil || status != http.StatusOK || len(grants.Items) != 1 {
		t.Fatalf("sign HTTP upload: %#v status=%d err=%v", grants, status, err)
	}
	putRequest, err := http.NewRequestWithContext(
		ctx, http.MethodPut, grants.Items[0].Transfer.URL,
		bytes.NewReader(contents),
	)
	if err != nil {
		t.Fatalf("create signed PUT: %v", err)
	}
	putRequest.ContentLength = int64(len(contents))
	for key, value := range grants.Items[0].Transfer.Headers {
		putRequest.Header.Set(key, value)
	}
	putResponse, err := client.Do(putRequest)
	if err != nil {
		t.Fatalf("perform signed PUT: %v", err)
	}
	_, _ = io.Copy(io.Discard, putResponse.Body)
	putResponse.Body.Close()
	if putResponse.StatusCode != http.StatusOK &&
		putResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected signed PUT status: %d", putResponse.StatusCode)
	}
	etag := putResponse.Header.Get("ETag")
	if etag == "" {
		t.Fatal("signed PUT did not return ETag")
	}

	var recovered PublicUploadSession
	status, err = requestJSON(
		ctx, client, http.MethodGet,
		coreURL+"/v1/projects/"+projectID+"/artifacts/uploads/"+
			upload.UploadID,
		token, nil, &recovered,
	)
	if err != nil || status != http.StatusOK ||
		len(recovered.CompletedParts) != 1 {
		t.Fatalf(
			"recover HTTP upload: %#v status=%d err=%v",
			recovered, status, err,
		)
	}
	var detail Detail
	status, err = requestJSON(
		ctx, client, http.MethodPost,
		coreURL+"/v1/projects/"+projectID+"/artifacts/uploads/"+
			upload.UploadID+"/confirm",
		token, map[string]interface{}{
			"parts": []map[string]interface{}{{
				"part_number": 1, "etag": etag,
			}},
		}, &detail,
	)
	if err != nil || status != http.StatusCreated ||
		detail.CurrentVersion == nil ||
		detail.CurrentVersion.Status != StatusAvailable {
		t.Fatalf("confirm HTTP upload: %#v status=%d err=%v", detail, status, err)
	}

	var page Page
	status, err = requestJSON(
		ctx, client, http.MethodGet,
		coreURL+"/v1/projects/"+projectID+
			"/artifacts?kind=attachment&tag=http",
		token, nil, &page,
	)
	if err != nil || status != http.StatusOK || len(page.Items) != 1 {
		t.Fatalf("list HTTP Artifacts: %#v status=%d err=%v", page, status, err)
	}
	var updated Detail
	status, err = requestJSON(
		ctx, client, http.MethodPatch,
		coreURL+"/v1/projects/"+projectID+"/artifacts/"+detail.Artifact.ID,
		token, map[string]interface{}{
			"name": "Updated HTTP Artifact", "tags": []string{"verified"},
			"description": nil,
		}, &updated,
	)
	if err != nil || status != http.StatusOK ||
		updated.Artifact.Name != "Updated HTTP Artifact" ||
		updated.Artifact.Description != nil {
		t.Fatalf("update HTTP Artifact: %#v status=%d err=%v", updated, status, err)
	}
	var download DownloadGrant
	status, err = requestJSON(
		ctx, client, http.MethodPost,
		coreURL+"/v1/projects/"+projectID+"/artifacts/"+
			detail.Artifact.ID+"/download",
		token, nil, &download,
	)
	if err != nil || status != http.StatusOK ||
		download.Transfer.URL == "" {
		t.Fatalf("sign HTTP download: %#v status=%d err=%v", download, status, err)
	}
	downloadResponse, err := client.Get(download.Transfer.URL)
	if err != nil {
		t.Fatalf("perform HTTP download: %v", err)
	}
	downloaded, readErr := io.ReadAll(downloadResponse.Body)
	downloadResponse.Body.Close()
	if readErr != nil ||
		downloadResponse.StatusCode != http.StatusOK ||
		!bytes.Equal(downloaded, contents) {
		t.Fatalf(
			"download HTTP Artifact: status=%d bytes=%q err=%v",
			downloadResponse.StatusCode, downloaded, readErr,
		)
	}

	status, err = requestJSON(
		ctx, client, http.MethodDelete,
		coreURL+"/v1/projects/"+projectID+"/artifacts/"+detail.Artifact.ID,
		token, nil, nil,
	)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("trash HTTP Artifact: status=%d err=%v", status, err)
	}
	status, err = requestJSON(
		ctx, client, http.MethodGet,
		coreURL+"/v1/projects/"+projectID+"/artifacts/"+
			detail.Artifact.ID,
		token, nil, nil,
	)
	if err == nil || status != http.StatusNotFound {
		t.Fatalf("trashed Artifact should be hidden: status=%d err=%v", status, err)
	}
	var trash Page
	status, err = requestJSON(
		ctx, client, http.MethodGet,
		coreURL+"/v1/projects/"+projectID+"/artifacts/trash",
		token, nil, &trash,
	)
	if err != nil || status != http.StatusOK || len(trash.Items) != 1 {
		t.Fatalf("list Artifact trash: %#v status=%d err=%v", trash, status, err)
	}
	var restored Detail
	status, err = requestJSON(
		ctx, client, http.MethodPost,
		coreURL+"/v1/projects/"+projectID+"/artifacts/"+
			detail.Artifact.ID+"/restore",
		token, nil, &restored,
	)
	if err != nil || status != http.StatusOK ||
		restored.Artifact.Status != StatusAvailable {
		t.Fatalf("restore HTTP Artifact: %#v status=%d err=%v", restored, status, err)
	}
	status, err = requestJSON(
		ctx, client, http.MethodDelete,
		coreURL+"/v1/projects/"+projectID+"/artifacts/"+detail.Artifact.ID,
		token, nil, nil,
	)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("re-trash HTTP Artifact: status=%d err=%v", status, err)
	}
	status, err = requestJSON(
		ctx, client, http.MethodDelete,
		coreURL+"/v1/projects/"+projectID+"/artifacts/"+
			detail.Artifact.ID+"/purge",
		token, nil, nil,
	)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("purge HTTP Artifact: status=%d err=%v", status, err)
	}

	var confirmedAudits int
	var leakedAudits int
	if err := db.QueryRowContext(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE action='artifact.upload.confirmed'),
		  COUNT(*) FILTER (
		    WHERE metadata::text ~* '(x-amz|provider_upload|staging_key|secret)'
		  )
		FROM audit_events
		WHERE project_id=$1 AND category='artifact'
	`, projectID).Scan(&confirmedAudits, &leakedAudits); err != nil {
		t.Fatalf("read Artifact audit trail: %v", err)
	}
	if confirmedAudits != 1 || leakedAudits != 0 {
		t.Fatalf(
			"unexpected Artifact audit trail: confirmed=%d leaked=%d",
			confirmedAudits, leakedAudits,
		)
	}
	metricsResponse, err := client.Get(coreURL + "/metrics")
	if err != nil {
		t.Fatalf("read Core metrics: %v", err)
	}
	metricsBody, metricsErr := io.ReadAll(metricsResponse.Body)
	metricsResponse.Body.Close()
	if metricsErr != nil ||
		!bytes.Contains(metricsBody, []byte("mmdash_artifact_operations_total")) {
		t.Fatalf("Artifact metrics unavailable: %v", metricsErr)
	}
}

func requestJSON(
	ctx context.Context,
	client *http.Client,
	method string,
	url string,
	token string,
	body interface{},
	target interface{},
) (int, error) {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, requestBody)
	if err != nil {
		return 0, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		contents, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return response.StatusCode, fmt.Errorf(
			"Core returned %d: %s",
			response.StatusCode, strings.TrimSpace(string(contents)),
		)
	}
	if target == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode, nil
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return response.StatusCode, err
	}
	return response.StatusCode, nil
}
