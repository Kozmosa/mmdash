// Package e2b implements the E2B Runtime Adapter. Provider fields and
// credentials stay behind the Sandbox Runtime interface and never enter Core
// contracts.
package e2b

import (
	"context"
	"errors"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
)

type Client interface {
	Run(context.Context, string, sandbox.RunRequest) (sandbox.RunResult, error)
	Cancel(context.Context, string) error
}

type Runtime struct {
	Template string
	Client   Client
}

func (runtime Runtime) Probe(ctx context.Context) error {
	if runtime.Client == nil || runtime.Template == "" {
		return errors.New("E2B runtime is not configured")
	}
	prober, ok := runtime.Client.(interface {
		Probe(context.Context, string) error
	})
	if !ok {
		return errors.New("E2B client does not implement lifecycle probing")
	}
	return prober.Probe(ctx, runtime.Template)
}

func (runtime Runtime) Destroy(ctx context.Context, id string) error {
	if runtime.Client == nil {
		return errors.New("E2B runtime is not configured")
	}
	if destroyer, ok := runtime.Client.(interface {
		Destroy(context.Context, string) error
	}); ok {
		return destroyer.Destroy(ctx, id)
	}
	return runtime.Client.Cancel(ctx, id)
}

func (runtime Runtime) Run(ctx context.Context, request sandbox.RunRequest) (sandbox.RunResult, error) {
	if runtime.Client == nil || runtime.Template == "" {
		return sandbox.RunResult{}, errors.New("E2B runtime is not configured")
	}
	return runtime.Client.Run(ctx, runtime.Template, request)
}

func (runtime Runtime) Cancel(ctx context.Context, id string) error {
	if runtime.Client == nil {
		return errors.New("E2B runtime is not configured")
	}
	return runtime.Client.Cancel(ctx, id)
}
