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
	box       Box
	revoked   bool
	forceUsed bool
	active    int
}

func (store *revokeStoreStub) Get(context.Context, string) (Box, error) {
	return store.box, nil
}

func (store *revokeStoreStub) BeginDrain(_ context.Context, _ string, now time.Time) (Box, int, error) {
	store.box.Status = StatusDraining
	store.box.DrainRequestedAt = &now
	return store.box, store.active, nil
}

func (store *revokeStoreStub) FinalizeDrained(_ context.Context, _ string, now time.Time) (Box, bool, error) {
	if store.active > 0 {
		return Box{}, false, nil
	}
	store.revoked = true
	store.box.Status = StatusRevoked
	store.box.RevokedAt = &now
	return store.box, true, nil
}

func (store *revokeStoreStub) ForceRevoke(_ context.Context, _ string, now time.Time) (Box, []Task, error) {
	store.revoked, store.forceUsed = true, true
	store.box.Status = StatusRevoked
	store.box.RevokedAt = &now
	return store.box, []Task{{ID: "task-1", Status: TaskFailed}}, nil
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
	called      bool
	ownerUserID string
	tokenID     string
}

func (stub *tokenRevokerStub) RevokeBoxToken(_ context.Context, tokenID, ownerUserID string) error {
	stub.called, stub.ownerUserID, stub.tokenID = true, ownerUserID, tokenID
	return nil
}

func validRegistrationBox() Box {
	return Box{
		Name: "box", Version: "1", InstallationID: "install-1",
		Capabilities: []Capability{{Name: "sandbox", Version: "1"}},
		Runtimes:     []Runtime{{Name: "local-docker", Version: "1"}},
		Limits: ResourceLimits{
			CPUMillis: 500, MemoryBytes: 1 << 20, TimeoutSecond: 30,
			DiskBytes: 1 << 20, PIDs: 32, Network: "disabled",
		},
	}
}

func TestValidateRegistrationRejectsUnsupportedRuntimeAndUnsafeLimits(t *testing.T) {
	base := validRegistrationBox()
	if err := validateRegistration(&base); err != nil {
		t.Fatalf("valid Box rejected: %v", err)
	}
	base.Runtimes[0].Name = "arbitrary-shell"
	if err := validateRegistration(&base); err == nil {
		t.Fatal("unsupported runtime accepted")
	}
	base = validRegistrationBox()
	base.Limits.Network = "public"
	if err := validateRegistration(&base); err == nil {
		t.Fatal("unsupported network policy accepted")
	}
}

func TestValidateHeartbeatPreservesRegisteredIdentity(t *testing.T) {
	registered := Box{Name: "box", InstallationID: "install-1"}
	update := validRegistrationBox()
	update.Name = "forged"
	update.InstallationID = "forged"
	update.Version = "2"
	update.Runtimes = []Runtime{{Name: "e2b", Version: "1"}}
	if err := validateHeartbeat(&update, registered); err != nil {
		t.Fatalf("valid heartbeat rejected: %v", err)
	}
	if update.Name != registered.Name || update.InstallationID != registered.InstallationID {
		t.Fatalf("registered identity was not preserved: %#v", update)
	}
}

func TestDrainRevokeFinalizesNodeThenAccountCredential(t *testing.T) {
	store := &revokeStoreStub{box: Box{
		ID: "box-1", OwnerUserID: "owner-1", Status: StatusOnline, TokenID: "token-1",
	}}
	revoker := &tokenRevokerStub{}
	service := Service{Revoker: revoker, Store: store}
	result, err := service.Revoke(
		context.Background(),
		auth.Identity{Kind: "session", User: auth.User{ID: "owner-1"}},
		"box-1", "drain",
	)
	if err != nil {
		t.Fatalf("revoke Box: %v", err)
	}
	if result.Box.Status != StatusRevoked || !store.revoked || !revoker.called ||
		revoker.ownerUserID != "owner-1" || revoker.tokenID != "token-1" {
		t.Fatalf("incomplete revoke lifecycle: result=%#v store=%#v revoker=%#v", result, store, revoker)
	}
}

func TestRevokeRequiresBoxOwnership(t *testing.T) {
	store := &revokeStoreStub{box: Box{
		ID: "box-1", OwnerUserID: "owner-1", Status: StatusOnline, TokenID: "token-1",
	}}
	revoker := &tokenRevokerStub{}
	service := Service{Revoker: revoker, Store: store}
	_, err := service.Revoke(
		context.Background(),
		auth.Identity{Kind: "session", User: auth.User{ID: "other-user"}},
		"box-1", "force",
	)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("unexpected unauthorized revoke result: %v", err)
	}
	if store.revoked || revoker.called {
		t.Fatal("unauthorized revoke mutated Box lifecycle")
	}
}

func TestArtifactPointerValidationRejectsForgedMetadata(t *testing.T) {
	valid := ArtifactPointer{
		ArtifactID: "00000000-0000-4000-8000-000000000001",
		VersionID:  "00000000-0000-4000-8000-000000000002",
		Filename:   "execution-bundle.zip",
		SHA256:     strings.Repeat("a", 64),
		SizeBytes:  1,
	}
	if !validArtifactPointer(valid) {
		t.Fatal("valid artifact pointer rejected")
	}
	cases := map[string]ArtifactPointer{
		"wrong filename": func() ArtifactPointer { copy := valid; copy.Filename = "artifact.zip"; return copy }(),
		"zero size":      func() ArtifactPointer { copy := valid; copy.SizeBytes = 0; return copy }(),
		"bad hash":       func() ArtifactPointer { copy := valid; copy.SHA256 = strings.Repeat("g", 64); return copy }(),
		"fake id":        func() ArtifactPointer { copy := valid; copy.ArtifactID = "artifact-1"; return copy }(),
	}
	for name, value := range cases {
		if validArtifactPointer(value) {
			t.Fatalf("%s was accepted", name)
		}
	}
}
