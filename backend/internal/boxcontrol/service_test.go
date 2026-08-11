package boxcontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type revokeStoreStub struct {
	Store
	box     Box
	revoked bool
}

func (store *revokeStoreStub) Get(context.Context, string) (Box, error) { return store.box, nil }
func (store *revokeStoreStub) Revoke(_ context.Context, _ string, now time.Time) (Box, error) {
	store.revoked = true
	store.box.Status, store.box.UpdatedAt = StatusRevoked, now
	return store.box, nil
}

type revokeAccessStub struct{ allowed bool }

func (stub revokeAccessStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return auth.Identity{}, nil
}
func (stub revokeAccessStub) Authorize(context.Context, auth.Identity, string, project.Permission) error {
	if !stub.allowed {
		return auth.ErrForbidden
	}
	return nil
}

type tokenRevokerStub struct {
	called    bool
	projectID string
	kind      string
	tokenID   string
}

func (stub *tokenRevokerStub) RevokeManagedToken(_ context.Context, _ auth.Identity, projectID, kind, tokenID string) error {
	stub.called, stub.projectID, stub.kind, stub.tokenID = true, projectID, kind, tokenID
	return nil
}

func TestValidateBoxRejectsUnsupportedRuntimeAndUnsafeLimits(t *testing.T) {
	base := Box{
		ProjectID: "project-1", Name: "box", Version: "1", Capabilities: []Capability{{Name: "sandbox", Version: "1"}},
		Runtimes: []Runtime{{Name: "local-docker", Version: "1"}}, Limits: ResourceLimits{CPUMillis: 500, MemoryBytes: 1 << 20, TimeoutSecond: 30, DiskBytes: 1 << 20, PIDs: 32, Network: "disabled"},
	}
	if err := validateBox(&base, "project-1"); err != nil {
		t.Fatalf("valid Box rejected: %v", err)
	}
	base.Runtimes[0].Name = "arbitrary-shell"
	if err := validateBox(&base, "project-1"); err == nil {
		t.Fatal("unsupported runtime accepted")
	}
	base.Runtimes[0].Name = "local-docker"
	base.Limits.Network = "public"
	if err := validateBox(&base, "project-1"); err == nil {
		t.Fatal("unsupported network policy accepted")
	}
}

func TestValidateHeartbeatPreservesRegisteredIdentity(t *testing.T) {
	registered := Box{ProjectID: "project-1", Name: "box"}
	update := Box{
		Version: "2", Capabilities: []Capability{{Name: "sandbox", Version: "1"}},
		Runtimes: []Runtime{{Name: "e2b", Version: "1"}},
		Limits:   ResourceLimits{CPUMillis: 1000, MemoryBytes: 512 << 20, TimeoutSecond: 90, DiskBytes: 1 << 30, PIDs: 64, Network: "disabled"},
	}
	if err := validateHeartbeat(&update, registered); err != nil {
		t.Fatalf("valid heartbeat rejected: %v", err)
	}
	if update.Name != registered.Name || update.ProjectID != registered.ProjectID {
		t.Fatalf("registered identity was not preserved: %#v", update)
	}
}

func TestRevokeBoxRevokesNodeThenManagedCredential(t *testing.T) {
	store := &revokeStoreStub{box: Box{ID: "box-1", ProjectID: "project-1", Status: StatusOnline, TokenID: "token-1"}}
	revoker := &tokenRevokerStub{}
	service := Service{Access: revokeAccessStub{allowed: true}, Revoker: revoker, Store: store}
	if err := service.Revoke(context.Background(), auth.Identity{Kind: "session", User: auth.User{ID: "manager"}}, "box-1"); err != nil {
		t.Fatalf("revoke Box: %v", err)
	}
	if !store.revoked || !revoker.called || revoker.projectID != "project-1" || revoker.kind != "box" || revoker.tokenID != "token-1" {
		t.Fatalf("incomplete revoke lifecycle: store=%#v revoker=%#v", store, revoker)
	}
}

func TestRevokeBoxRequiresProjectManagement(t *testing.T) {
	store := &revokeStoreStub{box: Box{ID: "box-1", ProjectID: "project-1", Status: StatusOnline, TokenID: "token-1"}}
	revoker := &tokenRevokerStub{}
	service := Service{Access: revokeAccessStub{}, Revoker: revoker, Store: store}
	if err := service.Revoke(context.Background(), auth.Identity{Kind: "session"}, "box-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unexpected unauthorized revoke result: %v", err)
	}
	if store.revoked || revoker.called {
		t.Fatal("unauthorized revoke mutated Box lifecycle")
	}
}

func TestDecodeOptionalTaskMapAcceptsInitialNullResourceUsage(t *testing.T) {
	value, err := decodeOptionalMap(nil)
	if err != nil || value == nil || len(value) != 0 {
		t.Fatalf("initial resource usage was not normalized: %#v %v", value, err)
	}
	value, err = decodeOptionalMap([]byte(`{"duration_ms":12}`))
	if err != nil || value["duration_ms"] != float64(12) {
		t.Fatalf("resource usage JSON was not decoded: %#v %v", value, err)
	}
}

func TestArtifactPointerValidationRejectsForgedMetadata(t *testing.T) {
	valid := map[string]interface{}{
		"artifact_id": "00000000-0000-4000-8000-000000000001", "version_id": "00000000-0000-4000-8000-000000000002",
		"filename": "artifact.zip", "sha256": strings.Repeat("a", 64), "size_bytes": int64(1),
	}
	if !validArtifactPointer(valid) {
		t.Fatal("valid artifact pointer rejected")
	}
	for name, value := range map[string]interface{}{
		"wrong filename": func() map[string]interface{} {
			copy := cloneMap(valid)
			copy["filename"] = "result_manifest.json"
			return copy
		}(),
		"zero size": func() map[string]interface{} { copy := cloneMap(valid); copy["size_bytes"] = int64(0); return copy }(),
		"bad hash": func() map[string]interface{} {
			copy := cloneMap(valid)
			copy["sha256"] = strings.Repeat("g", 64)
			return copy
		}(),
		"fake id": func() map[string]interface{} {
			copy := cloneMap(valid)
			copy["artifact_id"] = "artifact-1"
			return copy
		}(),
	} {
		if validArtifactPointer(value.(map[string]interface{})) {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func cloneMap(value map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}
