package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type serviceAccess struct {
	permission project.Permission
}

func (access *serviceAccess) Authenticate(
	context.Context, string,
) (auth.Identity, error) {
	return auth.Identity{User: auth.User{ID: "user-1"}}, nil
}

func (access *serviceAccess) Authorize(
	_ context.Context,
	_ auth.Identity,
	_ string,
	permission project.Permission,
) error {
	access.permission = permission
	return nil
}

type serviceSettings struct {
	resolved settings.ResolvedSetting
}

func (source *serviceSettings) Resolve(
	context.Context, settings.Scope, string, string,
) (settings.ResolvedSetting, error) {
	return source.resolved, nil
}

func (source *serviceSettings) Update(
	_ context.Context,
	_ auth.Identity,
	_ settings.Scope,
	_ string,
	_ string,
	patch map[string]interface{},
) (settings.Setting, error) {
	for key, value := range patch {
		source.resolved.Values[key] = value
	}
	source.resolved.Version++
	return settings.Setting{Version: source.resolved.Version}, nil
}

type serviceStore struct {
	created  int
	snapshot ConnectionSnapshot
	value    Repository
}

func (*serviceStore) ClaimSync(
	context.Context, string, time.Time, time.Duration, int,
) ([]SyncClaim, error) {
	return nil, nil
}

func (*serviceStore) CompleteSync(
	context.Context, string, SyncClaim, SyncResult, time.Time,
) error {
	return nil
}

func (store *serviceStore) CreatePending(
	_ context.Context,
	_ string,
	snapshot ConnectionSnapshot,
) (Repository, error) {
	store.created++
	store.snapshot = snapshot
	store.value = Repository{
		ID: "repository-1", ProjectID: snapshot.ProjectID,
		Provider: snapshot.Provider, Webhook: Webhook{HookID: "hook-1"},
	}
	return store.value, nil
}

func (*serviceStore) Disconnect(context.Context, string, time.Time, time.Time) error {
	return nil
}

func (*serviceStore) FailSync(
	context.Context, string, string, string, string, time.Time, time.Time,
) error {
	return nil
}

func (store *serviceStore) GetByHook(context.Context, string) (Repository, error) {
	return store.value, nil
}

func (store *serviceStore) GetByID(context.Context, string) (Repository, error) {
	return store.value, nil
}

func (store *serviceStore) GetByProject(context.Context, string) (Repository, error) {
	if store.value.ID == "" {
		return Repository{}, ErrNotConfigured
	}
	return store.value, nil
}

func (store *serviceStore) ListRepositories(context.Context) ([]Repository, error) {
	if store.value.ID == "" {
		return []Repository{}, nil
	}
	return []Repository{store.value}, nil
}

func (*serviceStore) RenewSyncLease(context.Context, string, string, time.Time) error {
	return nil
}

func (store *serviceStore) RequestSync(
	context.Context, string, time.Time,
) (Repository, error) {
	return store.value, nil
}

func (store *serviceStore) RequestSyncSource(
	context.Context, string, time.Time, string,
) (Repository, error) {
	return store.value, nil
}

func (store *serviceStore) UpdateMappings(
	context.Context, string, WorkspaceMappings, int64, time.Time,
) (Repository, error) {
	return store.value, nil
}

func TestServiceCommitRejectsUnsafeWritesBeforeGit(t *testing.T) {
	const head = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testCases := []struct {
		name   string
		mutate func(*WorkspaceCommitRequest)
	}{
		{
			name: "invalid workspace",
			mutate: func(request *WorkspaceCommitRequest) {
				request.Workspace = WorkspaceKind("unknown")
			},
		},
		{
			name: "symbolic head",
			mutate: func(request *WorkspaceCommitRequest) {
				request.ExpectedHeadSHA = "main"
			},
		},
		{
			name: "blank message",
			mutate: func(request *WorkspaceCommitRequest) {
				request.Message = " "
			},
		},
		{
			name: "duplicate path",
			mutate: func(request *WorkspaceCommitRequest) {
				request.Changes = append(
					request.Changes, request.Changes[0],
				)
			},
		},
		{
			name: "path traversal",
			mutate: func(request *WorkspaceCommitRequest) {
				request.Changes[0].Path = "../outside"
			},
		},
		{
			name: "delete with content",
			mutate: func(request *WorkspaceCommitRequest) {
				request.Changes[0].Operation = "delete"
			},
		},
		{
			name: "unsupported operation",
			mutate: func(request *WorkspaceCommitRequest) {
				request.Changes[0].Operation = "chmod"
			},
		},
		{
			name: "payload too large",
			mutate: func(request *WorkspaceCommitRequest) {
				request.Changes[0].Content = []byte("too large")
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			access := &serviceAccess{}
			service := Service{
				Access: access, MaxWriteBytes: 4,
			}
			request := WorkspaceCommitRequest{
				Changes: []FileChange{{
					Content: []byte("safe"), Operation: "put", Path: "safe.txt",
				}},
				ExpectedHeadSHA: head, IdempotencyKey: "write-1",
				Message: "test commit", ProjectID: "project-1",
				Workspace: WorkspaceCode,
			}
			testCase.mutate(&request)
			_, err := service.Commit(
				context.Background(),
				auth.Identity{User: auth.User{
					DisplayName: "Writer", Email: "writer@example.test",
					ID: "user-1",
				}},
				request,
			)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected invalid input, got %v", err)
			}
			if access.permission != project.PermissionRepoWrite {
				t.Fatalf("unexpected permission: %s", access.permission)
			}
		})
	}
}

