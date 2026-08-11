package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/artifact"
	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

var agentServiceTestNow = time.Date(2026, time.August, 6, 8, 30, 0, 0, time.UTC)

type agentServiceTestProjects struct {
	authorizeErr error
	item         project.Project
	permissions  []project.Permission
}

type agentServiceTestArtifacts struct {
	attachments []artifact.ChatAttachment
	inputs      []artifact.ChatAttachment
}

func (items *agentServiceTestArtifacts) AttachAgentRunInputs(
	_ context.Context,
	_ auth.Identity,
	_ string,
	_ string,
	_ []string,
) ([]artifact.ChatAttachment, error) {
	return append([]artifact.ChatAttachment(nil), items.inputs...), nil
}

func (items *agentServiceTestArtifacts) ListAgentRunAttachments(
	_ context.Context,
	_ auth.Identity,
	_ string,
	_ []string,
) ([]artifact.ChatAttachment, error) {
	return append([]artifact.ChatAttachment(nil), items.attachments...), nil
}

func (projects *agentServiceTestProjects) Authorize(
	_ context.Context,
	_ auth.Identity,
	_ string,
	permission project.Permission,
) error {
	projects.permissions = append(projects.permissions, permission)
	return projects.authorizeErr
}

func (projects *agentServiceTestProjects) Get(
	_ context.Context,
	_ auth.Identity,
	projectID string,
) (project.Project, error) {
	if projects.item.ID != projectID {
		return project.Project{}, project.ErrNotFound
	}
	return projects.item, nil
}

type agentServiceTestStore struct {
	instances         map[string]Instance
	sessions          map[string]SessionRecord
	runs              map[string]RunRecord
	rotations         map[string]TokenRotation
	approvalStates    map[string]map[string]string
	approvalClaims    map[string]map[string]string
	approvalUpdated   map[string]map[string]time.Time
	approvalOrder     map[string]map[string]int64
	nextApprovalOrder int64
	promptOverrides   map[string]string
	createdEvents     []string
	updatedEvents     []string
	savedChecks       int
	instanceUpdates   int
}

func newAgentServiceTestStore() *agentServiceTestStore {
	return &agentServiceTestStore{
		instances:       map[string]Instance{},
		sessions:        map[string]SessionRecord{},
		runs:            map[string]RunRecord{},
		rotations:       map[string]TokenRotation{},
		approvalStates:  map[string]map[string]string{},
		approvalClaims:  map[string]map[string]string{},
		approvalUpdated: map[string]map[string]time.Time{},
		approvalOrder:   map[string]map[string]int64{},
		promptOverrides: map[string]string{},
	}
}

func (store *agentServiceTestStore) CreateInstance(
	_ context.Context,
	_ string,
	_ string,
	instance Instance,
	grant ProjectGrant,
) (Instance, error) {
	instance.Grant = cloneAgentServiceTestGrant(&grant)
	store.instances[instance.ID] = cloneAgentServiceTestInstance(instance)
	return cloneAgentServiceTestInstance(instance), nil
}

func (store *agentServiceTestStore) DisableInstance(
	_ context.Context,
	_ string,
	_ string,
	projectID string,
	instanceID string,
	now time.Time,
) error {
	item, err := store.GetInstance(context.Background(), projectID, instanceID)
	if err != nil {
		return err
	}
	item.Status, item.DisabledAt, item.UpdatedAt = InstanceDisabled, &now, now
	store.instances[instanceID] = cloneAgentServiceTestInstance(item)
	return nil
}

func (store *agentServiceTestStore) GetGrant(
	_ context.Context,
	projectID string,
	instanceID string,
) (ProjectGrant, error) {
	item, err := store.GetInstance(context.Background(), projectID, instanceID)
	if err != nil || item.Grant == nil {
		return ProjectGrant{}, ErrNotFound
	}
	return *cloneAgentServiceTestGrant(item.Grant), nil
}

func (store *agentServiceTestStore) GetInstance(
	_ context.Context,
	projectID string,
	instanceID string,
) (Instance, error) {
	item, ok := store.instances[instanceID]
	if !ok || item.Grant == nil || item.Grant.ProjectID != projectID {
		return Instance{}, ErrNotFound
	}
	return cloneAgentServiceTestInstance(item), nil
}

func (store *agentServiceTestStore) ListInstances(
	_ context.Context,
	projectID string,
) ([]Instance, error) {
	items := []Instance{}
	for _, item := range store.instances {
		if item.Grant != nil && item.Grant.ProjectID == projectID {
			items = append(items, cloneAgentServiceTestInstance(item))
		}
	}
	sort.Slice(items, func(left, right int) bool { return items[left].ID < items[right].ID })
	return items, nil
}

func (store *agentServiceTestStore) SaveChecks(
	_ context.Context,
	instanceID string,
	runtime CheckSnapshot,
	management CheckSnapshot,
	projectAccess CheckSnapshot,
	managementPath string,
	capabilities map[string]interface{},
	status string,
	now time.Time,
) error {
	item, ok := store.instances[instanceID]
	if !ok {
		return ErrNotFound
	}
	item.RuntimeCheck = runtime
	item.ManagementCheck = management
	item.ProjectAccessCheck = projectAccess
	item.ManagementPath = managementPath
	item.Capabilities = capabilities
	item.Status = status
	item.UpdatedAt = now
	item.Version++
	store.instances[instanceID] = cloneAgentServiceTestInstance(item)
	store.savedChecks++
	return nil
}

func (store *agentServiceTestStore) SetProjectAccess(
	_ context.Context,
	grantID string,
	remoteID string,
	now time.Time,
) error {
	for instanceID, item := range store.instances {
		if item.Grant != nil && item.Grant.GrantID == grantID {
			item.Grant.RemoteAccessID = remoteID
			item.Grant.UpdatedAt = now
			item.Grant.Version++
			store.instances[instanceID] = cloneAgentServiceTestInstance(item)
			return nil
		}
	}
	return ErrNotFound
}

func (store *agentServiceTestStore) UpdateInstance(
	_ context.Context,
	_ string,
	_ string,
	item Instance,
	now time.Time,
) (Instance, error) {
	if _, ok := store.instances[item.ID]; !ok {
		return Instance{}, ErrNotFound
	}
	item.UpdatedAt = now
	item.Version++
	store.instances[item.ID] = cloneAgentServiceTestInstance(item)
	store.instanceUpdates++
	return cloneAgentServiceTestInstance(item), nil
}

func (store *agentServiceTestStore) GetPromptOverride(
	_ context.Context,
	grantID string,
) (string, time.Time, int64, error) {
	return store.promptOverrides[grantID], time.Time{}, 1, nil
}

func (store *agentServiceTestStore) ResetPrompt(
	_ context.Context,
	_ string,
	grantID string,
	_ time.Time,
) error {
	delete(store.promptOverrides, grantID)
	return nil
}

func (store *agentServiceTestStore) UpdatePrompt(
	_ context.Context,
	_ string,
	grantID string,
	override string,
	_ time.Time,
) error {
	store.promptOverrides[grantID] = override
	return nil
}

func (store *agentServiceTestStore) CreateSession(
	_ context.Context,
	_ string,
	item SessionRecord,
	event string,
) (SessionRecord, error) {
	if _, exists := store.sessions[item.ID]; exists {
		return SessionRecord{}, ErrConflict
	}
	store.sessions[item.ID] = item
	store.createdEvents = append(store.createdEvents, event)
	return item, nil
}

func (store *agentServiceTestStore) GetSession(
	_ context.Context,
	projectID string,
	instanceID string,
	sessionID string,
) (SessionRecord, error) {
	item, ok := store.sessions[sessionID]
	if !ok || item.ProjectID != projectID || item.AgentInstanceID != instanceID {
		return SessionRecord{}, ErrNotFound
	}
	return item, nil
}

func (store *agentServiceTestStore) ListSessions(
	_ context.Context,
	projectID string,
	instanceID string,
) ([]SessionRecord, error) {
	items := []SessionRecord{}
	for _, item := range store.sessions {
		if item.ProjectID == projectID && item.AgentInstanceID == instanceID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(left, right int) bool { return items[left].Title < items[right].Title })
	return items, nil
}

func (store *agentServiceTestStore) SetDefaultSession(
	_ context.Context,
	_ string,
	projectID string,
	instanceID string,
	sessionID string,
	now time.Time,
) error {
	if _, err := store.GetSession(context.Background(), projectID, instanceID, sessionID); err != nil {
		return err
	}
	item, err := store.GetInstance(context.Background(), projectID, instanceID)
	if err != nil {
		return err
	}
	item.Grant.DefaultSessionID = sessionID
	item.Grant.UpdatedAt = now
	item.Grant.Version++
	store.instances[instanceID] = cloneAgentServiceTestInstance(item)
	return nil
}

func (store *agentServiceTestStore) UpdateSession(
	_ context.Context,
	_ string,
	item SessionRecord,
	event string,
	now time.Time,
) (SessionRecord, error) {
	if _, ok := store.sessions[item.ID]; !ok {
		return SessionRecord{}, ErrNotFound
	}
	item.UpdatedAt = now
	item.Version++
	store.sessions[item.ID] = item
	store.updatedEvents = append(store.updatedEvents, event)
	return item, nil
}

func (store *agentServiceTestStore) ActivateRun(
	_ context.Context,
	_ string,
	item RunRecord,
	now time.Time,
) (RunRecord, error) {
	if _, ok := store.runs[item.ID]; !ok {
		return RunRecord{}, ErrNotFound
	}
	item.UpdatedAt = now
	item.Version++
	store.runs[item.ID] = item
	return item, nil
}

func (store *agentServiceTestStore) FailRunReservation(
	_ context.Context,
	_ string,
	runID string,
	code string,
	now time.Time,
) error {
	item, ok := store.runs[runID]
	if !ok {
		return ErrNotFound
	}
	item.Status, item.SafeErrorCode, item.UpdatedAt = RunRecordFailed, code, now
	store.runs[runID] = item
	return nil
}

func (store *agentServiceTestStore) GetRun(
	_ context.Context,
	sessionID string,
	runID string,
) (RunRecord, error) {
	item, ok := store.runs[runID]
	if !ok || item.SessionID != sessionID {
		return RunRecord{}, ErrNotFound
	}
	item.PendingApprovalIDs = store.pendingApprovalIDs(runID)
	return item, nil
}

func (store *agentServiceTestStore) ListRuns(
	_ context.Context,
	sessionID string,
) ([]RunRecord, error) {
	items := []RunRecord{}
	for _, item := range store.runs {
		if item.SessionID == sessionID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].CreatedAt.Before(items[right].CreatedAt)
	})
	return items, nil
}

func (store *agentServiceTestStore) RecordRunApproval(
	_ context.Context,
	runID string,
	approvalID string,
	now time.Time,
) (RunRecord, error) {
	item, ok := store.runs[runID]
	if !ok {
		return RunRecord{}, ErrNotFound
	}
	if terminalRunStatus(item.Status) || item.Status == RunRecordStopping {
		return RunRecord{}, ErrConflict
	}
	states := store.approvalStates[runID]
	if states == nil {
		states = map[string]string{}
		store.approvalStates[runID] = states
	}
	if _, exists := states[approvalID]; !exists {
		states[approvalID] = "pending"
		store.nextApprovalOrder++
		orders := store.approvalOrder[runID]
		if orders == nil {
			orders = map[string]int64{}
			store.approvalOrder[runID] = orders
		}
		orders[approvalID] = store.nextApprovalOrder
		updated := store.approvalUpdated[runID]
		if updated == nil {
			updated = map[string]time.Time{}
			store.approvalUpdated[runID] = updated
		}
		updated[approvalID] = now
	}
	if states[approvalID] == "pending" || states[approvalID] == "responding" {
		if item.Status != RunRecordWaitingForApproval {
			item.Status, item.UpdatedAt = RunRecordWaitingForApproval, now
			item.Version++
			store.runs[runID] = item
		}
	}
	return store.GetRun(context.Background(), item.SessionID, runID)
}

