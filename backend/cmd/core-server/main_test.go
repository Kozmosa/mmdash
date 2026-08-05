package main

import (
	"context"
	"testing"
	"time"
)

type progressReminderRunnerStub struct {
	started chan struct{}
}

type invitationExpiryRunnerStub struct {
	started chan struct{}
}

func (runner invitationExpiryRunnerStub) Run(ctx context.Context, _ func(error)) {
	close(runner.started)
	<-ctx.Done()
}

func (runner progressReminderRunnerStub) Run(ctx context.Context, _ func(error)) {
	close(runner.started)
	<-ctx.Done()
}

func TestProgressReminderProcessorStartsAndStopsWithCoreContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := startProgressReminderProcessor(ctx, progressReminderRunnerStub{started: started}, nil)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Progress reminder processor did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Progress reminder processor did not stop with Core context")
	}
}

func TestInvitationExpiryProcessorStartsAndStopsWithCoreContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := startInvitationExpiryProcessor(ctx, invitationExpiryRunnerStub{started: started}, nil)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Project invitation expiry processor did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Project invitation expiry processor did not stop with Core context")
	}
}
