package progress

import (
	"context"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
)

// TrackingProcessor reconciles desired Hermes Cron jobs and moves debounced
// requests into the shared PostgreSQL Job Queue. It never evaluates or mutates
// Progress-owned records outside the Progress service boundary.
type TrackingProcessor struct {
	Agent      AgentRuntime
	Facts      EvaluationFactsProvider
	Lease      time.Duration
	Metrics    *metrics.Registry
	Owner      string
	Poll       time.Duration
	RetryDelay time.Duration
	Store      TrackingStore
}

func (processor TrackingProcessor) Run(ctx context.Context, onError func(error)) {
	poll := processor.Poll
	if poll <= 0 {
		poll = time.Second
	}
	for {
		if err := processor.ReconcileCronOnce(ctx); err != nil && onError != nil {
			onError(err)
		}
		if err := processor.DispatchOnce(ctx); err != nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(poll):
		}
	}
}

func (processor TrackingProcessor) DispatchOnce(ctx context.Context) error {
	claim, err := processor.Store.ClaimRequest(ctx, processor.Owner, processor.lease())
	if err != nil || claim == nil {
		return err
	}
	base, err := processor.Facts.BuildEvaluationFacts(ctx, claim.ProjectID, claim.ActorID)
	if err != nil {
		_ = processor.Store.ReleaseRequest(ctx, claim.ID, processor.Owner, "INPUT_ASSEMBLY_FAILED", processor.retryDelay())
		processor.observe("assembly_failed")
		return err
	}
	progressFacts, err := processor.Store.EvaluationContext(ctx, claim.ProjectID)
	if err != nil {
		_ = processor.Store.ReleaseRequest(ctx, claim.ID, processor.Owner, "PROGRESS_CONTEXT_FAILED", processor.retryDelay())
		processor.observe("assembly_failed")
		return err
	}
	input := mergeEvaluationFacts(base, progressFacts)
	version, err := canonicalInputVersion(input)
	if err != nil {
		_ = processor.Store.ReleaseRequest(ctx, claim.ID, processor.Owner, "INPUT_VERSION_FAILED", processor.retryDelay())
		processor.observe("assembly_failed")
		return err
	}
	evaluation, err := processor.Store.FinalizeRequest(ctx, *claim, input, version)
	if err != nil {
		_ = processor.Store.ReleaseRequest(ctx, claim.ID, processor.Owner, "QUEUE_FAILED", processor.retryDelay())
		processor.observe("queue_failed")
		return err
	}
	if evaluation == nil {
		processor.observe("merged")
		return nil
	}
	processor.observe("queued")
	return nil
}

func (processor TrackingProcessor) ReconcileCronOnce(ctx context.Context) error {
	if processor.Agent == nil {
		return nil
	}
	claim, err := processor.Store.ClaimCron(ctx, processor.Owner, processor.lease())
	if err != nil || claim == nil {
		return err
	}
	result, err := processor.Agent.ReconcileProgressCron(
		ctx, claim.ProjectID, claim.AgentInstanceID, claim.RemoteJobID,
		claim.Schedule, claim.Enabled,
	)
	if err != nil {
		_ = processor.Store.FailCron(ctx, claim.ProjectID, processor.Owner,
			"HERMES_CRON_SYNC_FAILED", processor.retryDelay())
		processor.observe("cron_failed")
		return err
	}
	if err := processor.Store.CompleteCron(ctx, claim.ProjectID, processor.Owner, result.RemoteJobID); err != nil {
		return err
	}
	processor.observe("cron_synced")
	return nil
}

func (processor TrackingProcessor) lease() time.Duration {
	if processor.Lease <= 0 {
		return 2 * time.Minute
	}
	return processor.Lease
}

func (processor TrackingProcessor) retryDelay() time.Duration {
	if processor.RetryDelay <= 0 {
		return 30 * time.Second
	}
	return processor.RetryDelay
}

func (processor TrackingProcessor) observe(outcome string) {
	if processor.Metrics != nil {
		processor.Metrics.ObserveProgressEvaluation(outcome)
	}
}