func (store *agentServiceTestStore) ClaimRunApproval(
	_ context.Context,
	runID string,
	approvalID string,
	claimID string,
	now time.Time,
) (RunRecord, error) {
	item, ok := store.runs[runID]
	if !ok {
		return RunRecord{}, ErrNotFound
	}
	state := store.approvalStates[runID][approvalID]
	staleClaim := state == "responding" &&
		!store.approvalUpdated[runID][approvalID].After(now.Add(-runApprovalClaimLease))
	pending := store.pendingApprovalIDs(runID)
	if item.Status != RunRecordWaitingForApproval ||
		(state != "pending" && !staleClaim) || len(pending) == 0 ||
		pending[0] != approvalID {
		return RunRecord{}, ErrConflict
	}
	store.approvalStates[runID][approvalID] = "responding"
	claims := store.approvalClaims[runID]
	if claims == nil {
		claims = map[string]string{}
		store.approvalClaims[runID] = claims
	}
	claims[approvalID] = claimID
	store.approvalUpdated[runID][approvalID] = now
	item.UpdatedAt = now
	store.runs[runID] = item
	return store.GetRun(context.Background(), item.SessionID, runID)
}

func (store *agentServiceTestStore) ReleaseRunApprovalClaim(
	_ context.Context,
	runID string,
	approvalID string,
	claimID string,
	now time.Time,
) (RunRecord, error) {
	item, ok := store.runs[runID]
	if !ok {
		return RunRecord{}, ErrNotFound
	}
	if store.approvalStates[runID][approvalID] != "responding" ||
		store.approvalClaims[runID][approvalID] != claimID {
		return RunRecord{}, ErrConflict
	}
	store.approvalStates[runID][approvalID] = "pending"
	delete(store.approvalClaims[runID], approvalID)
	store.approvalUpdated[runID][approvalID] = now
	item.UpdatedAt = now
	store.runs[runID] = item
	return store.GetRun(context.Background(), item.SessionID, runID)
}

func (store *agentServiceTestStore) CompleteRunApproval(
	_ context.Context,
	runID string,
	approvalID string,
	claimID string,
	now time.Time,
) (RunRecord, error) {
	item, ok := store.runs[runID]
	if !ok {
		return RunRecord{}, ErrNotFound
	}
	state := store.approvalStates[runID][approvalID]
	currentClaim := store.approvalClaims[runID][approvalID]
	if state == "resolved" && currentClaim == claimID {
		return store.GetRun(context.Background(), item.SessionID, runID)
	}
	if item.Status != RunRecordWaitingForApproval || state != "responding" ||
		currentClaim != claimID {
		return RunRecord{}, ErrConflict
	}
	store.approvalStates[runID][approvalID] = "resolved"
	store.approvalUpdated[runID][approvalID] = now
	return store.finishApproval(item, now)
}

func (store *agentServiceTestStore) ApplyRunApprovalResponse(
	_ context.Context,
	runID string,
	approvalID string,
	now time.Time,
) (RunRecord, error) {
	item, ok := store.runs[runID]
	if !ok {
		return RunRecord{}, ErrNotFound
	}
	state := store.approvalStates[runID][approvalID]
	if state == "resolved" {
		return store.GetRun(context.Background(), item.SessionID, runID)
	}
	if item.Status != RunRecordWaitingForApproval ||
		(state != "pending" && state != "responding") {
		return RunRecord{}, ErrConflict
	}
	store.approvalStates[runID][approvalID] = "resolved"
	store.approvalUpdated[runID][approvalID] = now
	return store.finishApproval(item, now)
}

func (store *agentServiceTestStore) ApplyNextRunApprovalResponse(
	ctx context.Context,
	runID string,
	now time.Time,
) (RunRecord, string, error) {
	pending := store.pendingApprovalIDs(runID)
	if len(pending) == 0 {
		return RunRecord{}, "", ErrConflict
	}
	approvalID := pending[0]
	item, err := store.ApplyRunApprovalResponse(ctx, runID, approvalID, now)
	return item, approvalID, err
}

func (store *agentServiceTestStore) finishApproval(
	item RunRecord,
	now time.Time,
) (RunRecord, error) {
	item.Status = RunRecordRunning
	if len(store.pendingApprovalIDs(item.ID)) > 0 {
		item.Status = RunRecordWaitingForApproval
	}
	item.UpdatedAt = now
	item.Version++
	store.runs[item.ID] = item
	return store.GetRun(context.Background(), item.SessionID, item.ID)
}

func (store *agentServiceTestStore) pendingApprovalIDs(runID string) []string {
	values := []string{}
	for approvalID, status := range store.approvalStates[runID] {
		if status == "pending" || status == "responding" {
			values = append(values, approvalID)
		}
	}
	sort.Slice(values, func(left, right int) bool {
		return store.approvalOrder[runID][values[left]] <
			store.approvalOrder[runID][values[right]]
	})
	return values
}

func (store *agentServiceTestStore) ReserveRun(
	_ context.Context,
	item RunRecord,
) (RunRecord, error) {
	if _, exists := store.runs[item.ID]; exists {
		return RunRecord{}, ErrConflict
	}
	store.runs[item.ID] = item
	return item, nil
}

func (store *agentServiceTestStore) UpdateRun(
	_ context.Context,
	runID string,
	status string,
	code string,
	now time.Time,
) (RunRecord, error) {
	item, ok := store.runs[runID]
	if !ok {
		return RunRecord{}, ErrNotFound
	}
	item.Status, item.SafeErrorCode, item.UpdatedAt = status, code, now
	if status == RunRecordRunning && len(store.pendingApprovalIDs(runID)) > 0 {
		item.Status = RunRecordWaitingForApproval
	}
	item.Version++
	if terminalRunStatus(status) {
		item.CompletedAt = &now
		for approvalID, approvalStatus := range store.approvalStates[runID] {
			if approvalStatus == "pending" || approvalStatus == "responding" {
				store.approvalStates[runID][approvalID] = "expired"
				delete(store.approvalClaims[runID], approvalID)
				store.approvalUpdated[runID][approvalID] = now
			}
		}
	}
	store.runs[runID] = item
	return item, nil
}

func (store *agentServiceTestStore) UpsertToolCall(
	_ context.Context,
	call ToolCallRecord,
) (ToolCallRecord, error) {
	item, ok := store.runs[call.RunID]
	if !ok {
		return ToolCallRecord{}, ErrNotFound
	}
	item.ToolCalls = append(item.ToolCalls, call)
	store.runs[call.RunID] = item
	return call, nil
}

func (*agentServiceTestStore) ValidateProvenance(
	context.Context,
	string,
	string,
	string,
	string,
) error {
	return nil
}

func (store *agentServiceTestStore) CreateRotation(
	_ context.Context,
	_ string,
	rotation TokenRotation,
) (TokenRotation, error) {
	store.rotations[rotation.NewTokenID] = rotation
	return rotation, nil
}

func (store *agentServiceTestStore) FindRotationByToken(
	_ context.Context,
	grantID string,
	tokenID string,
) (TokenRotation, error) {
	rotation, ok := store.rotations[tokenID]
	if !ok || rotation.GrantID != grantID {
		return TokenRotation{}, ErrNotFound
	}
	return rotation, nil
}

func (store *agentServiceTestStore) UpdateRotation(
	_ context.Context,
	rotationID string,
	status string,
	code string,
	now time.Time,
) (TokenRotation, error) {
	for tokenID, rotation := range store.rotations {
		if rotation.RotationID == rotationID {
			rotation.Status, rotation.SafeErrorCode, rotation.UpdatedAt = status, code, now
			if status == "completed" {
				rotation.CompletedAt = &now
			}
			store.rotations[tokenID] = rotation
			return rotation, nil
		}
	}
	return TokenRotation{}, ErrNotFound
}

type agentServiceTestAdapter struct {
	createSessionRequests []CreateSessionRequest
	updateSessionRequests []UpdateSessionRequest
	forkSessionRequests   []ForkSessionRequest
	startRunRequests      []StartRunRequest
	messages              []Message
	probe                 ProbeResult
	probeCalls            int
	checkRuntimeCalls     int
	checkRuntimeErr       error
	verifyAccess          ProjectAccessResult
	getRunResult          Run
	stopRunResult         Run
	approvalResult        ApprovalResult
	approvalRequest       ApprovalRequest
	approvalErr           error
	approvalCalls         int
	approvalMu            sync.Mutex
	onApprove             func(ApprovalRequest)
	configureAccessResult ProjectAccessResult
	onConfigureAccess     func(ProjectAccessRequest)
	rotateAccessResult    ProjectAccessResult
	onRotateAccess        func(ProjectAccessRequest)
	finalizeAccessResult  ProjectAccessFinalizeResult
	finalizeAccessErr     error
	onFinalizeAccess      func(ProjectAccessFinalizeRequest)
	configureAccessCalls  int
	getRunCalls           int
	stopRunCalls          int
	streamRunCalls        int
	streamRunEvents       []Event
	streamRunErr          error
	forkCalls             int
	rotateAccessCalls     int
	finalizeAccessCalls   int
	runSequence           int
}

func (adapter *agentServiceTestAdapter) Probe(context.Context) (ProbeResult, error) {
	adapter.probeCalls++
	return adapter.probe, nil
}

func (adapter *agentServiceTestAdapter) CheckRuntime(context.Context) error {
	adapter.checkRuntimeCalls++
	return adapter.checkRuntimeErr
}

func (*agentServiceTestAdapter) ListSessions(context.Context, SessionFilter) (SessionPage, error) {
	return SessionPage{Sessions: []Session{}}, nil
}

func (adapter *agentServiceTestAdapter) CreateSession(
	_ context.Context,
	request CreateSessionRequest,
) (Session, error) {
	adapter.createSessionRequests = append(adapter.createSessionRequests, request)
	return Session{RemoteID: "remote-" + request.RemoteID, Source: request.Source, Title: request.Title}, nil
}

func (*agentServiceTestAdapter) GetSession(context.Context, string) (Session, error) {
	return Session{}, nil
}

func (adapter *agentServiceTestAdapter) UpdateSession(
	_ context.Context,
	remoteID string,
	request UpdateSessionRequest,
) (Session, error) {
	adapter.updateSessionRequests = append(adapter.updateSessionRequests, request)
	return Session{RemoteID: remoteID}, nil
}

func (*agentServiceTestAdapter) DeleteSession(context.Context, string) error { return nil }

func (adapter *agentServiceTestAdapter) ForkSession(
	_ context.Context,
	_ string,
	request ForkSessionRequest,
) (Session, error) {
	adapter.forkCalls++
	adapter.forkSessionRequests = append(adapter.forkSessionRequests, request)
	return Session{RemoteID: "remote-" + request.RemoteID, Title: request.Title}, nil
}

func (adapter *agentServiceTestAdapter) ListMessages(context.Context, string) ([]Message, error) {
	return append([]Message(nil), adapter.messages...), nil
}

func (*agentServiceTestAdapter) Chat(context.Context, string, ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, ErrUnsupported
}

func (*agentServiceTestAdapter) StreamChat(
	context.Context,
	string,
	ChatRequest,
	StreamOptions,
	EventHandler,
) error {
	return ErrUnsupported
}

func (adapter *agentServiceTestAdapter) StartRun(
	_ context.Context,
	request StartRunRequest,
) (Run, error) {
	adapter.runSequence++
	adapter.startRunRequests = append(adapter.startRunRequests, request)
	return Run{
		RemoteID:        "remote-run-" + request.SessionRemoteID + "-" + string(rune('0'+adapter.runSequence)),
		SessionRemoteID: request.SessionRemoteID,
		Status:          RunRunning,
	}, nil
}

func (adapter *agentServiceTestAdapter) GetRun(context.Context, string) (Run, error) {
	adapter.getRunCalls++
	return adapter.getRunResult, nil
}

