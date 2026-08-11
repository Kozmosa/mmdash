package progress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

const EvaluationJobType = "progress.evaluate"

var automaticTriggerEvents = map[string]bool{
	"repo.commit.created":        true,
	"repo.commit.detected":       true,
	"model.snapshot.created":     true,
	"experiment.archived":        true,
	"article.build.completed":    true,
	"agent.run.completed":        true,
	"artifact.available":         true,
	"context.confirmed":          true,
	"progress.task.created":      true,
	"progress.task.updated":      true,
	"progress.task.deleted":      true,
	"progress.milestone.created": true,
	"progress.milestone.updated": true,
}

func AutomaticTriggerPatterns() []string {
	patterns := make([]string, 0, len(automaticTriggerEvents))
	for pattern := range automaticTriggerEvents {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns
}

type TrackerState struct {
	ProjectID        string     `json:"project_id"`
	LastEvaluationID string     `json:"last_evaluation_id,omitempty"`
	DetectedStage    string     `json:"detected_stage"`
	EffectiveStage   string     `json:"effective_stage"`
	StageOverridden  bool       `json:"stage_overridden"`
	Summary          string     `json:"summary"`
	ChangesSinceLast []string   `json:"changes_since_last"`
	CompletedItems   []string   `json:"completed_items"`
	InProgressItems  []string   `json:"in_progress_items"`
	Blockers         []string   `json:"blockers"`
	PendingQuestions []string   `json:"pending_questions"`
	LastEvaluatedAt  *time.Time `json:"last_evaluated_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Evaluation struct {
	ID               string                 `json:"evaluation_id"`
	RequestID        string                 `json:"request_id"`
	ProjectID        string                 `json:"project_id"`
	JobID            string                 `json:"job_id,omitempty"`
	Status           string                 `json:"status"`
	InputVersion     string                 `json:"input_version"`
	InputSnapshot    map[string]interface{} `json:"input_snapshot"`
	Output           *EvaluationOutput      `json:"output,omitempty"`
	DetectedStage    string                 `json:"detected_stage"`
	Summary          string                 `json:"summary"`
	ChangesSinceLast []string               `json:"changes_since_last"`
	CompletedItems   []string               `json:"completed_items"`
	InProgressItems  []string               `json:"in_progress_items"`
	Blockers         []string               `json:"blockers"`
	PendingQuestions []string               `json:"pending_questions"`
	SourceEventIDs   []string               `json:"source_event_ids"`
	TriggerKind      string                 `json:"trigger_kind"`
	AgentInstanceID  string                 `json:"agent_instance_id,omitempty"`
	AgentSessionID   string                 `json:"agent_session_id,omitempty"`
	AgentRunID       string                 `json:"agent_run_id,omitempty"`
	EvaluatorMode    string                 `json:"evaluator_mode"`
	Attempts         int                    `json:"attempts"`
	ErrorCode        string                 `json:"error_code,omitempty"`
	ErrorMessage     string                 `json:"error_message,omitempty"`
	RequestedBy      string                 `json:"requested_by"`
	CreatedAt        time.Time              `json:"created_at"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	UpdatedAt        time.Time              `json:"updated_at"`
	Triggers         []EvaluationTrigger    `json:"triggers"`
	Risks            []Risk                 `json:"risks"`
}

type EvaluationTrigger struct {
	ID               string                 `json:"trigger_id"`
	TriggerType      string                 `json:"trigger_type"`
	SourceEventID    string                 `json:"source_event_id,omitempty"`
	SourceEventType  string                 `json:"source_event_type,omitempty"`
	SourceResourceID string                 `json:"source_resource_id,omitempty"`
	SourceVersion    string                 `json:"source_version,omitempty"`
	Payload          map[string]interface{} `json:"payload"`
	OccurredAt       time.Time              `json:"occurred_at"`
}

type Risk struct {
	ID           string    `json:"risk_id"`
	EvaluationID string    `json:"evaluation_id"`
	ProjectID    string    `json:"project_id"`
	Key          string    `json:"risk_key"`
	Title        string    `json:"title"`
	Severity     string    `json:"severity"`
	Status       string    `json:"status"`
	Detail       string    `json:"detail"`
	CreatedAt    time.Time `json:"created_at"`
}

type StageOverride struct {
	ID        string     `json:"override_id"`
	ProjectID string     `json:"project_id"`
	Stage     string     `json:"stage"`
	Summary   string     `json:"summary"`
	Note      string     `json:"note"`
	Active    bool       `json:"active"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	ClearedBy string     `json:"cleared_by,omitempty"`
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
}

type EvaluationSuggestion struct {
	Key          string                 `json:"key"`
	ProposalType string                 `json:"proposal_type"`
	TargetID     string                 `json:"target_id,omitempty"`
	Title        string                 `json:"title"`
	Rationale    string                 `json:"rationale"`
	Changes      map[string]interface{} `json:"changes"`
}

type EvaluationWorkStateUpdate struct {
	TaskID string `json:"task_id"`
	State  string `json:"state"`
}

type EvaluationRisk struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

type EvaluationOutput struct {
	Stage            string                      `json:"stage"`
	Summary          string                      `json:"summary"`
	ChangesSinceLast []string                    `json:"changes_since_last"`
	CompletedItems   []string                    `json:"completed_items"`
	InProgressItems  []string                    `json:"in_progress_items"`
	Blockers         []string                    `json:"blockers"`
	Risks            []EvaluationRisk            `json:"risks"`
	WorkStateUpdates []EvaluationWorkStateUpdate `json:"work_state_updates"`
	Suggestions      []EvaluationSuggestion      `json:"suggestions"`
	PendingQuestions []string                    `json:"pending_questions"`
}

type EvaluationPage struct {
	Items      []Evaluation `json:"items"`
	HasMore    bool         `json:"has_more"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type RecalculateResult struct {
	RequestID    string    `json:"request_id"`
	Status       string    `json:"status"`
	ScheduledFor time.Time `json:"scheduled_for"`
	Merged       bool      `json:"merged"`
}

type UpdateTrackingSettingsInput struct {
	AutoTaskChanges      bool
	AutoTrackingEnabled  bool
	EventTriggersEnabled bool
	CronEnabled          bool
	CronSchedule         string
	DebounceSeconds      int
	MinIntervalSeconds   int
	AgentInstanceID      string
}

type RequestClaim struct {
	ID              string
	ProjectID       string
	ActorID         string
	RequestedByKind string
	TriggerKind     string
	Force           bool
	ScheduledFor    time.Time
	LeaseOwner      string
}

type CronClaim struct {
	ProjectID       string
	AgentInstanceID string
	RemoteJobID     string
	Schedule        string
	Enabled         bool
}

type AgentExecution struct {
	Output          string `json:"output"`
	AgentInstanceID string `json:"agent_instance_id"`
	AgentSessionID  string `json:"agent_session_id"`
	AgentRunID      string `json:"agent_run_id"`
}

type CronResult struct {
	RemoteJobID string
}

type TrackingStore interface {
	GetState(context.Context, string) (TrackerState, error)
	GetLatestEvaluation(context.Context, string) (*Evaluation, error)
	ListEvaluations(context.Context, string, pagination.Request) (EvaluationPage, error)
	GetEvaluation(context.Context, string, string) (Evaluation, error)
	GetEvaluationByJob(context.Context, string) (Evaluation, error)
	GetRisk(context.Context, string, string) (Risk, error)
	GetActiveOverride(context.Context, string) (*StageOverride, error)
	SetStageOverride(context.Context, string, string, string, string, string) (StageOverride, error)
	ClearStageOverride(context.Context, string, string) (StageOverride, error)
	UpdateTrackingSettings(context.Context, string, string, UpdateTrackingSettingsInput) (Settings, error)
	ScheduleRequest(context.Context, string, string, string, string, bool, EvaluationTrigger) (RecalculateResult, error)
	ScheduleEvent(context.Context, contract.EventEnvelope, string) error
	ClaimRequest(context.Context, string, time.Duration) (*RequestClaim, error)
	ReleaseRequest(context.Context, string, string, string, time.Duration) error
	EvaluationContext(context.Context, string) (map[string]interface{}, error)
	FinalizeRequest(context.Context, RequestClaim, map[string]interface{}, string) (*Evaluation, error)
	ClaimCron(context.Context, string, time.Duration) (*CronClaim, error)
	CompleteCron(context.Context, string, string, string) error
	FailCron(context.Context, string, string, string, time.Duration) error
	MarkEvaluationRunning(context.Context, transaction.Tx, jobs.Job) error
	CompleteEvaluation(context.Context, transaction.Tx, jobs.Job, map[string]interface{}) error
	FailEvaluation(context.Context, transaction.Tx, jobs.Job, jobs.Failure) error
}

type EvaluationFactsProvider interface {
	BuildEvaluationFacts(context.Context, string, string) (map[string]interface{}, error)
}

type AgentRuntime interface {
	EvaluateProgress(context.Context, string, string, string, map[string]interface{}) (AgentExecution, error)
	ReconcileProgressCron(context.Context, string, string, string, string, bool) (CronResult, error)
}

type JobAccess interface {
	ClaimedWorkerJob(context.Context, auth.Identity, string) (jobs.Job, error)
}

func (service Service) HandleTrackingEvent(ctx context.Context, event contract.EventEnvelope) error {
	if service.Tracking == nil || event.ProjectID == nil || !automaticTriggerEvents[event.EventType] {
		return nil
	}
	if event.EventType == "agent.run.completed" &&
		strings.TrimSpace(stringMapValue(event.Payload, "source")) == "progress_evaluation" {
		return nil
	}
	if strings.HasPrefix(event.EventType, "progress.") {
		source, _ := event.Payload["source"].(string)
		if source == "agent" || strings.TrimSpace(stringMapValue(event.Payload, "source_evaluation_id")) != "" {
			return nil
		}
	}
	actorID := firstNonEmptyTracking(event.Actor["user_id"], event.Actor["actor_id"])
	return service.Tracking.ScheduleEvent(ctx, event, actorID)
}

func (service Service) Recalculate(ctx context.Context, caller auth.Identity, projectID, triggerKind string, force bool) (RecalculateResult, error) {
	triggerKind = strings.TrimSpace(triggerKind)
	if triggerKind == "" {
		triggerKind = "manual"
	}
	if triggerKind != "manual" && triggerKind != "cron" {
		return RecalculateResult{}, ErrInvalid
	}
	if caller.Kind == "agent" && triggerKind != "cron" {
		return RecalculateResult{}, ErrInvalid
	}
	if force && caller.Kind != "session" {
		return RecalculateResult{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, caller, projectID, project.PermissionProgressEvaluate); err != nil {
		return RecalculateResult{}, err
	}
	actorID := caller.User.ID
	result, err := service.Tracking.ScheduleRequest(ctx, projectID, actorID, caller.Kind, triggerKind, force, EvaluationTrigger{
		TriggerType: triggerKind, Payload: map[string]interface{}{}, OccurredAt: service.now(),
	})
	service.record(ctx, caller, "progress.evaluation.requested", "progress-evaluation-request", result.RequestID, projectID, map[string]interface{}{
		"trigger_kind": triggerKind, "force": force, "merged": result.Merged,
	}, err)
	return result, err
}

func (service Service) RetryEvaluation(ctx context.Context, caller auth.Identity, projectID, evaluationID string) (RecalculateResult, error) {
	if caller.Kind != "session" {
		return RecalculateResult{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, caller, projectID, project.PermissionProgressManage); err != nil {
		return RecalculateResult{}, err
	}
	evaluation, err := service.Tracking.GetEvaluation(ctx, projectID, evaluationID)
	if err != nil {
		return RecalculateResult{}, err
	}
	if evaluation.Status != "failed" {
		return RecalculateResult{}, ErrConflict
	}
	result, err := service.Tracking.ScheduleRequest(ctx, projectID, caller.User.ID, caller.Kind, "retry", true, EvaluationTrigger{
		TriggerType: "retry", SourceResourceID: evaluationID, Payload: map[string]interface{}{"evaluation_id": evaluationID}, OccurredAt: service.now(),
	})
	service.record(ctx, caller, "progress.evaluation.retry", "progress-evaluation", evaluationID, projectID, map[string]interface{}{}, err)
	return result, err
}

func (service Service) ListEvaluations(ctx context.Context, caller auth.Identity, projectID string, page pagination.Request) (EvaluationPage, error) {
	if err := service.Access.Authorize(ctx, caller, projectID, project.PermissionProgressRead); err != nil {
		return EvaluationPage{}, err
	}
	normalized, err := page.Normalize()
	if err != nil {
		return EvaluationPage{}, ErrInvalid
	}
	return service.Tracking.ListEvaluations(ctx, projectID, normalized)
}

func (service Service) ReadEvaluation(ctx context.Context, caller auth.Identity, projectID, evaluationID string) (Evaluation, error) {
	if err := service.Access.Authorize(ctx, caller, projectID, project.PermissionProgressRead); err != nil {
		return Evaluation{}, err
	}
	return service.Tracking.GetEvaluation(ctx, projectID, evaluationID)
}

func (service Service) ReadRisk(ctx context.Context, caller auth.Identity, projectID, riskID string) (Risk, error) {
	if err := service.Access.Authorize(ctx, caller, projectID, project.PermissionProgressRead); err != nil {
		return Risk{}, err
	}
	return service.Tracking.GetRisk(ctx, projectID, riskID)
}

func (service Service) OverrideStage(ctx context.Context, caller auth.Identity, projectID, stage, summary, note string) (StageOverride, error) {
	if caller.Kind != "session" {
		return StageOverride{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, caller, projectID, project.PermissionProgressManage); err != nil {
		return StageOverride{}, err
	}
	stage, summary, note = strings.TrimSpace(stage), strings.TrimSpace(summary), strings.TrimSpace(note)
	if stage == "" || len(stage) > 100 || len(summary) > 2000 || len(note) > 2000 {
		return StageOverride{}, ErrInvalid
	}
	item, err := service.Tracking.SetStageOverride(ctx, projectID, caller.User.ID, stage, summary, note)
	if err != nil {
		service.record(ctx, caller, "progress.stage.overridden", "progress-stage-override", item.ID, projectID, map[string]interface{}{"stage": stage}, err)
	}
	return item, err
}

func (service Service) ClearStageOverride(ctx context.Context, caller auth.Identity, projectID string) (StageOverride, error) {
	if caller.Kind != "session" {
		return StageOverride{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, caller, projectID, project.PermissionProgressManage); err != nil {
		return StageOverride{}, err
	}
	item, err := service.Tracking.ClearStageOverride(ctx, projectID, caller.User.ID)
	if err != nil {
		service.record(ctx, caller, "progress.stage.override.cleared", "progress-stage-override", item.ID, projectID, map[string]interface{}{}, err)
	}
	return item, err
}

func (service Service) UpdateTrackingSettings(ctx context.Context, caller auth.Identity, projectID string, input UpdateTrackingSettingsInput) (Settings, error) {
	if caller.Kind != "session" {
		return Settings{}, ErrHumanRequired
	}
	if err := service.Access.Authorize(ctx, caller, projectID, project.PermissionProgressManage); err != nil {
		return Settings{}, err
	}
	input.CronSchedule = strings.TrimSpace(input.CronSchedule)
	input.AgentInstanceID = strings.TrimSpace(input.AgentInstanceID)
	if input.DebounceSeconds < 0 || input.DebounceSeconds > 3600 || input.MinIntervalSeconds < 0 || input.MinIntervalSeconds > 86400 ||
		(input.CronEnabled && (!validCron(input.CronSchedule) || input.AgentInstanceID == "")) ||
		(input.AutoTrackingEnabled && input.AgentInstanceID == "" && service.evaluatorMode() != "mock") {
		return Settings{}, ErrInvalid
	}
	item, err := service.Tracking.UpdateTrackingSettings(ctx, projectID, caller.User.ID, input)
	item.EvaluatorMode = service.evaluatorMode()
	if err != nil {
		service.record(ctx, caller, "progress.settings.updated", "progress-settings", projectID, projectID, map[string]interface{}{
			"auto_task_changes": input.AutoTaskChanges, "auto_tracking_enabled": input.AutoTrackingEnabled,
			"event_triggers_enabled": input.EventTriggersEnabled, "cron_enabled": input.CronEnabled,
		}, err)
	}
	return item, err
}

func (service Service) ExecuteWorkerEvaluation(ctx context.Context, caller auth.Identity, jobID string) (AgentExecution, error) {
	if service.Jobs == nil || service.Tracking == nil || service.Agent == nil {
		return AgentExecution{}, ErrEvaluationUnavailable
	}
	job, err := service.Jobs.ClaimedWorkerJob(ctx, caller, jobID)
	if err != nil {
		return AgentExecution{}, err
	}
	if job.JobType != EvaluationJobType {
		return AgentExecution{}, ErrInvalid
	}
	evaluation, err := service.Tracking.GetEvaluationByJob(ctx, jobID)
	if err != nil {
		return AgentExecution{}, err
	}
	if evaluation.Status != "running" || evaluation.AgentInstanceID == "" {
		return AgentExecution{}, ErrConflict
	}
	return service.Agent.EvaluateProgress(ctx, evaluation.ProjectID, evaluation.AgentInstanceID, evaluation.ID, evaluation.InputSnapshot)
}

func (service Service) WorkerEvaluationInput(ctx context.Context, caller auth.Identity, jobID string) (Evaluation, error) {
	if service.Jobs == nil || service.Tracking == nil {
		return Evaluation{}, ErrEvaluationUnavailable
	}
	job, err := service.Jobs.ClaimedWorkerJob(ctx, caller, jobID)
	if err != nil {
		return Evaluation{}, err
	}
	if job.JobType != EvaluationJobType {
		return Evaluation{}, ErrInvalid
	}
	return service.Tracking.GetEvaluationByJob(ctx, jobID)
}

func (service Service) PrepareComplete(_ context.Context, job jobs.Job, result map[string]interface{}) error {
	if job.JobType != EvaluationJobType {
		return nil
	}
	_, err := decodeEvaluationResult(result)
	return err
}

func (service Service) ClaimInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job) error {
	if job.JobType != EvaluationJobType || service.Tracking == nil {
		return nil
	}
	return service.Tracking.MarkEvaluationRunning(ctx, tx, job)
}

func (service Service) CompleteInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, result map[string]interface{}) error {
	if job.JobType != EvaluationJobType || service.Tracking == nil {
		return nil
	}
	return service.Tracking.CompleteEvaluation(ctx, tx, job, result)
}

func (service Service) FailInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, failure jobs.Failure) error {
	if job.JobType != EvaluationJobType || service.Tracking == nil {
		return nil
	}
	return service.Tracking.FailEvaluation(ctx, tx, job, failure)
}

func decodeEvaluationResult(result map[string]interface{}) (EvaluationOutput, error) {
	raw, ok := result["output"]
	if !ok {
		return EvaluationOutput{}, ErrInvalidEvaluationOutput
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return EvaluationOutput{}, ErrInvalidEvaluationOutput
	}
	var output EvaluationOutput
	if err := json.Unmarshal(encoded, &output); err != nil {
		return EvaluationOutput{}, ErrInvalidEvaluationOutput
	}
	if output.WorkStateUpdates == nil {
		return EvaluationOutput{}, ErrInvalidEvaluationOutput
	}
	output.Stage = strings.TrimSpace(output.Stage)
	output.Summary = strings.TrimSpace(output.Summary)
	if output.Stage == "" || len(output.Stage) > 100 || output.Summary == "" || len(output.Summary) > 10000 ||
		len(output.ChangesSinceLast) > 200 || len(output.CompletedItems) > 200 || len(output.InProgressItems) > 200 ||
		len(output.Blockers) > 200 || len(output.PendingQuestions) > 200 || len(output.Risks) > 100 || len(output.WorkStateUpdates) > 200 || len(output.Suggestions) > 100 {
		return EvaluationOutput{}, ErrInvalidEvaluationOutput
	}
	for index := range output.Risks {
		risk := &output.Risks[index]
		risk.Key, risk.Title, risk.Detail = strings.TrimSpace(risk.Key), strings.TrimSpace(risk.Title), strings.TrimSpace(risk.Detail)
		if risk.Key == "" || risk.Title == "" || !validRiskSeverity(risk.Severity) {
			return EvaluationOutput{}, ErrInvalidEvaluationOutput
		}
	}
	seenTasks := map[string]bool{}
	for index := range output.WorkStateUpdates {
		update := &output.WorkStateUpdates[index]
		update.TaskID, update.State = strings.TrimSpace(update.TaskID), strings.TrimSpace(update.State)
		if update.TaskID == "" || seenTasks[update.TaskID] || !validAutomaticWorkState(update.State) {
			return EvaluationOutput{}, ErrInvalidEvaluationOutput
		}
		seenTasks[update.TaskID] = true
	}
	seen := map[string]bool{}
	for index := range output.Suggestions {
		suggestion := &output.Suggestions[index]
		suggestion.Key, suggestion.Title = strings.TrimSpace(suggestion.Key), strings.TrimSpace(suggestion.Title)
		if suggestion.Key == "" || seen[suggestion.Key] || suggestion.Title == "" || suggestion.Changes == nil || !validProposalType(suggestion.ProposalType) {
			return EvaluationOutput{}, ErrInvalidEvaluationOutput
		}
		seen[suggestion.Key] = true
	}
	output.ChangesSinceLast = nonNilStrings(output.ChangesSinceLast)
	output.CompletedItems = nonNilStrings(output.CompletedItems)
	output.InProgressItems = nonNilStrings(output.InProgressItems)
	output.Blockers = nonNilStrings(output.Blockers)
	output.PendingQuestions = nonNilStrings(output.PendingQuestions)
	if output.Risks == nil {
		output.Risks = []EvaluationRisk{}
	}
	if output.WorkStateUpdates == nil {
		output.WorkStateUpdates = []EvaluationWorkStateUpdate{}
	}
	if output.Suggestions == nil {
		output.Suggestions = []EvaluationSuggestion{}
	}
	return output, nil
}

func validAutomaticWorkState(value string) bool {
	return value == string(TaskTodo) || value == string(TaskInProgress) || value == string(TaskBlocked)
}

func canonicalInputVersion(input map[string]interface{}) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func mergeEvaluationFacts(base, progressFacts map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range progressFacts {
		result[key] = value
	}
	return result
}

func validRiskSeverity(value string) bool {
	return value == "low" || value == "medium" || value == "high" || value == "critical"
}

func validCron(value string) bool {
	parts := strings.Fields(value)
	return len(parts) == 5 && len(value) <= 100
}

func stringMapValue(values map[string]interface{}, key string) string {
	value, _ := values[key].(string)
	return value
}

func firstNonEmptyTracking(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func evaluationErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidEvaluationOutput):
		return "INVALID_EVALUATION_OUTPUT"
	case errors.Is(err, ErrEvaluationUnavailable):
		return "PROGRESS_EVALUATOR_UNAVAILABLE"
	default:
		return "PROGRESS_EVALUATION_FAILED"
	}
}

func trackingAudit(ctx context.Context, recorder audit.Recorder, tx transaction.Tx, event audit.Event) error {
	if recorder.Store == nil {
		return nil
	}
	if event.RequestID == "" {
		event.RequestID = requestctx.RequestID(ctx)
	}
	return recorder.RecordInTransaction(ctx, tx, event)
}

var (
	ErrEvaluationUnavailable   = fmt.Errorf("progress evaluator unavailable")
	ErrInvalidEvaluationOutput = fmt.Errorf("invalid progress evaluation output")
)
