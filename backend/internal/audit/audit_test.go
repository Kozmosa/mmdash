package audit

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type accessStub struct {
	permission project.Permission
}

func (*accessStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return auth.Identity{}, nil
}

func (stub *accessStub) Authorize(
	_ context.Context,
	_ auth.Identity,
	_ string,
	permission project.Permission,
) error {
	stub.permission = permission
	return nil
}

type storeStub struct {
	event Event
	page  Page
	err   error
}

func (stub *storeStub) Record(_ context.Context, event Event) (Event, error) {
	stub.event = event
	return event, stub.err
}

func (stub *storeStub) RecordInTransaction(
	_ context.Context,
	_ transaction.Tx,
	event Event,
) (Event, error) {
	stub.event = event
	return event, stub.err
}

func (stub *storeStub) List(
	context.Context,
	Filter,
	pagination.Request,
) (Page, error) {
	return stub.page, stub.err
}

func TestIngestDerivesActorAndRedactsMetadata(t *testing.T) {
	access := &accessStub{}
	store := &storeStub{}
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	service := Service{
		Access: access, Clock: clock.Fixed{Time: now}, Store: store,
	}
	ctx := requestctx.WithValues(
		context.Background(), requestctx.Values{RequestID: "request-1"},
	)
	identity := auth.Identity{
		Kind:      "api",
		ProjectID: "00000000-0000-4000-8000-000000000002",
		User: auth.User{
			ID: "00000000-0000-4000-8000-000000000001",
		},
	}
	event, err := service.Ingest(ctx, identity, Input{
		Action: "mcp.tool.called", Category: "mcp",
		Metadata: map[string]interface{}{"token": "must-not-survive"},
		Outcome:  "success", Source: "mcp-gateway",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.ActorID != identity.User.ID ||
		event.ProjectID != identity.ProjectID ||
		event.RequestID != "request-1" {
		t.Fatalf("identity was not derived by Core: %#v", event)
	}
	if event.Metadata["token"] != "[REDACTED]" {
		t.Fatalf("audit metadata was not redacted: %#v", event.Metadata)
	}
	if access.permission != project.PermissionAuditWrite {
		t.Fatalf("unexpected permission: %s", access.permission)
	}
}

func TestIngestRejectsEndUserCredential(t *testing.T) {
	service := Service{
		Access: &accessStub{},
		Clock:  clock.Fixed{Time: time.Now()},
		Store:  &storeStub{},
	}
	_, err := service.Ingest(context.Background(), auth.Identity{
		Kind: "agent", User: auth.User{SystemRole: "admin"},
	}, Input{})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("untrusted caller must not submit audit events: %v", err)
	}
}

func TestProjectAuditSearchRequiresReadPermission(t *testing.T) {
	access := &accessStub{}
	service := Service{
		Access: access,
		Store:  &storeStub{page: Page{Items: []Event{}}},
	}
	_, err := service.List(
		context.Background(),
		auth.Identity{User: auth.User{SystemRole: "member"}},
		Filter{ProjectID: "00000000-0000-4000-8000-000000000002"},
		pagination.Request{Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if access.permission != project.PermissionAuditRead {
		t.Fatalf("unexpected permission: %s", access.permission)
	}
}

func TestRecorderUsesTrustedRequestContext(t *testing.T) {
	store := &storeStub{}
	recorder := Recorder{
		Clock: clock.Fixed{
			Time: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		},
		Store: store,
	}
	ctx := requestctx.WithValues(context.Background(), requestctx.Values{
		ActorID:   "00000000-0000-4000-8000-000000000001",
		ActorKind: "session",
		ProjectID: "00000000-0000-4000-8000-000000000002",
		RequestID: "request-1",
	})
	requestctx.SetActor(
		ctx, "00000000-0000-4000-8000-000000000001", "session",
	)
	requestctx.SetProject(ctx, "00000000-0000-4000-8000-000000000002")
	if err := recorder.Record(ctx, Event{
		Action: "settings.updated", Category: "settings",
		Metadata: map[string]interface{}{}, Outcome: "success", Source: "core",
	}); err != nil {
		t.Fatal(err)
	}
	expected := []string{
		store.event.ActorID, store.event.ActorKind,
		store.event.ProjectID, store.event.RequestID,
	}
	if !reflect.DeepEqual(expected, []string{
		"00000000-0000-4000-8000-000000000001", "session",
		"00000000-0000-4000-8000-000000000002", "request-1",
	}) {
		t.Fatalf("unexpected recorded context: %#v", store.event)
	}
}

func TestRecorderWritesThroughBusinessTransaction(t *testing.T) {
	store := &storeStub{}
	recorder := Recorder{
		Clock: clock.Fixed{
			Time: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		},
		Store: store,
	}
	ctx := requestctx.WithValues(context.Background(), requestctx.Values{
		RequestID: "artifact-request",
	})
	requestctx.SetActor(
		ctx, "00000000-0000-4000-8000-000000000001", "session",
	)
	requestctx.SetProject(ctx, "00000000-0000-4000-8000-000000000002")
	if err := recorder.RecordInTransaction(ctx, nil, Event{
		Action: "artifact.upload.confirmed", Category: "artifact",
		Metadata: map[string]interface{}{"version_id": "version-1"},
		Outcome:  "success", Source: "core",
	}); err != nil {
		t.Fatal(err)
	}
	if store.event.RequestID != "artifact-request" ||
		store.event.ActorKind != "session" ||
		store.event.ProjectID != "00000000-0000-4000-8000-000000000002" {
		t.Fatalf("unexpected transactional audit context: %#v", store.event)
	}
}