func (adapter *agentServiceTestAdapter) StreamRun(
	ctx context.Context,
	_ string,
	_ StreamOptions,
	handler EventHandler,
) error {
	adapter.streamRunCalls++
	if adapter.streamRunErr != nil {
		return adapter.streamRunErr
	}
	events := adapter.streamRunEvents
	if len(events) == 0 {
		events = []Event{{Type: EventHeartbeat}}
	}
	for _, event := range events {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (adapter *agentServiceTestAdapter) ApproveRun(
	_ context.Context,
	_ string,
	request ApprovalRequest,
) (ApprovalResult, error) {
	adapter.approvalMu.Lock()
	adapter.approvalCalls++
	adapter.approvalRequest = request
	callback := adapter.onApprove
	result, err := adapter.approvalResult, adapter.approvalErr
	adapter.approvalMu.Unlock()
	if callback != nil {
		callback(request)
	}
	return result, err
}

func (adapter *agentServiceTestAdapter) StopRun(context.Context, string) (Run, error) {
	adapter.stopRunCalls++
	return adapter.stopRunResult, nil
}

func (*agentServiceTestAdapter) ListJobs(context.Context, bool) ([]Job, error) {
	return nil, ErrUnsupported
}

func (*agentServiceTestAdapter) CreateJob(context.Context, CreateJobRequest) (Job, error) {
	return Job{}, ErrUnsupported
}

func (*agentServiceTestAdapter) GetJob(context.Context, string) (Job, error) {
	return Job{}, ErrUnsupported
}

func (*agentServiceTestAdapter) UpdateJob(context.Context, string, UpdateJobRequest) (Job, error) {
	return Job{}, ErrUnsupported
}

func (*agentServiceTestAdapter) DeleteJob(context.Context, string) error { return ErrUnsupported }
func (*agentServiceTestAdapter) PauseJob(context.Context, string) (Job, error) {
	return Job{}, ErrUnsupported
}

func (*agentServiceTestAdapter) ResumeJob(context.Context, string) (Job, error) {
	return Job{}, ErrUnsupported
}

func (*agentServiceTestAdapter) RunJob(context.Context, string) (Job, error) {
	return Job{}, ErrUnsupported
}

func (adapter *agentServiceTestAdapter) VerifyProjectAccess(
	context.Context,
	ProjectAccessRequest,
) (ProjectAccessResult, error) {
	return adapter.verifyAccess, nil
}

func (adapter *agentServiceTestAdapter) ConfigureProjectAccess(
	_ context.Context,
	request ProjectAccessRequest,
) (ProjectAccessResult, error) {
	adapter.configureAccessCalls++
	if adapter.onConfigureAccess != nil {
		adapter.onConfigureAccess(request)
	}
	return adapter.configureAccessResult, nil
}

func (adapter *agentServiceTestAdapter) RotateProjectAccess(
	_ context.Context,
	request ProjectAccessRequest,
) (ProjectAccessResult, error) {
	adapter.rotateAccessCalls++
	if adapter.onRotateAccess != nil {
		adapter.onRotateAccess(request)
	}
	return adapter.rotateAccessResult, nil
}

func (adapter *agentServiceTestAdapter) FinalizeProjectAccess(
	_ context.Context,
	request ProjectAccessFinalizeRequest,
) (ProjectAccessFinalizeResult, error) {
	adapter.finalizeAccessCalls++
	if adapter.onFinalizeAccess != nil {
		adapter.onFinalizeAccess(request)
	}
	return adapter.finalizeAccessResult, adapter.finalizeAccessErr
}

type agentServiceTestSettingsAccess struct{}

func (agentServiceTestSettingsAccess) Authorize(
	context.Context,
	auth.Identity,
	settings.Scope,
	string,
	bool,
) error {
	return nil
}

type agentServiceTestSettingsStore struct {
	settings.Store
	item        settings.StoredSetting
	upsertCalls int
}

func (store *agentServiceTestSettingsStore) GetResource(
	_ context.Context,
	scope settings.Scope,
	scopeID string,
	typeKey string,
	resourceID string,
) (settings.StoredSetting, error) {
	if store.item.Scope != scope || store.item.ScopeID != scopeID ||
		store.item.TypeKey != typeKey || store.item.ResourceID != resourceID {
		return settings.StoredSetting{}, settings.ErrNotFound
	}
	return store.item, nil
}

func (*agentServiceTestSettingsStore) DeleteResource(
	context.Context,
	settings.Scope,
	string,
	string,
	string,
	string,
) error {
	return nil
}

func (store *agentServiceTestSettingsStore) UpsertResource(
	_ context.Context,
	_ string,
	item settings.StoredSetting,
) (settings.StoredSetting, error) {
	store.item = item
	store.upsertCalls++
	return item, nil
}

type agentServiceTestAuthStore struct {
	auth.Store
	auth.AgentTokenStore
	tokens             map[string]auth.AgentToken
	activations        int
	activateErr        error
	lastCreatedTokenID string
	onActivate         func(string, string, string, time.Time)
}

func (store *agentServiceTestAuthStore) GetAgentToken(
	_ context.Context,
	tokenID string,
) (auth.AgentToken, error) {
	token, ok := store.tokens[tokenID]
	if !ok {
		return auth.AgentToken{}, auth.ErrNotFound
	}
	return token, nil
}

func (store *agentServiceTestAuthStore) CreateAgentToken(
	_ context.Context,
	token auth.AgentToken,
) error {
	if _, exists := store.tokens[token.ID]; exists {
		return auth.ErrConflict
	}
	store.tokens[token.ID] = token
	store.lastCreatedTokenID = token.ID
	return nil
}

func (store *agentServiceTestAuthStore) ListAgentTokens(
	_ context.Context,
	grantID string,
) ([]auth.AgentToken, error) {
	items := []auth.AgentToken{}
	for _, token := range store.tokens {
		if token.GrantID == grantID {
			items = append(items, token)
		}
	}
	return items, nil
}

func (store *agentServiceTestAuthStore) ActivateAgentToken(
	_ context.Context,
	tokenID string,
	oldTokenID string,
	newRemoteAccessID string,
	now time.Time,
) (auth.AgentToken, error) {
	if store.activateErr != nil {
		return auth.AgentToken{}, store.activateErr
	}
	token, ok := store.tokens[tokenID]
	if !ok || token.Status != "pending" || token.Verification == nil {
		return auth.AgentToken{}, auth.ErrConflict
	}
	if oldTokenID != "" {
		old, exists := store.tokens[oldTokenID]
		if !exists || old.Status != "active" {
			return auth.AgentToken{}, auth.ErrConflict
		}
		old.Status, old.RevokedAt = "revoked", &now
		store.tokens[oldTokenID] = old
	}
	token.Status, token.ActivatedAt = "active", &now
	store.tokens[tokenID] = token
	store.activations++
	if store.onActivate != nil {
		store.onActivate(tokenID, oldTokenID, newRemoteAccessID, now)
	}
	return token, nil
}

type agentServiceTestTokenAuthorizer struct{}

func (agentServiceTestTokenAuthorizer) AuthorizeTokenManagement(
	context.Context,
	auth.Identity,
	string,
) error {
	return nil
}

type agentServiceTestAuditStore struct {
	events []audit.Event
}

func (*agentServiceTestAuditStore) List(
	context.Context,
	audit.Filter,
	pagination.Request,
) (audit.Page, error) {
	return audit.Page{Items: []audit.Event{}}, nil
}

func (store *agentServiceTestAuditStore) Record(
	_ context.Context,
	event audit.Event,
) (audit.Event, error) {
	store.events = append(store.events, event)
	return event, nil
}

type agentServiceFixture struct {
	service       Service
	store         *agentServiceTestStore
	settingsStore *agentServiceTestSettingsStore
	adapter       *agentServiceTestAdapter
	authStore     *agentServiceTestAuthStore
	audit         *agentServiceTestAuditStore
	caller        auth.Identity
}

func newAgentServiceFixture(t *testing.T) *agentServiceFixture {
	t.Helper()
	grant := &ProjectGrant{
		AgentInstanceID: "agent-1",
		AllowedTools:    append([]string(nil), DefaultAllowedTools...),
		CreatedAt:       agentServiceTestNow,
		CreatedBy:       "user-1",
		GrantID:         "grant-1",
		ProjectID:       "project-1",
		Role:            string(project.RoleAgent),
		Status:          "active",
		UpdatedAt:       agentServiceTestNow,
		Version:         1,
	}
	instance := Instance{
		AdapterType:           AdapterHermes,
		Capabilities:          map[string]interface{}{"sessions": true, "runs": true},
		CreatedAt:             agentServiceTestNow,
		CreatedBy:             "user-1",
		DisplayName:           "Test Hermes",
		ID:                    "agent-1",
		ManagementMode:        ManagementManual,
		ManagementPath:        "unreachable",
		Profile:               "default",
		RuntimeCheck:          CheckSnapshot{CheckedAt: agentServiceTestNow, Status: "passed"},
		ManagementCheck:       CheckSnapshot{CheckedAt: agentServiceTestNow, Status: "unsupported"},
		ProjectAccessCheck:    CheckSnapshot{CheckedAt: agentServiceTestNow, Status: "passed"},
		RequestTimeoutSeconds: 30,
		RuntimeURL:            "https://runtime.example.test",
		Status:                InstanceActive,
		UpdatedAt:             agentServiceTestNow,
		Version:               1,
		Grant:                 grant,
	}
	store := newAgentServiceTestStore()
	store.instances[instance.ID] = cloneAgentServiceTestInstance(instance)
	adapter := &agentServiceTestAdapter{
		probe: ProbeResult{
			Healthy: true, Authenticated: true,
			Capabilities: RuntimeCapabilities{
				Sessions: true, Runs: true, RunStreaming: true, RunStop: true,
			},
		},
		approvalResult: ApprovalResult{Resolved: 1},
	}
	adapters := NewRegistry()
	if err := adapters.Register(Descriptor{Key: AdapterHermes, DisplayName: "Hermes"},
		func(context.Context, AdapterConfig) (Adapter, error) { return adapter, nil }); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	settingsRegistry := settings.NewRegistry()
	if err := settingsRegistry.Register(SettingDefinition()); err != nil {
		t.Fatalf("register settings: %v", err)
	}
	codec, err := settings.NewSecretCodec("agent-service-test-encryption-key-material")
	if err != nil {
		t.Fatalf("settings codec: %v", err)
	}
	encryptedAPIKey, err := codec.Encrypt("top-secret-hermes-key")
	if err != nil {
		t.Fatalf("encrypt fixture API key: %v", err)
	}
	settingsStore := &agentServiceTestSettingsStore{item: settings.StoredSetting{
		EncryptedSecrets: map[string]settings.EncryptedSecret{settingAPIKey: encryptedAPIKey},
		PublicValues: map[string]interface{}{
			settingProfile:        "default",
			settingRequestTimeout: float64(30),
		},
		ResourceID: instance.ID,
		Scope:      settings.ScopeProject,
		ScopeID:    grant.ProjectID,
		TypeKey:    SettingTypeAgentHermes,
		Version:    1,
	}}
	settingsService := &settings.Service{
		Access: agentServiceTestSettingsAccess{}, Clock: clock.Fixed{Time: agentServiceTestNow},
		Codec: codec, Registry: settingsRegistry, Store: settingsStore,
	}
	authStore := &agentServiceTestAuthStore{tokens: map[string]auth.AgentToken{}}
	authStore.onActivate = func(tokenID string, _ string, newRemoteAccessID string, now time.Time) {
		rotation, ok := store.rotations[tokenID]
		if !ok {
			return
		}
		rotation.Status, rotation.SafeErrorCode = "completed", ""
		rotation.UpdatedAt, rotation.CompletedAt = now, &now
		store.rotations[tokenID] = rotation
		if newRemoteAccessID != "" {
			_ = store.SetProjectAccess(context.Background(), rotation.GrantID,
				newRemoteAccessID, now)
		}
	}
	authService := &auth.Service{
		Clock:         clock.Fixed{Time: agentServiceTestNow},
		Generator:     identity.Generator{},
		ProjectTokens: agentServiceTestTokenAuthorizer{},
		Store:         authStore,
	}
	auditStore := &agentServiceTestAuditStore{}
	projects := &agentServiceTestProjects{item: project.Project{
		ID: "project-1", Name: "Traceable model", ProblemTitle: "Optimize",
		ProblemSummary: "Find a robust solution", ProjectConstraints: []string{"Keep evidence"},
	}}
	return &agentServiceFixture{
		adapter: adapter, audit: auditStore, authStore: authStore,
		caller: auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}},
		store:  store, settingsStore: settingsStore,
		service: Service{
			Adapters: adapters,
			Audit: audit.Recorder{
				Clock: clock.Fixed{Time: agentServiceTestNow}, Store: auditStore,
			},
			Auth: authService, Clock: clock.Fixed{Time: agentServiceTestNow},
			GatewayURL: "https://gateway.example.test/mcp",
			Generator:  identity.Generator{}, Projects: projects,
			Settings: settingsService, Store: store,
		},
	}
}

