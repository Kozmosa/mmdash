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
	claimedReplacement   int
	completedReplacement int
	created              int
	disconnected         int
	reconnected          int
	releasedReplacement  int
	snapshot             ConnectionSnapshot
	value                Repository
}

type replacementStorage struct {
	err     error
	removed []string
}

func (storage *replacementStorage) RemoveRepository(storageKey string) error {
	storage.removed = append(storage.removed, storageKey)
	return storage.err
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

func (store *serviceStore) CompleteReplacement(
	_ context.Context,
	repositoryID string,
) error {
	if store.value.ID != repositoryID ||
		store.value.SyncLockedBy == nil ||
		*store.value.SyncLockedBy != "repo-replace" {
		return ErrConflict
	}
	store.completedReplacement++
	store.value = Repository{}
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
		Status: StatusPending,
	}
	return store.value, nil
}

func (store *serviceStore) ClaimReplacement(
	_ context.Context,
	projectID string,
	now time.Time,
	lease time.Duration,
) (Repository, error) {
	if store.value.ProjectID != projectID {
		return Repository{}, ErrNotConfigured
	}
	if store.value.Status != StatusDisconnected {
		return Repository{}, ErrAlreadyConnected
	}
	if store.value.SyncLockedBy != nil {
		return Repository{}, ErrReconnectExpired
	}
	owner := "repo-replace"
	expiresAt := now.Add(lease)
	store.claimedReplacement++
	store.value.SyncLockedBy = &owner
	store.value.SyncLeaseExpiresAt = &expiresAt
	return store.value, nil
}

func (store *serviceStore) Disconnect(
	_ context.Context,
	_ string,
	cleanupAfter time.Time,
	now time.Time,
) error {
	store.disconnected++
	store.value.Status = StatusDisconnected
	store.value.CleanupAfter = &cleanupAfter
	store.value.UpdatedAt = now
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

func (store *serviceStore) ReconnectPending(
	_ context.Context,
	snapshot ConnectionSnapshot,
	now time.Time,
) (Repository, error) {
	if store.value.Status != StatusDisconnected {
		return Repository{}, ErrAlreadyConnected
	}
	if store.value.Provider != snapshot.Provider ||
		store.value.CanonicalRemoteURL != snapshot.CanonicalRemoteURL {
		return Repository{}, ErrReconnectMismatch
	}
	if store.value.CleanupAfter == nil ||
		!store.value.CleanupAfter.After(now) ||
		store.value.SyncLockedBy != nil {
		return Repository{}, ErrReconnectExpired
	}
	store.reconnected++
	store.snapshot = snapshot
	store.value.DefaultBranch = snapshot.DefaultBranch
	store.value.DisplayName = snapshot.DisplayName
	store.value.SettingsVersion = snapshot.SettingsVersion
	store.value.Status = StatusPending
	store.value.CleanupAfter = nil
	store.value.SyncRequestedAt = &now
	store.value.UpdatedAt = now
	store.value.Workspaces = mappingList(snapshot.Workspaces, now)
	return store.value, nil
}

func (store *serviceStore) ReleaseReplacement(
	_ context.Context,
	repositoryID string,
	cleanupAfter time.Time,
	now time.Time,
) error {
	if store.value.ID != repositoryID {
		return ErrNotConfigured
	}
	store.releasedReplacement++
	store.value.CleanupAfter = &cleanupAfter
	store.value.SyncLockedBy = nil
	store.value.SyncLeaseExpiresAt = nil
	store.value.UpdatedAt = now
	return nil
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
		context.Background(), identity, "project-1",
		ConnectRequest{SettingsVersion: 7},
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
		ConnectRequest{SettingsVersion: 2},
	)
	if !errors.Is(err, ErrConflict) || store.created != 0 {
		t.Fatalf("expected stale version conflict, got %v", err)
	}
}

