package repo

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type coordinatorStore struct {
	claims      []SyncClaim
	completed   []string
	failedCode  string
	failedRetry time.Time
	retryable   bool
	mutex       sync.Mutex
	renewals    int
}

func (store *coordinatorStore) ClaimSync(
	context.Context, string, time.Time, time.Duration, int,
) ([]SyncClaim, error) {
	return append([]SyncClaim(nil), store.claims...), nil
}

func (store *coordinatorStore) CompleteSync(
	_ context.Context,
	_ string,
	claim SyncClaim,
	_ SyncResult,
	_ time.Time,
) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.completed = append(store.completed, claim.Repository.ID)
	return nil
}

func (store *coordinatorStore) FailSync(
	_ context.Context,
	_ string,
	_ SyncClaim,
	failure SyncFailure,
	retryAt time.Time,
	_ time.Time,
) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.failedCode = failure.Code
	store.failedRetry = retryAt
	store.retryable = failure.Retryable
	return nil
}

func (store *coordinatorStore) RenewSyncLease(
	context.Context, string, string, time.Time,
) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.renewals++
	return nil
}

type coordinatorSettings struct {
	resolved settings.ResolvedSetting
}

func (source coordinatorSettings) Resolve(
	context.Context, settings.Scope, string, string,
) (settings.ResolvedSetting, error) {
	return source.resolved, nil
}

type coordinatorProvider struct {
	err error
}

func (adapter coordinatorProvider) Test(
	_ context.Context,
	config provider.Config,
) (provider.Connection, error) {
	if adapter.err != nil {
		return provider.Connection{}, adapter.err
	}
	return provider.Connection{
		Branches: map[string]string{
			config.CodeBranch: "a", config.ArticleBranch: "b",
			config.ResultBranch: "c",
		},
		CanonicalRemoteURL: config.RemoteURL,
		DefaultBranch:      "main", DisplayName: "test", FetchURL: config.RemoteURL,
		Provider: config.Provider,
	}, nil
}

type coordinatorRuntime struct {
	delay     time.Duration
	maxActive int
	mutex     sync.Mutex
	active    int
}