func cloneAgentServiceTestGrant(grant *ProjectGrant) *ProjectGrant {
	if grant == nil {
		return nil
	}
	cloned := *grant
	cloned.AllowedTools = append([]string(nil), grant.AllowedTools...)
	return &cloned
}

func cloneAgentServiceTestInstance(item Instance) Instance {
	item.Grant = cloneAgentServiceTestGrant(item.Grant)
	capabilities := map[string]interface{}{}
	for key, value := range item.Capabilities {
		capabilities[key] = value
	}
	item.Capabilities = capabilities
	return item
}

func (fixture *agentServiceFixture) seedSession(status string) SessionRecord {
	item := SessionRecord{
		AgentInstanceID: "agent-1", CreatedAt: agentServiceTestNow,
		CreatedBy: "user-1", GrantID: "grant-1", ID: "session-1",
		ProjectID: "project-1", RemoteSessionID: "remote-session-1",
		SessionType: SessionMain, Status: status, Title: "Main session",
		UpdatedAt: agentServiceTestNow, Version: 1,
	}
	fixture.store.sessions[item.ID] = item
	return item
}

func (fixture *agentServiceFixture) seedRun(status string) RunRecord {
	item := RunRecord{
		CreatedAt: agentServiceTestNow, CreatedBy: "user-1", ID: "run-1",
		RemoteRunID: "remote-run-1", SessionID: "session-1", Source: "message",
		Status: status, ToolCalls: []ToolCallRecord{}, UpdatedAt: agentServiceTestNow,
		Version: 1,
	}
	fixture.store.runs[item.ID] = item
	if status == RunRecordWaitingForApproval {
		fixture.store.approvalStates[item.ID] = map[string]string{"approval-1": "pending"}
		fixture.store.approvalClaims[item.ID] = map[string]string{}
		fixture.store.approvalUpdated[item.ID] = map[string]time.Time{
			"approval-1": agentServiceTestNow,
		}
		fixture.store.nextApprovalOrder++
		fixture.store.approvalOrder[item.ID] = map[string]int64{
			"approval-1": fixture.store.nextApprovalOrder,
		}
		item.PendingApprovalIDs = []string{"approval-1"}
	}
	return item
}

func trustedAgentServiceTestEvidence(token auth.AgentToken) *auth.AgentTokenVerificationEvidence {
	return &auth.AgentTokenVerificationEvidence{
		AgentInstanceID: token.AgentInstanceID,
		EvidenceID:      "evidence-1",
		MCPSessionID:    "mcp-session-1",
		ProjectID:       token.ProjectID,
		RequestID:       "gateway-request-1",
		TokenID:         token.ID,
		MCPMethod:       auth.AgentTokenVerificationMethod,
		VerifiedAt:      token.CreatedAt.Add(time.Second),
	}
}

func TestEvaluateProgressUsesDedicatedEvaluationProvenance(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.adapter.getRunResult = Run{
		RemoteID: "remote-progress-run", Status: RunCompleted, Output: `{"stage":"planning"}`,
	}
	evaluationID := "00000000-0000-4000-8000-000000000099"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := fixture.service.EvaluateProgress(
		ctx, "project-1", "agent-1", evaluationID, map[string]interface{}{"project_id": "project-1"},
	)
	if err != nil {
		t.Fatalf("evaluate progress: %v", err)
	}
	run, ok := fixture.store.runs[result.AgentRunID]
	if !ok {
		t.Fatalf("progress run %q was not persisted", result.AgentRunID)
	}
	if run.Source != "progress_evaluation" || run.SourceEvaluationID != evaluationID || run.SourceRunID != "" {
		t.Fatalf("progress provenance overloaded parent Run reference: %#v", run)
	}
	session, ok := fixture.store.sessions[run.SessionID]
	if !ok || session.SessionType != SessionProgress {
		t.Fatalf("progress evaluation used a non-Progress Session: %#v", session)
	}
	if len(fixture.adapter.startRunRequests) != 1 || !strings.Contains(fixture.adapter.startRunRequests[0].Instructions, "Progress-type Session") || !strings.Contains(fixture.adapter.startRunRequests[0].Instructions, "task.complete") {
		t.Fatalf("progress evaluation instructions lost the human review boundary: %#v", fixture.adapter.startRunRequests)
	}
}

func (fixture *agentServiceFixture) trustLastCreatedToken(t *testing.T) auth.AgentToken {
	t.Helper()
	tokenID := fixture.authStore.lastCreatedTokenID
	token, ok := fixture.authStore.tokens[tokenID]
	if tokenID == "" || !ok {
		t.Fatal("runtime management callback ran before a pending token was created")
	}
	token.Verification = trustedAgentServiceTestEvidence(token)
	fixture.authStore.tokens[tokenID] = token
	return token
}

func (fixture *agentServiceFixture) enableAutoManagement(t *testing.T, remoteAccessID string) {
	t.Helper()
	item := fixture.store.instances["agent-1"]
	item.ManagementMode = ManagementAuto
	item.DashboardURL = "https://dashboard.example.test"
	item.Grant.RemoteAccessID = remoteAccessID
	fixture.store.instances[item.ID] = cloneAgentServiceTestInstance(item)
	fixture.setEncryptedSetting(t, settingDashboardToken, "dashboard-session-token")
}

func (fixture *agentServiceFixture) setEncryptedSetting(t *testing.T, key, value string) {
	t.Helper()
	encrypted, err := fixture.service.Settings.Codec.Encrypt(value)
	if err != nil {
		t.Fatalf("encrypt %s: %v", key, err)
	}
	fixture.settingsStore.item.EncryptedSecrets[key] = encrypted
}

func TestServiceSessionLifecycle(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	created := map[string]SessionRecord{}
	for _, reservedType := range []string{SessionProgress, SessionExperiment} {
		if _, err := fixture.service.CreateSession(
			context.Background(), fixture.caller, "project-1", "agent-1",
			CreateSessionInput{SessionType: reservedType, Title: strings.ToUpper(reservedType)},
		); !errors.Is(err, ErrInvalid) {
			t.Fatalf("human created reserved %s session: %v", reservedType, err)
		}
	}
	item, err := fixture.service.CreateSession(
		context.Background(), fixture.caller, "project-1", "agent-1",
		CreateSessionInput{SessionType: SessionMain, Title: "MAIN"},
	)
	if err != nil {
		t.Fatalf("create main session: %v", err)
	}
	created[SessionMain] = item
	if item.Status != SessionActive || item.RemoteSessionID == "" || item.SessionType != SessionMain {
		t.Fatalf("unexpected created main session: %#v", item)
	}
	if len(fixture.adapter.createSessionRequests) != 1 ||
		!strings.Contains(fixture.adapter.createSessionRequests[0].SystemPrompt, "Traceable model") ||
		strings.Contains(fixture.adapter.createSessionRequests[0].SystemPrompt, "top-secret") {
		t.Fatalf("unexpected runtime session requests: %#v", fixture.adapter.createSessionRequests)
	}
	items, err := fixture.service.ListSessions(
		context.Background(), fixture.caller, "project-1", "agent-1",
	)
	if err != nil || len(items) != 1 {
		t.Fatalf("list sessions: items=%#v err=%v", items, err)
	}
	mainSession, err := fixture.service.GetSession(
		context.Background(), fixture.caller, "project-1", "agent-1", created[SessionMain].ID,
	)
	if err != nil || mainSession.ID != created[SessionMain].ID {
		t.Fatalf("get main session: item=%#v err=%v", mainSession, err)
	}
	mainSession, err = fixture.service.RenameSession(
		context.Background(), fixture.caller, "project-1", "agent-1", mainSession.ID, " Renamed main ",
	)
	if err != nil || mainSession.Title != "Renamed main" {
		t.Fatalf("rename main session: item=%#v err=%v", mainSession, err)
	}
	mainSession, err = fixture.service.EndSession(
		context.Background(), fixture.caller, "project-1", "agent-1", mainSession.ID, "",
	)
	if err != nil || mainSession.Status != SessionEnded || mainSession.EndReason != "ended_by_user" || mainSession.EndedAt == nil {
		t.Fatalf("end main session: item=%#v err=%v", mainSession, err)
	}
	mainSession, err = fixture.service.ContinueSession(
		context.Background(), fixture.caller, "project-1", "agent-1", mainSession.ID,
	)
	if err != nil || mainSession.Status != SessionActive || mainSession.EndReason != "" || mainSession.EndedAt != nil {
		t.Fatalf("continue main session: item=%#v err=%v", mainSession, err)
	}
	forked, err := fixture.service.ForkSession(
		context.Background(), fixture.caller, "project-1", "agent-1", mainSession.ID, "",
	)
	if err != nil {
		t.Fatalf("fork main session: %v", err)
	}
	if forked.ParentSessionID != mainSession.ID || forked.Title != "Renamed main (fork)" ||
		forked.Status != SessionActive || forked.RemoteSessionID == "" {
		t.Fatalf("unexpected forked session: %#v", forked)
	}
	endedParent := fixture.store.sessions[mainSession.ID]
	if endedParent.Status != SessionEnded || endedParent.EndReason != "branched" {
		t.Fatalf("fork did not end parent: %#v", endedParent)
	}
	if err := fixture.service.SetDefaultSession(
		context.Background(), fixture.caller, "project-1", "agent-1", forked.ID,
	); err != nil {
		t.Fatalf("set default session: %v", err)
	}
	instance, _ := fixture.store.GetInstance(context.Background(), "project-1", "agent-1")
	if instance.Grant.DefaultSessionID != forked.ID {
		t.Fatalf("default session not switched: %#v", instance.Grant)
	}
}

func TestServiceGetRunReturnsTerminalLocalRecordWithoutRuntimeLookup(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	expected := fixture.seedRun(RunRecordCompleted)
	fixture.adapter.getRunResult = Run{RemoteID: expected.RemoteRunID, Status: RunFailed}

	actual, err := fixture.service.GetRun(
		context.Background(), fixture.caller, "project-1", "agent-1", "session-1", "run-1",
	)
	if err != nil {
		t.Fatalf("get terminal run: %v", err)
	}
	if actual.Status != RunRecordCompleted || fixture.adapter.getRunCalls != 0 {
		t.Fatalf("terminal local state was not authoritative: run=%#v runtime_calls=%d", actual, fixture.adapter.getRunCalls)
	}
}

func TestServiceStopRunFallsBackToStoppingForUnsafeRemoteStates(t *testing.T) {
	for _, remoteStatus := range []RunStatus{RunRunning, RunStatus("unknown-provider-state")} {
		t.Run(string(remoteStatus), func(t *testing.T) {
			fixture := newAgentServiceFixture(t)
			fixture.seedSession(SessionActive)
			fixture.seedRun(RunRecordRunning)
			fixture.adapter.stopRunResult = Run{RemoteID: "remote-run-1", Status: remoteStatus}
			item, err := fixture.service.StopRun(
				context.Background(), fixture.caller, "project-1", "agent-1", "session-1", "run-1",
			)
			if err != nil {
				t.Fatalf("stop run: %v", err)
			}
			if item.Status != RunRecordStopping || fixture.adapter.stopRunCalls != 1 {
				t.Fatalf("unsafe stop status escaped: run=%#v calls=%d", item, fixture.adapter.stopRunCalls)
			}
		})
	}
}