func TestServiceConnectRestoresDisconnectedRepositoryInPlace(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	cleanupAfter := now.Add(time.Hour)
	settingSource := &serviceSettings{resolved: coordinatorResolvedSetting()}
	providers := provider.NewRegistry()
	if err := providers.Register("local", coordinatorProvider{}); err != nil {
		t.Fatal(err)
	}
	store := &serviceStore{value: Repository{
		CanonicalRemoteURL: "C:/repositories/source",
		CleanupAfter:       &cleanupAfter,
		ID:                 "repository-1",
		ProjectID:          "project-1",
		Provider:           ProviderLocal,
		Status:             StatusDisconnected,
		StorageKey:         "storage-1",
		Webhook:            Webhook{HookID: "hook-1"},
	}}
	service := Service{
		Access: &serviceAccess{}, Clock: clock.Fixed{Time: now},
		Providers: providers, Settings: settingSource, Store: store,
	}

	restored, err := service.Connect(
		context.Background(),
		auth.Identity{User: auth.User{ID: "user-1"}},
		"project-1",
		ConnectRequest{SettingsVersion: 1},
	)
	if err != nil {
		t.Fatalf("restore disconnected repository: %v", err)
	}
	if store.created != 0 || store.reconnected != 1 {
		t.Fatalf(
			"unexpected persistence path: created=%d reconnected=%d",
			store.created, store.reconnected,
		)
	}
	if restored.ID != "repository-1" ||
		restored.StorageKey != "storage-1" ||
		restored.Status != StatusPending ||
		restored.CleanupAfter != nil {
		t.Fatalf("repository was not restored in place: %#v", restored)
	}
	if len(restored.Workspaces) != 3 ||
		restored.Workspaces[0].RemoteBranch != "main" {
		t.Fatalf("workspace mappings were not restored: %#v", restored.Workspaces)
	}
}

func TestServiceConnectRejectsDifferentRemoteDuringRecoveryGrace(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	cleanupAfter := now.Add(time.Hour)
	settingSource := &serviceSettings{resolved: coordinatorResolvedSetting()}
	settingSource.resolved.Values["remote_url"] = "C:/repositories/different"
	providers := provider.NewRegistry()
	if err := providers.Register("local", coordinatorProvider{}); err != nil {
		t.Fatal(err)
	}
	store := &serviceStore{value: Repository{
		CanonicalRemoteURL: "C:/repositories/source",
		CleanupAfter:       &cleanupAfter,
		ID:                 "repository-1",
		ProjectID:          "project-1",
		Provider:           ProviderLocal,
		Status:             StatusDisconnected,
	}}
	service := Service{
		Access: &serviceAccess{}, Clock: clock.Fixed{Time: now},
		Providers: providers, Settings: settingSource, Store: store,
	}

	_, err := service.Connect(
		context.Background(),
		auth.Identity{User: auth.User{ID: "user-1"}},
		"project-1",
		ConnectRequest{SettingsVersion: 1},
	)
	if !errors.Is(err, ErrReconnectMismatch) ||
		store.created != 0 ||
		store.reconnected != 0 {
		t.Fatalf("expected reconnect remote mismatch, got %v", err)
	}
}

func TestServiceConnectReplacesDifferentDisconnectedRemoteAfterConfirmation(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	cleanupAfter := now.Add(time.Hour)
	settingSource := &serviceSettings{resolved: coordinatorResolvedSetting()}
	settingSource.resolved.Values["remote_url"] = "C:/repositories/different"
	providers := provider.NewRegistry()
	if err := providers.Register("local", coordinatorProvider{}); err != nil {
		t.Fatal(err)
	}
	store := &serviceStore{value: Repository{
		CanonicalRemoteURL: "C:/repositories/source",
		CleanupAfter:       &cleanupAfter,
		ID:                 "old-repository",
		ProjectID:          "project-1",
		Provider:           ProviderLocal,
		Status:             StatusDisconnected,
		StorageKey:         "old-storage",
	}}
	storage := &replacementStorage{}
	service := Service{
		Access: &serviceAccess{}, Clock: clock.Fixed{Time: now},
		Providers: providers, Settings: settingSource, Storage: storage,
		Store: store,
	}

	replaced, err := service.Connect(
		context.Background(),
		auth.Identity{User: auth.User{ID: "user-1"}},
		"project-1",
		ConnectRequest{
			ReplaceDisconnected: true,
			SettingsVersion:     1,
		},
	)
	if err != nil {
		t.Fatalf("replace disconnected repository: %v", err)
	}
	if store.claimedReplacement != 1 ||
		store.completedReplacement != 1 ||
		store.releasedReplacement != 0 ||
		store.created != 1 {
		t.Fatalf("unexpected replacement lifecycle: %#v", store)
	}
	if len(storage.removed) != 1 || storage.removed[0] != "old-storage" {
		t.Fatalf("old managed storage was not removed: %#v", storage.removed)
	}
	if store.snapshot.CanonicalRemoteURL != "C:/repositories/different" ||
		replaced.Status != StatusPending {
		t.Fatalf("different repository was not bound: %#v", replaced)
	}
}

