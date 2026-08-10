// Package e2b is a provider-neutral E2B Runtime Adapter. Provider fields are
// kept behind the SandboxRuntime interface and never enter Core contracts.
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