func TestServiceReplayRunSupportsRerunAndRegenerate(t *testing.T) {
	for _, regenerate := range []bool{false, true} {
		name := "rerun"
		if regenerate {
			name = "regenerate"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newAgentServiceFixture(t)
			fixture.seedSession(SessionActive)
			fixture.seedRun(RunRecordCompleted)
			fixture.adapter.messages = []Message{
				{RemoteID: "message-1", Role: "user", Content: "first question"},
				{RemoteID: "message-2", Role: "assistant", Content: "answer"},
				{RemoteID: "message-3", Role: "user", Content: "latest question"},
			}
			messageID := "message-1"
			expectedInput := "first question"
			if regenerate {
				messageID, expectedInput = "", "latest question"
			}
			result, err := fixture.service.ReplayRun(
				context.Background(), fixture.caller, "project-1", "agent-1",
				"session-1", "run-1", messageID, regenerate,
			)
			if err != nil {
				t.Fatalf("%s run: %v", name, err)
			}
			if result.Run.Source != name || result.Run.SourceRunID != "run-1" ||
				len(fixture.adapter.startRunRequests) != 1 ||
				fixture.adapter.startRunRequests[0].Input != expectedInput {
				t.Fatalf("unexpected %s result: result=%#v request=%#v", name, result, fixture.adapter.startRunRequests)
			}
			if regenerate {
				if fixture.adapter.forkCalls != 1 || result.Session.ParentSessionID != "session-1" ||
					result.Session.ID == "session-1" || fixture.store.sessions["session-1"].Status != SessionEnded {
					t.Fatalf("regenerate did not branch safely: %#v", result)
				}
			} else if result.Session.ID != "session-1" || fixture.adapter.forkCalls != 0 {
				t.Fatalf("rerun unexpectedly forked: %#v", result)
			}
		})
	}
}

func TestServiceStartRunPassesExistingConversationHistory(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.adapter.messages = []Message{
		{RemoteID: "message-1", Role: "user", Content: "Remember cobalt-17."},
		{RemoteID: "message-2", Role: "tool", Content: "ignored tool output"},
		{RemoteID: "message-3", Role: "assistant", Content: "I will remember cobalt-17."},
		{RemoteID: "message-4", Role: "assistant", Content: "   "},
	}

	if _, err := fixture.service.StartRun(
		context.Background(), fixture.caller, "project-1", "agent-1", "session-1",
		StartRunInput{Input: "What value did I ask you to remember?"},
	); err != nil {
		t.Fatalf("start run: %v", err)
	}
	if len(fixture.adapter.startRunRequests) != 1 {
		t.Fatalf("unexpected start requests: %#v", fixture.adapter.startRunRequests)
	}
	request := fixture.adapter.startRunRequests[0]
	if request.SessionRemoteID != "remote-session-1" {
		t.Fatalf("wrong remote session: %#v", request)
	}
	expected := []ConversationMessage{
		{Role: "user", Content: "Remember cobalt-17."},
		{Role: "assistant", Content: "I will remember cobalt-17."},
	}
	if !reflect.DeepEqual(request.ConversationHistory, expected) {
		t.Fatalf("conversation history was not preserved: got=%#v want=%#v", request.ConversationHistory, expected)
	}
}

func TestServiceStartRunPassesValidatedReasoningEffort(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)

	if _, err := fixture.service.StartRun(
		context.Background(), fixture.caller, "project-1", "agent-1", "session-1",
		StartRunInput{Input: "Analyze carefully", ReasoningEffort: "xhigh"},
	); err != nil {
		t.Fatalf("start run: %v", err)
	}
	if got := fixture.adapter.startRunRequests[0].ReasoningEffort; got != "xhigh" {
		t.Fatalf("reasoning effort was not forwarded: %q", got)
	}
	if _, err := fixture.service.StartRun(
		context.Background(), fixture.caller, "project-1", "agent-1", "session-1",
		StartRunInput{Input: "Analyze", ReasoningEffort: "unbounded"},
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid reasoning effort: %v", err)
	}
}

func TestServiceStartRunPassesCurrentAndPreviousSessionFiles(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.seedRun(RunRecordCompleted)
	fixture.service.Artifacts = &agentServiceTestArtifacts{
		attachments: []artifact.ChatAttachment{{
			ArtifactID: "artifact-previous", VersionID: "version-previous",
			RunID: "run-1", Direction: "output", Filename: "heart-curve.png",
			MIMEType: "image/png", SizeBytes: 2048,
		}},
		inputs: []artifact.ChatAttachment{{
			ArtifactID: "artifact-current", VersionID: "version-current",
			Direction: "input", Filename: "measurements.csv",
			MIMEType: "text/csv", SizeBytes: 512,
		}},
	}

	if _, err := fixture.service.StartRun(
		context.Background(), fixture.caller, "project-1", "agent-1", "session-1",
		StartRunInput{Input: "What is the value?", ArtifactIDs: []string{"artifact-current"}},
	); err != nil {
		t.Fatalf("start run: %v", err)
	}
	instructions := fixture.adapter.startRunRequests[0].Instructions
	for _, expected := range []string{
		"heart-curve.png", "artifact-previous", "version-previous", "direction=output",
		"measurements.csv", "artifact-current", "first-class message input",
		"even when the user did not explicitly say", "previously exchanged in this mmdash Session",
	} {
		if !strings.Contains(instructions, expected) {
			t.Fatalf("run instructions do not contain %q:\n%s", expected, instructions)
		}
	}
}

func TestConversationHistoryKeepsTheNewestTwoHundredMessages(t *testing.T) {
	messages := make([]Message, 0, 205)
	for index := range 205 {
		messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("message-%03d", index)})
	}
	history := conversationHistory(messages)
	if len(history) != 200 || history[0].Content != "message-005" || history[199].Content != "message-204" {
		t.Fatalf("unexpected bounded history: first=%#v last=%#v len=%d", history[0], history[len(history)-1], len(history))
	}
}

func TestServiceApproveRunRequiresWaitingStateAndPersistsRunning(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.seedRun(RunRecordWaitingForApproval)
	ctx := requestctx.WithValues(context.Background(), requestctx.Values{RequestID: "approval-request"})
	item, err := fixture.service.ApproveRun(
		ctx, fixture.caller, "project-1", "agent-1", "session-1", "run-1",
		"approval-1", ApprovalSession,
	)
	if err != nil {
		t.Fatalf("approve run: %v", err)
	}
	if item.Status != RunRecordRunning || len(item.PendingApprovalIDs) != 0 ||
		fixture.adapter.approvalRequest.RemoteID != "approval-1" ||
		fixture.adapter.approvalRequest.Choice != ApprovalSession {
		t.Fatalf("approval not persisted: item=%#v request=%#v", item, fixture.adapter.approvalRequest)
	}
	if len(fixture.audit.events) != 1 || fixture.audit.events[0].Action != "agent.run.approve" {
		t.Fatalf("approval audit missing: %#v", fixture.audit.events)
	}
	fixture.store.runs["run-1"] = RunRecord{
		ID: "run-1", RemoteRunID: "remote-run-1", SessionID: "session-1", Status: RunRecordCompleted,
	}
	if _, err := fixture.service.ApproveRun(
		context.Background(), fixture.caller, "project-1", "agent-1", "session-1", "run-1",
		"approval-1", ApprovalOnce,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("approval outside waiting state: %v", err)
	}
}

func TestServiceApproveRunRejectsForgedAndStaleIDsBeforeHermes(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.seedRun(RunRecordWaitingForApproval)
	if _, err := fixture.store.RecordRunApproval(
		context.Background(), "run-1", "approval-2", agentServiceTestNow.Add(time.Second),
	); err != nil {
		t.Fatalf("record queued approval: %v", err)
	}
	for _, approvalID := range []string{
		"forged-approval", "approval-from-previous-run", "approval-2",
	} {
		if _, err := fixture.service.ApproveRun(
			context.Background(), fixture.caller, "project-1", "agent-1",
			"session-1", "run-1", approvalID, ApprovalOnce,
		); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s was not rejected: %v", approvalID, err)
		}
	}
	if fixture.adapter.approvalCalls != 0 {
		t.Fatalf("forged or stale approval reached Hermes: %d", fixture.adapter.approvalCalls)
	}
	actual, err := fixture.store.GetRun(context.Background(), "session-1", "run-1")
	if err != nil || actual.Status != RunRecordWaitingForApproval ||
		len(actual.PendingApprovalIDs) != 2 || actual.PendingApprovalIDs[0] != "approval-1" {
		t.Fatalf("pending approval changed after rejection: %#v %v", actual, err)
	}
}

func TestServiceApproveRunReleasesClaimAfterHermesFailure(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.seedRun(RunRecordWaitingForApproval)
	fixture.adapter.approvalErr = &AdapterError{Code: ErrorUnavailable}
	if _, err := fixture.service.ApproveRun(
		context.Background(), fixture.caller, "project-1", "agent-1",
		"session-1", "run-1", "approval-1", ApprovalOnce,
	); !errors.Is(err, ErrRuntime) {
		t.Fatalf("Hermes failure: %v", err)
	}
	actual, err := fixture.store.GetRun(context.Background(), "session-1", "run-1")
	if err != nil || fixture.store.approvalStates["run-1"]["approval-1"] != "pending" ||
		!containsApprovalID(actual.PendingApprovalIDs, "approval-1") {
		t.Fatalf("failed approval was not safely released: %#v %v", actual, err)
	}
	fixture.adapter.approvalErr = nil
	if _, err := fixture.service.ApproveRun(
		context.Background(), fixture.caller, "project-1", "agent-1",
		"session-1", "run-1", "approval-1", ApprovalOnce,
	); err != nil {
		t.Fatalf("retry same stable approval ID: %v", err)
	}
	if fixture.adapter.approvalCalls != 2 ||
		fixture.adapter.approvalRequest.RemoteID != "approval-1" {
		t.Fatalf("stable approval ID was not retried: calls=%d request=%#v",
			fixture.adapter.approvalCalls, fixture.adapter.approvalRequest)
	}
}

func TestServiceApproveRunClaimsBeforeCallingHermes(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.seedRun(RunRecordWaitingForApproval)
	started := make(chan struct{})
	release := make(chan struct{})
	fixture.adapter.onApprove = func(ApprovalRequest) {
		close(started)
		<-release
	}
	firstResult := make(chan error, 1)
	go func() {
		_, err := fixture.service.ApproveRun(
			context.Background(), fixture.caller, "project-1", "agent-1",
			"session-1", "run-1", "approval-1", ApprovalOnce,
		)
		firstResult <- err
	}()
	<-started
	if _, err := fixture.service.ApproveRun(
		context.Background(), fixture.caller, "project-1", "agent-1",
		"session-1", "run-1", "approval-1", ApprovalOnce,
	); !errors.Is(err, ErrConflict) {
		close(release)
		t.Fatalf("concurrent response was not rejected: %v", err)
	}
	fixture.adapter.approvalMu.Lock()
	approvalCalls := fixture.adapter.approvalCalls
	fixture.adapter.approvalMu.Unlock()
	if approvalCalls != 1 {
		close(release)
		t.Fatalf("concurrent response reached Hermes: %d calls", approvalCalls)
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("claimed approval failed: %v", err)
	}
}