func (runtime *coordinatorRuntime) Synchronize(
	ctx context.Context,
	repository Repository,
	_ provider.Connection,
	source string,
) (SyncResult, error) {
	runtime.mutex.Lock()
	runtime.active++
	if runtime.active > runtime.maxActive {
		runtime.maxActive = runtime.active
	}
	runtime.mutex.Unlock()
	defer func() {
		runtime.mutex.Lock()
		runtime.active--
		runtime.mutex.Unlock()
	}()
	if runtime.delay > 0 {
		timer := time.NewTimer(runtime.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return SyncResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	return SyncResult{
		Source: source,
		Workspaces: []SyncedWorkspace{
			{Workspace: WorkspaceCode},
			{Workspace: WorkspaceArticle},
			{Workspace: WorkspaceResult},
		},
	}, nil
}

func TestCoordinatorRunsClaimsConcurrentlyAndRenewsLeases(t *testing.T) {
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	store := &coordinatorStore{claims: []SyncClaim{
		{Repository: Repository{ID: "repo-1", ProjectID: "project-1"}, Requested: now, Source: "manual"},
		{Repository: Repository{ID: "repo-2", ProjectID: "project-2"}, Requested: now, Source: "manual"},
	}}
	providers := provider.NewRegistry()
	if err := providers.Register("server_existing", coordinatorProvider{}); err != nil {
		t.Fatal(err)
	}
	runtime := &coordinatorRuntime{delay: 80 * time.Millisecond}
	coordinator := Coordinator{
		BatchSize: 2, Clock: clock.Fixed{Time: now}, Lease: 30 * time.Millisecond,
		Owner: "core-test", Providers: providers, Runtime: runtime,
		Settings: coordinatorSettings{resolved: coordinatorResolvedSetting()},
		Store:    store,
	}
	if err := coordinator.RunOnce(context.Background()); err != nil {
		t.Fatalf("run coordinator: %v", err)
	}
	if len(store.completed) != 2 || runtime.maxActive != 2 {
		t.Fatalf("claims were not concurrent: %#v max=%d", store.completed, runtime.maxActive)
	}
	if store.renewals < 2 {
		t.Fatalf("expected lease renewals, got %d", store.renewals)
	}
}

func TestCoordinatorPersistsRetryableNetworkFailureWithExponentialBackoff(t *testing.T) {
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	store := &coordinatorStore{claims: []SyncClaim{{
		Repository: Repository{
			ID: "repo-1", ProjectID: "project-1", SyncAttempts: 3,
		},
		Requested: now, Source: "manual",
	}}}
	providers := provider.NewRegistry()
	if err := providers.Register("server_existing", coordinatorProvider{
		err: provider.ErrNetworkUnavailable,
	}); err != nil {
		t.Fatal(err)
	}
	var reported error
	coordinator := Coordinator{
		Clock: clock.Fixed{Time: now}, Lease: time.Minute,
		OnError: func(err error) {
			reported = err
		},
		Owner: "core-test", Providers: providers,
		Runtime:  &coordinatorRuntime{},
		Settings: coordinatorSettings{resolved: coordinatorResolvedSetting()},
		Store:    store, RetryBase: time.Second, RetryLimit: time.Minute,
	}
	if err := coordinator.RunOnce(context.Background()); err != nil {
		t.Fatalf("run coordinator: %v", err)
	}
	if store.failedCode != "REPO_NETWORK_UNAVAILABLE" || !store.retryable {
		t.Fatalf("unexpected failure code: %s", store.failedCode)
	}
	if !store.failedRetry.Equal(now.Add(8 * time.Second)) {
		t.Fatalf("unexpected retry time: %s", store.failedRetry)
	}
	if !errors.Is(reported, provider.ErrNetworkUnavailable) {
		t.Fatalf("provider failure was not reported: %v", reported)
	}
	var syncFailure *SyncFailureError
	if !errors.As(reported, &syncFailure) || syncFailure.Code != "REPO_NETWORK_UNAVAILABLE" ||
		!syncFailure.Retryable || syncFailure.RepositoryID != "repo-1" {
		t.Fatalf("safe structured sync failure missing: %#v", syncFailure)
	}
}

func TestCoordinatorStopsAutomaticRetryForAuthenticationFailure(t *testing.T) {
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	store := &coordinatorStore{claims: []SyncClaim{{
		Repository: Repository{ID: "repo-1", ProjectID: "project-1"},
		Requested:  now, Source: "manual",
	}}}
	providers := provider.NewRegistry()
	if err := providers.Register("server_existing", coordinatorProvider{
		err: provider.ErrAuthentication,
	}); err != nil {
		t.Fatal(err)
	}
	coordinator := Coordinator{
		Clock: clock.Fixed{Time: now}, Lease: time.Minute,
		Owner: "core-test", Providers: providers, Runtime: &coordinatorRuntime{},
		Settings: coordinatorSettings{resolved: coordinatorResolvedSetting()},
		Store:    store,
	}
	if err := coordinator.RunOnce(context.Background()); err != nil {
		t.Fatalf("run coordinator: %v", err)
	}
	if store.failedCode != "REPO_AUTH_FAILED" || store.retryable {
		t.Fatalf("authentication failure should be terminal: %#v", store)
	}
}

func coordinatorResolvedSetting() settings.ResolvedSetting {
	return settings.ResolvedSetting{
		Scope: settings.ScopeProject, ScopeID: "project", TypeKey: SettingType,
		Values: map[string]interface{}{
			"provider": "server_existing", "remote_url": "C:/repositories/source",
			"code_branch": "main", "article_branch": "article",
			"result_branch": "result",
		},
		Version: 1,
	}
}