func TestServiceConnectPreservesDisconnectedBindingWhenReplacementCleanupFails(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	cleanupAfter := now.Add(time.Hour)
	settingSource := &serviceSettings{resolved: coordinatorResolvedSetting()}
	settingSource.resolved.Values["remote_url"] = "C:/repositories/different"
	providers := provider.NewRegistry()
	if err := providers.Register("local", coordinatorProvider{}); err != nil {
		t.Fatal(err)
	}
	store := &serviceStore{value: Repository{
		CanonicalRemoteURL: "C:/repositories/source",
		CleanupAfter:       &cleanupAfter,
		ID:                 "old-repository",
		ProjectID:          "project-1",
		Provider:           ProviderLocal,
		Status:             StatusDisconnected,
		StorageKey:         "old-storage",
	}}
	storage := &replacementStorage{err: errors.New("storage unavailable")}
	service := Service{
		Access: &serviceAccess{}, Clock: clock.Fixed{Time: now},
		Providers: providers, Settings: settingSource, Storage: storage,
		Store: store,
	}

	_, err := service.Connect(
		context.Background(),
		auth.Identity{User: auth.User{ID: "user-1"}},
		"project-1",
		ConnectRequest{
			ReplaceDisconnected: true,
			SettingsVersion:     1,
		},
	)
	if !errors.Is(err, ErrReplacementCleanup) ||
		store.claimedReplacement != 1 ||
		store.completedReplacement != 0 ||
		store.releasedReplacement != 1 ||
		store.created != 0 {
		t.Fatalf("failed replacement did not preserve old binding: %v %#v", err, store)
	}
	if store.value.ID != "old-repository" ||
		store.value.SyncLockedBy != nil ||
		store.value.CleanupAfter == nil ||
		!store.value.CleanupAfter.Equal(cleanupAfter) {
		t.Fatalf("old disconnected binding was not restored: %#v", store.value)
	}
}

func TestServiceConnectRejectsRecoveryAfterCleanupLease(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	cleanupAfter := now.Add(time.Hour)
	cleanupOwner := "repo-cleanup"
	settingSource := &serviceSettings{resolved: coordinatorResolvedSetting()}
	providers := provider.NewRegistry()
	if err := providers.Register("local", coordinatorProvider{}); err != nil {
		t.Fatal(err)
	}
	store := &serviceStore{value: Repository{
		CanonicalRemoteURL: "C:/repositories/source",
		CleanupAfter:       &cleanupAfter,
		ID:                 "repository-1",
		ProjectID:          "project-1",
		Provider:           ProviderLocal,
		Status:             StatusDisconnected,
		SyncLockedBy:       &cleanupOwner,
	}}
	service := Service{
		Access: &serviceAccess{}, Clock: clock.Fixed{Time: now},
		Providers: providers, Settings: settingSource, Store: store,
	}

	_, err := service.Connect(
		context.Background(),
		auth.Identity{User: auth.User{ID: "user-1"}},
		"project-1",
		ConnectRequest{SettingsVersion: 1},
	)
	if !errors.Is(err, ErrReconnectExpired) ||
		store.created != 0 ||
		store.reconnected != 0 {
		t.Fatalf("expected expired reconnect lease, got %v", err)
	}
}

func TestServiceDisconnectIsIdempotent(t *testing.T) {
	access := &serviceAccess{}
	store := &serviceStore{value: Repository{
		ID: "repository-1", ProjectID: "project-1",
	}}
	service := Service{
		Access: access, Clock: clock.Fixed{Time: time.Now()}, Store: store,
	}
	identity := auth.Identity{User: auth.User{ID: "user-1"}}

	if err := service.Disconnect(
		context.Background(), identity, "project-1",
	); err != nil {
		t.Fatalf("disconnect repository: %v", err)
	}
	if err := service.Disconnect(
		context.Background(), identity, "project-1",
	); err != nil {
		t.Fatalf("repeat disconnect repository: %v", err)
	}
	if store.disconnected != 1 {
		t.Fatalf("expected one stored disconnect, got %d", store.disconnected)
	}
	if access.permission != project.PermissionRepoManage {
		t.Fatalf("unexpected permission: %s", access.permission)
	}
}

var _ Store = (*serviceStore)(nil)