func TestServiceApproveRunReclaimsExpiredClaim(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.seedRun(RunRecordWaitingForApproval)
	fixture.store.approvalStates["run-1"]["approval-1"] = "responding"
	fixture.store.approvalClaims["run-1"]["approval-1"] = "abandoned-claim"
	fixture.store.approvalUpdated["run-1"]["approval-1"] =
		agentServiceTestNow.Add(-runApprovalClaimLease - time.Second)
	if _, err := fixture.service.ApproveRun(
		context.Background(), fixture.caller, "project-1", "agent-1",
		"session-1", "run-1", "approval-1", ApprovalDeny,
	); err != nil {
		t.Fatalf("reclaim abandoned response after restart: %v", err)
	}
	if fixture.adapter.approvalCalls != 1 ||
		fixture.adapter.approvalRequest.RemoteID != "approval-1" {
		t.Fatalf("reclaimed response did not reuse stable ID: %#v",
			fixture.adapter.approvalRequest)
	}
}

func TestServiceStreamRunTracksApprovalIDsIndependently(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.seedRun(RunRecordRunning)
	fixture.adapter.streamRunEvents = []Event{
		{Type: EventApprovalRequested, Approval: &ApprovalEvent{RemoteID: "approval-1"}},
		{Type: EventApprovalRequested, Approval: &ApprovalEvent{RemoteID: "approval-2"}},
		{Type: EventApprovalResponded, Approval: &ApprovalEvent{RemoteID: "approval-1"}},
		{Type: EventRunCompleted},
	}
	var afterOldResponse RunRecord
	if err := fixture.service.StreamRun(
		context.Background(), fixture.caller, "project-1", "agent-1",
		"session-1", "run-1", "", func(ctx context.Context, event Event) error {
			if event.Type == EventApprovalResponded {
				var err error
				afterOldResponse, err = fixture.store.GetRun(ctx, "session-1", "run-1")
				return err
			}
			return nil
		},
	); err != nil {
		t.Fatalf("stream run: %v", err)
	}
	if afterOldResponse.Status != RunRecordWaitingForApproval ||
		len(afterOldResponse.PendingApprovalIDs) != 1 ||
		afterOldResponse.PendingApprovalIDs[0] != "approval-2" {
		t.Fatalf("old response cleared a different pending approval: %#v", afterOldResponse)
	}
	terminal, err := fixture.store.GetRun(context.Background(), "session-1", "run-1")
	if err != nil || terminal.Status != RunRecordCompleted ||
		len(terminal.PendingApprovalIDs) != 0 ||
		fixture.store.approvalStates["run-1"]["approval-2"] != "expired" {
		t.Fatalf("terminal run did not expire pending approvals: %#v %v", terminal, err)
	}
}

func TestServiceStreamRunMapsUnidentifiedResponseToOldestPersistedApproval(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.seedSession(SessionActive)
	fixture.seedRun(RunRecordRunning)
	if _, err := fixture.store.RecordRunApproval(
		context.Background(), "run-1", "approval-1", agentServiceTestNow,
	); err != nil {
		t.Fatalf("record first persisted approval: %v", err)
	}
	if _, err := fixture.store.RecordRunApproval(
		context.Background(), "run-1", "approval-2", agentServiceTestNow.Add(time.Second),
	); err != nil {
		t.Fatalf("record second persisted approval: %v", err)
	}
	fixture.adapter.streamRunEvents = []Event{{
		Type: EventApprovalResponded,
		Approval: &ApprovalEvent{
			Choice: ApprovalOnce, Resolved: 1,
		},
	}}
	var browserApprovalID string
	if err := fixture.service.StreamRun(
		context.Background(), fixture.caller, "project-1", "agent-1",
		"session-1", "run-1", "", func(_ context.Context, event Event) error {
			browserApprovalID = event.Approval.RemoteID
			return nil
		},
	); err != nil {
		t.Fatalf("resume unidentified approval response: %v", err)
	}
	actual, err := fixture.store.GetRun(context.Background(), "session-1", "run-1")
	if err != nil || browserApprovalID != "approval-1" ||
		len(actual.PendingApprovalIDs) != 1 || actual.PendingApprovalIDs[0] != "approval-2" {
		t.Fatalf("unidentified response was not mapped FIFO: browser=%q run=%#v err=%v",
			browserApprovalID, actual, err)
	}
}

func TestServiceStreamRunRejectsNilHandlerBeforeRuntimeAccess(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	if err := fixture.service.StreamRun(
		context.Background(), fixture.caller, "project-1", "agent-1", "session-1", "run-1", "", nil,
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil stream handler: %v", err)
	}
	if fixture.adapter.streamRunCalls != 0 {
		t.Fatalf("runtime stream called with nil handler: %d", fixture.adapter.streamRunCalls)
	}
}

func TestServiceVerifyTokenRequiresTrustedGatewayEvidenceInBothModes(t *testing.T) {
	for _, mode := range []string{ManagementManual, ManagementAuto} {
		t.Run(mode, func(t *testing.T) {
			fixture := newAgentServiceFixture(t)
			instance := fixture.store.instances["agent-1"]
			instance.ManagementMode = mode
			fixture.store.instances["agent-1"] = instance
			lastUsed := agentServiceTestNow.Add(-time.Minute)
			token := auth.AgentToken{
				AgentInstanceID: "agent-1", AllowedTools: append([]string(nil), DefaultAllowedTools...),
				CreatedAt: agentServiceTestNow.Add(-time.Hour), GrantID: "grant-1", ID: "pending-token",
				IssuedBy: "user-1", LastUsedAt: &lastUsed, Name: "Pending",
				ProjectID: "project-1", Status: "pending",
			}
			fixture.authStore.tokens[token.ID] = token
			fixture.store.rotations[token.ID] = TokenRotation{
				GrantID: "grant-1", NewTokenID: token.ID, RotationID: "rotation-1", Status: "awaiting_user",
			}
			if _, err := fixture.service.VerifyToken(
				context.Background(), fixture.caller, "project-1", "agent-1", token.ID,
			); !errors.Is(err, ErrConflict) {
				t.Fatalf("LastUsedAt activated unverified %s token: %v", mode, err)
			}
			if fixture.authStore.activations != 0 {
				t.Fatal("activation attempted without trusted evidence")
			}
			token.Verification = trustedAgentServiceTestEvidence(token)
			fixture.authStore.tokens[token.ID] = token
			updated, err := fixture.service.VerifyToken(
				context.Background(), fixture.caller, "project-1", "agent-1", token.ID,
			)
			if err != nil {
				t.Fatalf("activate verified %s token: %v", mode, err)
			}
			if fixture.authStore.activations != 1 || updated.Status != InstanceActive ||
				updated.ProjectAccessCheck.Status != "passed" {
				t.Fatalf("verified token not activated: instance=%#v activations=%d", updated, fixture.authStore.activations)
			}
		})
	}
}

func TestServiceCreateInstanceValidatesCanonicalHermesProfile(t *testing.T) {
	base := CreateInstanceInput{
		APIKey:         "runtime-secret",
		AllowedTools:   append([]string(nil), DefaultAllowedTools...),
		DisplayName:    "New Hermes",
		ManagementMode: ManagementManual,
		RuntimeURL:     "https://runtime.example.test",
	}
	for _, profile := range []string{" research ", "Research", "research.profile", "research/profile", "hermes", "test", "tmp", "root", "sudo"} {
		fixture := newAgentServiceFixture(t)
		input := base
		input.Profile = profile
		if _, err := fixture.service.CreateInstance(context.Background(), fixture.caller, "project-1", input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CreateInstance accepted profile %q: %v", profile, err)
		}
		if len(fixture.store.instances) != 1 || fixture.settingsStore.upsertCalls != 0 {
			t.Fatalf("invalid profile %q caused a write: instances=%d settings=%d", profile, len(fixture.store.instances), fixture.settingsStore.upsertCalls)
		}
	}
	for _, profile := range []string{"", "default", "research"} {
		fixture := newAgentServiceFixture(t)
		input := base
		input.Profile = profile
		result, err := fixture.service.CreateInstance(context.Background(), fixture.caller, "project-1", input)
		if err != nil {
			t.Fatalf("CreateInstance rejected profile %q: %v", profile, err)
		}
		want := profile
		if want == "" {
			want = "default"
		}
		if result.Instance.Profile != want || fixture.settingsStore.upsertCalls == 0 {
			t.Fatalf("CreateInstance profile %q normalized to %q or did not persist", profile, result.Instance.Profile)
		}
	}
}

func TestServiceCreateInstanceRequiresRuntimeInteroperabilityCheck(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.adapter.checkRuntimeErr = &AdapterError{Code: ErrorUnavailable, Operation: "hermes.runtime_check"}
	input := CreateInstanceInput{
		APIKey: "runtime-secret", AllowedTools: append([]string(nil), DefaultAllowedTools...),
		DisplayName: "New Hermes", ManagementMode: ManagementManual,
		RuntimeURL: "https://runtime.example.test", Profile: "default",
	}
	if _, err := fixture.service.CreateInstance(context.Background(), fixture.caller, "project-1", input); !errors.Is(err, ErrRuntime) {
		t.Fatalf("runtime check failure was not returned: %v", err)
	}
	if fixture.adapter.probeCalls != 1 || fixture.adapter.checkRuntimeCalls != 1 {
		t.Fatalf("runtime checks not invoked exactly once: probe=%d deep=%d", fixture.adapter.probeCalls, fixture.adapter.checkRuntimeCalls)
	}
	if len(fixture.store.instances) != 1 || fixture.settingsStore.upsertCalls != 0 {
		t.Fatalf("failed runtime check wrote instance state: instances=%d settings=%d", len(fixture.store.instances), fixture.settingsStore.upsertCalls)
	}
}

func TestServiceCheckConnectionsDoesNotPassFailedRuntimeInteroperability(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.adapter.checkRuntimeErr = &AdapterError{Code: ErrorUnavailable, Operation: "hermes.runtime_check"}
	item, err := fixture.service.CheckConnections(
		context.Background(), fixture.caller, "project-1", "agent-1", "runtime",
	)
	if err != nil {
		t.Fatalf("runtime connection check returned an unexpected service error: %v", err)
	}
	if fixture.adapter.probeCalls != 1 || fixture.adapter.checkRuntimeCalls != 1 {
		t.Fatalf("runtime checks not invoked exactly once: probe=%d deep=%d", fixture.adapter.probeCalls, fixture.adapter.checkRuntimeCalls)
	}
	if item.RuntimeCheck.Status != "failed" || item.RuntimeCheck.Code != string(ErrorUnavailable) || item.Status != InstanceDegraded {
		t.Fatalf("failed runtime interoperability was marked passed: %#v", item)
	}
}

func TestServiceUpdateInstanceValidatesCanonicalHermesProfileBeforeWrites(t *testing.T) {
	for _, profile := range []string{"", " research ", "Research", "research.profile", "research/profile", "hermes", "test", "tmp", "root", "sudo"} {
		fixture := newAgentServiceFixture(t)
		_, err := fixture.service.UpdateInstance(
			context.Background(), fixture.caller, "project-1", "agent-1",
			UpdateInstanceInput{Profile: stringPointer(profile)},
		)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("UpdateInstance accepted profile %q: %v", profile, err)
		}
		if fixture.settingsStore.upsertCalls != 0 || fixture.store.instanceUpdates != 0 || fixture.store.instances["agent-1"].Profile != "default" {
			t.Fatalf("invalid profile %q caused a write: settings=%d instances=%d stored=%q", profile, fixture.settingsStore.upsertCalls, fixture.store.instanceUpdates, fixture.store.instances["agent-1"].Profile)
		}
	}
	for _, profile := range []string{"default", "research"} {
		fixture := newAgentServiceFixture(t)
		result, err := fixture.service.UpdateInstance(
			context.Background(), fixture.caller, "project-1", "agent-1",
			UpdateInstanceInput{Profile: stringPointer(profile)},
		)
		if err != nil || result.Instance.Profile != profile || fixture.settingsStore.upsertCalls != 1 || fixture.store.instanceUpdates != 1 {
			t.Fatalf("UpdateInstance profile %q result=%#v err=%v settings=%d instances=%d", profile, result.Instance, err, fixture.settingsStore.upsertCalls, fixture.store.instanceUpdates)
		}
	}
}