func TestServiceCommitRequestHashBindsActorAndContents(t *testing.T) {
	service := Service{MaxWriteBytes: 1024}
	request := WorkspaceCommitRequest{
		ActorID: "user-1",
		Changes: []FileChange{{
			Content: []byte("safe"), Operation: "put", Path: "safe.txt",
		}},
		ExpectedHeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IdempotencyKey:  "write-1",
		Message:         "test commit",
		ProjectID:       "project-1",
		Workspace:       WorkspaceCode,
	}
	if err := service.validateCommitRequest(&request); err != nil {
		t.Fatalf("validate commit request: %v", err)
	}
	firstHash := request.RequestSHA256
	if len(firstHash) != 64 {
		t.Fatalf("unexpected request hash: %q", firstHash)
	}
	request.ActorID = "user-2"
	if err := service.validateCommitRequest(&request); err != nil {
		t.Fatalf("validate changed request: %v", err)
	}
	if request.RequestSHA256 == firstHash {
		t.Fatal("request hash did not bind the authenticated actor")
	}
}

func TestServiceConnectUsesTestedSettingsAndReturnsWebhookSecretOnce(t *testing.T) {
	access := &serviceAccess{}
	settingSource := &serviceSettings{resolved: settings.ResolvedSetting{
		Scope: settings.ScopeProject, ScopeID: "project-1", TypeKey: SettingType,
		Values: map[string]interface{}{
			"provider": "github", "remote_url": "https://github.com/acme/repo",
			"code_branch": "main", "article_branch": "article",
			"result_branch": "result",
		},
		Version: 7,
	}}
	providers := provider.NewRegistry()
	if err := providers.Register("github", coordinatorProvider{}); err != nil {
		t.Fatal(err)
	}
	store := &serviceStore{}
	service := Service{
		Access: access, Clock: clock.Fixed{Time: time.Now()},
		Providers: providers, PublicURL: "https://mmdash.example",
		Settings: settingSource, Store: store,
	}
	identity := auth.Identity{User: auth.User{ID: "user-1"}}
	connected, err := service.Connect(
		context.Background(), identity, "project-1", 7,
	)
	if err != nil {
		t.Fatalf("connect repository: %v", err)
	}
	if store.created != 1 || store.snapshot.SettingsVersion != 8 {
		t.Fatalf("unexpected persisted snapshot: %#v", store.snapshot)
	}
	if store.snapshot.CanonicalRemoteURL != "https://github.com/acme/repo" {
		t.Fatalf("unexpected canonical remote: %s", store.snapshot.CanonicalRemoteURL)
	}
	if connected.Webhook.Secret == "" ||
		connected.Webhook.PublicURL !=
			"https://mmdash.example/api/webhooks/github/hook-1" ||
		!connected.Webhook.SecretConfigured {
		t.Fatalf("unexpected initial webhook response: %#v", connected.Webhook)
	}
	got, err := service.Get(context.Background(), identity, "project-1")
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	if got.Webhook.Secret != "" || !got.Webhook.SecretConfigured {
		t.Fatalf("webhook secret was exposed again: %#v", got.Webhook)
	}
	if access.permission != project.PermissionRepoRead {
		t.Fatalf("unexpected final permission: %s", access.permission)
	}
}

func TestServiceConnectRejectsStaleSettingsVersion(t *testing.T) {
	settingSource := &serviceSettings{resolved: coordinatorResolvedSetting()}
	settingSource.resolved.Version = 3
	providers := provider.NewRegistry()
	if err := providers.Register("local", coordinatorProvider{}); err != nil {
		t.Fatal(err)
	}
	store := &serviceStore{}
	service := Service{
		Access: &serviceAccess{}, Clock: clock.Fixed{Time: time.Now()},
		Providers: providers, Settings: settingSource, Store: store,
	}
	_, err := service.Connect(
		context.Background(),
		auth.Identity{User: auth.User{ID: "user-1"}},
		"project-1",
		2,
	)
	if !errors.Is(err, ErrConflict) || store.created != 0 {
		t.Fatalf("expected stale version conflict, got %v", err)
	}
}

var _ Store = (*serviceStore)(nil)