func TestServiceUpdateInstanceRejectsCrossOriginSecretReuseBeforePersistence(t *testing.T) {
	t.Run("runtime origin requires an explicit API key replacement", func(t *testing.T) {
		for _, apiKey := range []*string{nil, stringPointer(settings.RedactedSecret)} {
			fixture := newAgentServiceFixture(t)
			runtimeURL := "https://other-runtime.example.test"
			_, err := fixture.service.UpdateInstance(
				context.Background(), fixture.caller, "project-1", "agent-1",
				UpdateInstanceInput{RuntimeURL: &runtimeURL, APIKey: apiKey},
			)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("cross-origin runtime reused a prior API key: %v", err)
			}
			if fixture.settingsStore.upsertCalls != 0 || fixture.store.instanceUpdates != 0 {
				t.Fatalf("unsafe runtime update persisted partially: settings=%d instances=%d",
					fixture.settingsStore.upsertCalls, fixture.store.instanceUpdates)
			}
			if stored := fixture.store.instances["agent-1"]; stored.RuntimeURL != "https://runtime.example.test" {
				t.Fatalf("runtime origin changed after rejected update: %#v", stored)
			}
		}
	})

	t.Run("dashboard origin requires explicit replacement or clearing of every credential", func(t *testing.T) {
		fixture := newAgentServiceFixture(t)
		fixture.enableAutoManagement(t, "managed-access-old")
		fixture.setEncryptedSetting(t, settingCFClientID, "old-cf-client")
		fixture.setEncryptedSetting(t, settingCFClientSecret, "old-cf-secret")
		dashboardURL := "https://other-dashboard.example.test"
		dashboardToken := "new-dashboard-token"
		_, err := fixture.service.UpdateInstance(
			context.Background(), fixture.caller, "project-1", "agent-1",
			UpdateInstanceInput{DashboardURL: &dashboardURL, DashboardToken: &dashboardToken},
		)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("cross-origin dashboard reused prior Cloudflare credentials: %v", err)
		}
		if fixture.settingsStore.upsertCalls != 0 || fixture.store.instanceUpdates != 0 {
			t.Fatalf("unsafe dashboard update persisted partially: settings=%d instances=%d",
				fixture.settingsStore.upsertCalls, fixture.store.instanceUpdates)
		}
		if stored := fixture.store.instances["agent-1"]; stored.DashboardURL != "https://dashboard.example.test" {
			t.Fatalf("dashboard origin changed after rejected update: %#v", stored)
		}
	})

	t.Run("all prospective values are validated before a secret write", func(t *testing.T) {
		fixture := newAgentServiceFixture(t)
		runtimeURL := "https://other-runtime.example.test"
		apiKey := "new-runtime-api-key"
		emptyProfile := ""
		_, err := fixture.service.UpdateInstance(
			context.Background(), fixture.caller, "project-1", "agent-1",
			UpdateInstanceInput{RuntimeURL: &runtimeURL, APIKey: &apiKey, Profile: &emptyProfile},
		)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid prospective profile was accepted: %v", err)
		}
		if fixture.settingsStore.upsertCalls != 0 || fixture.store.instanceUpdates != 0 {
			t.Fatalf("invalid prospective configuration persisted partially: settings=%d instances=%d",
				fixture.settingsStore.upsertCalls, fixture.store.instanceUpdates)
		}
	})
}

func TestServiceUpdateInstanceCrossOriginReplacementDoesNotCarryOldSecrets(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.enableAutoManagement(t, "managed-access-old")
	fixture.setEncryptedSetting(t, settingCFClientID, "old-cf-client")
	fixture.setEncryptedSetting(t, settingCFClientSecret, "old-cf-secret")
	dashboardURL := "https://other-dashboard.example.test"
	dashboardToken := "new-dashboard-token"
	clearCloudflare := ""
	result, err := fixture.service.UpdateInstance(
		context.Background(), fixture.caller, "project-1", "agent-1",
		UpdateInstanceInput{
			DashboardURL: &dashboardURL, DashboardToken: &dashboardToken,
			CloudflareClientID: &clearCloudflare, CloudflareClientSecret: &clearCloudflare,
		},
	)
	if err != nil {
		t.Fatalf("replace dashboard origin credentials: %v", err)
	}
	resolved, err := fixture.service.Settings.ResolveResource(
		context.Background(), settings.ScopeProject, "project-1",
		SettingTypeAgentHermes, "agent-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.DashboardURL != dashboardURL ||
		stringValue(resolved.Values[settingDashboardToken]) != dashboardToken ||
		stringValue(resolved.Values[settingCFClientID]) != "" ||
		stringValue(resolved.Values[settingCFClientSecret]) != "" {
		t.Fatalf("old dashboard credentials crossed origins: instance=%#v values=%#v",
			result.Instance, resolved.Values)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestServiceSwitchFromManualToAutoConfiguresFreshCredential(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	oldToken := auth.AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: agentServiceTestNow.Add(-time.Hour),
		GrantID: "grant-1", ID: "old-active-token", ProjectID: "project-1", Status: "active",
	}
	fixture.authStore.tokens[oldToken.ID] = oldToken
	fixture.adapter.configureAccessResult = ProjectAccessResult{
		State: ProjectAccessReady, Route: AccessRouteDirect, RemoteID: "managed-access-1",
		Tools: append([]string(nil), DefaultAllowedTools...), Verified: true,
	}
	fixture.adapter.onConfigureAccess = func(request ProjectAccessRequest) {
		if request.Credential == "" || request.CurrentRemoteID != "" ||
			!strings.Contains(request.Endpoint, "mmdash_challenge=") ||
			!sameTools(request.ExpectedTools, DefaultAllowedTools) {
			t.Fatalf("unexpected manual-to-auto access request: %#v", request)
		}
		fixture.trustLastCreatedToken(t)
	}
	mode := ManagementAuto
	dashboardURL := "https://dashboard.example.test"
	dashboardToken := "dashboard-session-token"
	result, err := fixture.service.UpdateInstance(
		context.Background(), fixture.caller, "project-1", "agent-1", UpdateInstanceInput{
			ManagementMode: &mode, DashboardURL: &dashboardURL, DashboardToken: &dashboardToken,
		},
	)
	if err != nil {
		t.Fatalf("switch manual instance to auto: %v", err)
	}
	newToken := fixture.authStore.tokens[fixture.authStore.lastCreatedTokenID]
	if fixture.adapter.configureAccessCalls != 1 || fixture.adapter.rotateAccessCalls != 0 ||
		newToken.ID == "" || newToken.Status != "active" || newToken.ReplacesTokenID != oldToken.ID {
		t.Fatalf("manual-to-auto did not configure managed access: token=%#v rotate=%d configure=%d",
			newToken, fixture.adapter.rotateAccessCalls, fixture.adapter.configureAccessCalls)
	}
	if old := fixture.authStore.tokens[oldToken.ID]; old.Status != "revoked" || old.RevokedAt == nil {
		t.Fatalf("old credential not revoked after successful activation: %#v", old)
	}
	if result.OneTimeToken != nil || result.Rotation == nil || result.Rotation.Status != "completed" ||
		result.Instance.ManagementMode != ManagementAuto || result.Instance.Status != InstanceActive {
		t.Fatalf("unexpected manual-to-auto result: %#v", result)
	}
}

func TestServiceAutoRotationFinalizesOldAccessOnlyAfterActivation(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.enableAutoManagement(t, "managed-access-old")
	oldToken := auth.AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: agentServiceTestNow.Add(-time.Hour),
		GrantID: "grant-1", ID: "old-active-token", ProjectID: "project-1", Status: "active",
	}
	fixture.authStore.tokens[oldToken.ID] = oldToken
	fixture.adapter.rotateAccessResult = ProjectAccessResult{
		State: ProjectAccessReady, Route: AccessRouteDirect, RemoteID: "managed-access-new",
		Tools: append([]string(nil), DefaultAllowedTools...), Verified: true,
	}
	fixture.adapter.onRotateAccess = func(request ProjectAccessRequest) {
		if request.CurrentRemoteID != "managed-access-old" ||
			!strings.Contains(request.Endpoint, "mmdash_challenge=") {
			t.Fatalf("unexpected previous remote access: %#v", request)
		}
		fixture.trustLastCreatedToken(t)
	}
	fixture.adapter.onFinalizeAccess = func(request ProjectAccessFinalizeRequest) {
		newToken := fixture.authStore.tokens[fixture.authStore.lastCreatedTokenID]
		old := fixture.authStore.tokens[oldToken.ID]
		stored := fixture.store.instances["agent-1"]
		if newToken.Status != "active" || old.Status != "revoked" ||
			stored.Grant.RemoteAccessID != "managed-access-new" {
			t.Fatalf("cleanup ran before durable activation: new=%#v old=%#v grant=%#v",
				newToken, old, stored.Grant)
		}
		if request.ActiveRemoteID != "managed-access-new" ||
			request.PreviousRemoteID != "managed-access-old" {
			t.Fatalf("unexpected finalize request: %#v", request)
		}
	}

	result, err := fixture.service.RotateToken(
		context.Background(), fixture.caller, "project-1", "agent-1", RotateTokenInput{},
	)
	if err != nil {
		t.Fatalf("rotate token: %v", err)
	}
	if fixture.adapter.rotateAccessCalls != 1 || fixture.adapter.finalizeAccessCalls != 1 ||
		result.Rotation == nil || result.Rotation.Status != "completed" ||
		result.Rotation.SafeErrorCode != "" || result.Instance.ProjectAccessCheck.Code != "" {
		t.Fatalf("unexpected completed rotation: result=%#v rotate=%d finalize=%d",
			result, fixture.adapter.rotateAccessCalls, fixture.adapter.finalizeAccessCalls)
	}
}

func TestServiceAutoRotationActivationFailurePreservesOldAccess(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.enableAutoManagement(t, "managed-access-old")
	oldToken := auth.AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: agentServiceTestNow.Add(-time.Hour),
		GrantID: "grant-1", ID: "old-active-token", ProjectID: "project-1", Status: "active",
	}
	fixture.authStore.tokens[oldToken.ID] = oldToken
	fixture.authStore.activateErr = errors.New("activation transaction failed")
	fixture.adapter.rotateAccessResult = ProjectAccessResult{
		State: ProjectAccessReady, Route: AccessRouteDirect, RemoteID: "managed-access-new",
		Tools: append([]string(nil), DefaultAllowedTools...), Verified: true,
	}
	fixture.adapter.onRotateAccess = func(ProjectAccessRequest) {
		fixture.trustLastCreatedToken(t)
	}

	_, err := fixture.service.RotateToken(
		context.Background(), fixture.caller, "project-1", "agent-1", RotateTokenInput{},
	)
	if err == nil || err.Error() != "activation transaction failed" {
		t.Fatalf("expected activation failure, got %v", err)
	}
	if fixture.adapter.finalizeAccessCalls != 0 {
		t.Fatalf("old remote access cleanup ran after activation failure: %d",
			fixture.adapter.finalizeAccessCalls)
	}
	if old := fixture.authStore.tokens[oldToken.ID]; old.Status != "active" || old.RevokedAt != nil {
		t.Fatalf("old token was not preserved: %#v", old)
	}
	stored := fixture.store.instances["agent-1"]
	if stored.Grant.RemoteAccessID != "managed-access-old" {
		t.Fatalf("old remote mapping was not preserved: %#v", stored.Grant)
	}
	rotation := fixture.store.rotations[fixture.authStore.lastCreatedTokenID]
	if rotation.Status != "failed" || rotation.SafeErrorCode != "activation_failed" {
		t.Fatalf("unexpected failed rotation state: %#v", rotation)
	}
}

func TestServiceAutoRotationRecordsCleanupPendingWithoutFailingActivation(t *testing.T) {
	fixture := newAgentServiceFixture(t)
	fixture.enableAutoManagement(t, "managed-access-old")
	fixture.authStore.tokens["old-active-token"] = auth.AgentToken{
		AgentInstanceID: "agent-1", GrantID: "grant-1", ID: "old-active-token",
		ProjectID: "project-1", Status: "active",
	}
	fixture.adapter.rotateAccessResult = ProjectAccessResult{
		State: ProjectAccessReady, Route: AccessRouteDirect, RemoteID: "managed-access-new",
		Tools: append([]string(nil), DefaultAllowedTools...), Verified: true,
	}
	fixture.adapter.onRotateAccess = func(ProjectAccessRequest) {
		fixture.trustLastCreatedToken(t)
	}
	fixture.adapter.finalizeAccessResult = ProjectAccessFinalizeResult{CleanupPending: true}
	fixture.adapter.finalizeAccessErr = &AdapterError{
		Code: ErrorUnavailable, Operation: "project_access.finalize",
		Message: "safe normalized cleanup failure",
	}

	result, err := fixture.service.RotateToken(
		context.Background(), fixture.caller, "project-1", "agent-1", RotateTokenInput{},
	)
	if err != nil {
		t.Fatalf("cleanup failure invalidated activated token: %v", err)
	}
	if result.Rotation == nil || result.Rotation.Status != "completed" ||
		result.Rotation.SafeErrorCode != projectAccessCleanupPending ||
		result.Instance.Status != InstanceActive ||
		result.Instance.ProjectAccessCheck.Status != "passed" ||
		result.Instance.ProjectAccessCheck.Code != projectAccessCleanupPending {
		t.Fatalf("cleanup pending was not safely recorded: %#v", result)
	}
	for _, event := range fixture.audit.events {
		encoded, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		text := string(encoded)
		if strings.Contains(text, "dashboard-session-token") ||
			strings.Contains(text, "managed-access-old") || strings.Contains(text, "managed-access-new") {
			t.Fatalf("cleanup audit leaked remote or credential material: %s", text)
		}
	}
}

func TestServiceRotateTokenRecoversWithoutUsableActiveCredential(t *testing.T) {
	for _, mode := range []string{ManagementManual, ManagementAuto} {
		for _, staleKind := range []string{"revoked", "expired"} {
			t.Run(mode+"/"+staleKind, func(t *testing.T) {
				fixture := newAgentServiceFixture(t)
				instance := fixture.store.instances["agent-1"]
				instance.ManagementMode = mode
				if mode == ManagementAuto {
					instance.DashboardURL = "https://dashboard.example.test"
				}
				fixture.store.instances["agent-1"] = instance
				stale := auth.AgentToken{
					AgentInstanceID: "agent-1", CreatedAt: agentServiceTestNow.Add(-2 * time.Hour),
					GrantID: "grant-1", ID: "stale-token", ProjectID: "project-1", Status: "active",
				}
				if staleKind == "revoked" {
					revokedAt := agentServiceTestNow.Add(-time.Hour)
					stale.Status, stale.RevokedAt = "revoked", &revokedAt
				} else {
					expiresAt := agentServiceTestNow.Add(-time.Second)
					stale.ExpiresAt = &expiresAt
				}
				fixture.authStore.tokens[stale.ID] = stale
				fixture.adapter.configureAccessResult = ProjectAccessResult{
					State: ProjectAccessReady, Route: AccessRouteDirect, RemoteID: "recovered-access",
					Tools: append([]string(nil), DefaultAllowedTools...), Verified: true,
				}
				fixture.adapter.onConfigureAccess = func(request ProjectAccessRequest) {
					if request.CurrentRemoteID != "" || request.Credential == "" ||
						!strings.Contains(request.Endpoint, "mmdash_challenge=") {
						t.Fatalf("recovery attempted unsafe rotation: %#v", request)
					}
					fixture.trustLastCreatedToken(t)
				}
				result, err := fixture.service.RotateToken(
					context.Background(), fixture.caller, "project-1", "agent-1", RotateTokenInput{},
				)
				if err != nil {
					t.Fatalf("recover %s token in %s mode: %v", staleKind, mode, err)
				}
				newToken := fixture.authStore.tokens[fixture.authStore.lastCreatedTokenID]
				if newToken.ID == "" || newToken.ReplacesTokenID != "" ||
					result.Rotation == nil || result.Rotation.OldTokenID != "" {
					t.Fatalf("recovery incorrectly depended on stale token: token=%#v result=%#v", newToken, result)
				}
				if mode == ManagementManual {
					if result.OneTimeToken == nil || result.OneTimeToken.Token == "" ||
						!strings.Contains(result.OneTimeToken.GatewayURL, "mmdash_challenge=") ||
						newToken.Status != "pending" || fixture.adapter.configureAccessCalls != 0 {
						t.Fatalf("manual recovery did not return pending material once: token=%#v result=%#v", newToken, result)
					}
				} else if result.OneTimeToken != nil || newToken.Status != "active" ||
					fixture.adapter.configureAccessCalls != 1 || fixture.adapter.rotateAccessCalls != 0 ||
					result.Rotation.Status != "completed" {
					t.Fatalf("auto recovery did not configure a fresh binding: token=%#v result=%#v configure=%d rotate=%d",
						newToken, result, fixture.adapter.configureAccessCalls, fixture.adapter.rotateAccessCalls)
				}
			})
		}
	}
}

func TestServiceVerifyTokenFailuresAreTerminalAndKeepOldTokenActive(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		fixture := newAgentServiceFixture(t)
		expiresAt := agentServiceTestNow.Add(-time.Second)
		token := auth.AgentToken{
			AgentInstanceID: "agent-1", CreatedAt: agentServiceTestNow.Add(-time.Hour),
			ExpiresAt: &expiresAt, GrantID: "grant-1", ID: "expired-token",
			ProjectID: "project-1", Status: "pending",
		}
		token.Verification = trustedAgentServiceTestEvidence(token)
		fixture.authStore.tokens[token.ID] = token
		fixture.store.rotations[token.ID] = TokenRotation{
			GrantID: "grant-1", NewTokenID: token.ID, RotationID: "rotation-expired", Status: "awaiting_user",
		}
		if _, err := fixture.service.VerifyToken(
			context.Background(), fixture.caller, "project-1", "agent-1", token.ID,
		); !errors.Is(err, ErrConflict) {
			t.Fatalf("verify expired token: %v", err)
		}
		rotation := fixture.store.rotations[token.ID]
		if rotation.Status != "failed" || rotation.SafeErrorCode != "token_expired" ||
			fixture.authStore.activations != 0 {
			t.Fatalf("expired verification was not terminal: rotation=%#v activations=%d", rotation, fixture.authStore.activations)
		}
	})

	t.Run("activation failure", func(t *testing.T) {
		fixture := newAgentServiceFixture(t)
		oldToken := auth.AgentToken{
			AgentInstanceID: "agent-1", GrantID: "grant-1", ID: "old-token",
			ProjectID: "project-1", Status: "active",
		}
		pending := auth.AgentToken{
			AgentInstanceID: "agent-1", CreatedAt: agentServiceTestNow.Add(-time.Hour),
			GrantID: "grant-1", ID: "replacement-token", ProjectID: "project-1",
			ReplacesTokenID: oldToken.ID, Status: "pending",
		}
		pending.Verification = trustedAgentServiceTestEvidence(pending)
		fixture.authStore.tokens[oldToken.ID] = oldToken
		fixture.authStore.tokens[pending.ID] = pending
		fixture.authStore.activateErr = errors.New("transaction failed")
		fixture.store.rotations[pending.ID] = TokenRotation{
			GrantID: "grant-1", NewTokenID: pending.ID, OldTokenID: oldToken.ID,
			RotationID: "rotation-failed", Status: "awaiting_user",
		}
		storedToken, getErr := fixture.service.Auth.GetAgentToken(context.Background(), pending.ID)
		if getErr != nil || !hasAgentVerification(storedToken) {
			t.Fatalf("invalid verified replacement fixture: token=%#v err=%v", storedToken, getErr)
		}
		if _, err := fixture.service.VerifyToken(
			context.Background(), fixture.caller, "project-1", "agent-1", pending.ID,
		); err == nil || err.Error() != "transaction failed" {
			t.Fatalf("verify with activation failure: %v", err)
		}
		rotation := fixture.store.rotations[pending.ID]
		if rotation.Status != "failed" || rotation.SafeErrorCode != "activation_failed" {
			t.Fatalf("activation failure rotation state: %#v", rotation)
		}
		if current := fixture.authStore.tokens[oldToken.ID]; current.Status != "active" || current.RevokedAt != nil {
			t.Fatalf("old token was lost after activation failure: %#v", current)
		}
		if current := fixture.authStore.tokens[pending.ID]; current.Status != "pending" {
			t.Fatalf("replacement token changed after failed activation: %#v", current)
		}
	})
}

func TestServiceConnectionChecksUseTrustedEvidenceAndSafeAudit(t *testing.T) {
	t.Run("manual", func(t *testing.T) {
		fixture := newAgentServiceFixture(t)
		lastUsed := agentServiceTestNow.Add(-time.Minute)
		token := auth.AgentToken{
			AgentInstanceID: "agent-1", CreatedAt: agentServiceTestNow.Add(-time.Hour),
			GrantID: "grant-1", ID: "active-token", LastUsedAt: &lastUsed,
			ProjectID: "project-1", Status: "active",
		}
		fixture.authStore.tokens[token.ID] = token
		ctx := requestctx.WithValues(context.Background(), requestctx.Values{RequestID: "manual-check"})
		item, err := fixture.service.CheckConnections(
			ctx, fixture.caller, "project-1", "agent-1", "all",
		)
		if err != nil {
			t.Fatalf("manual check without evidence: %v", err)
		}
		if item.ProjectAccessCheck.Status != "failed" ||
			item.ProjectAccessCheck.Code != "gateway_verification_missing" || item.Status != InstanceDegraded {
			t.Fatalf("ordinary token use passed manual verification: %#v", item)
		}
		token.Verification = trustedAgentServiceTestEvidence(token)
		fixture.authStore.tokens[token.ID] = token
		item, err = fixture.service.CheckConnections(
			ctx, fixture.caller, "project-1", "agent-1", "all",
		)
		if err != nil {
			t.Fatalf("manual check with evidence: %v", err)
		}
		if item.ProjectAccessCheck.Status != "passed" || item.Status != InstanceActive {
			t.Fatalf("trusted manual verification did not pass: %#v", item)
		}
		assertAgentServiceTestAuditSafe(t, fixture.audit.events)
	})

	t.Run("auto", func(t *testing.T) {
		fixture := newAgentServiceFixture(t)
		instance := fixture.store.instances["agent-1"]
		instance.ManagementMode = ManagementAuto
		instance.DashboardURL = "https://dashboard.example.test"
		fixture.store.instances["agent-1"] = instance
		fixture.adapter.verifyAccess = ProjectAccessResult{
			State: ProjectAccessReady, Route: AccessRouteAuthenticatedProxy,
			RemoteID: "remote-access-1", Tools: append([]string(nil), DefaultAllowedTools...), Verified: true,
		}
		ctx := requestctx.WithValues(context.Background(), requestctx.Values{RequestID: "auto-check"})
		item, err := fixture.service.CheckConnections(
			ctx, fixture.caller, "project-1", "agent-1", "all",
		)
		if err != nil {
			t.Fatalf("auto connection check: %v", err)
		}
		if item.Status != InstanceActive || item.ManagementCheck.Status != "passed" ||
			item.ProjectAccessCheck.Status != "passed" || item.ManagementPath != "cloudflare_access" {
			t.Fatalf("unexpected auto connection state: %#v", item)
		}
		assertAgentServiceTestAuditSafe(t, fixture.audit.events)
	})
}

func assertAgentServiceTestAuditSafe(t *testing.T, events []audit.Event) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("connection check audit event missing")
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal audit events: %v", err)
	}
	text := string(encoded)
	for _, secret := range []string{"top-secret-hermes-key", "dashboard-session-token", "agent-token-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("audit leaked secret %q: %s", secret, text)
		}
	}
	last := events[len(events)-1]
	if last.Action != "agent.connection.check" || last.ResourceID != "agent-1" ||
		last.Metadata["scope"] != "all" {
		t.Fatalf("unexpected connection audit: %#v", last)
	}
}
