package experiment

import (
	"context"

	"github.com/mmdash/mmdash/clients/cli/internal/app"
	"github.com/mmdash/mmdash/clients/cli/internal/apperror"
	cliAuth "github.com/mmdash/mmdash/clients/cli/internal/auth"
)

// Feature registers the human Experiment commands without coupling the CLI
// to TypeScript or the remote Box implementation.
type Feature struct{}

func (Feature) Register(registry *app.Registry) error {
	for _, command := range []app.Command{listCommand{}, createCommand{}, runCommand{}, statusCommand{}} {
		if err := registry.AddCommand(command); err != nil {
			return err
		}
	}
	return nil
}

type listCommand struct{}

func (listCommand) Name() string    { return "experiment list" }
func (listCommand) Summary() string { return "List frozen experiments in the current project" }
func (listCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 0 {
		return apperror.Usage("experiment list does not accept arguments")
	}
	projectID, token, err := contextFor(ctx, runtime)
	if err != nil {
		return err
	}
	value, err := runtime.API.ListExperiments(ctx, token, projectID)
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(value)
}

type createCommand struct{}

func (createCommand) Name() string    { return "experiment create" }
func (createCommand) Summary() string { return "Create a frozen local-Docker experiment" }
func (createCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 3 {
		return apperror.Usage("Usage: mmdash experiment create <name> <full_commit_sha> <entrypoint>")
	}
	projectID, token, err := contextFor(ctx, runtime)
	if err != nil {
		return err
	}
	value, err := runtime.API.CreateExperiment(ctx, token, projectID, arguments[0], arguments[1], arguments[2])
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(value)
}

type runCommand struct{}

func (runCommand) Name() string    { return "experiment run" }
func (runCommand) Summary() string { return "Queue one frozen experiment" }
func (runCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 1 {
		return apperror.Usage("Usage: mmdash experiment run <experiment_id>")
	}
	projectID, token, err := contextFor(ctx, runtime)
	if err != nil {
		return err
	}
	value, err := runtime.API.RunExperiment(ctx, token, projectID, arguments[0])
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(value)
}

type statusCommand struct{}

func (statusCommand) Name() string    { return "experiment status" }
func (statusCommand) Summary() string { return "Read one experiment lifecycle state" }
func (statusCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 1 {
		return apperror.Usage("Usage: mmdash experiment status <experiment_id>")
	}
	projectID, token, err := contextFor(ctx, runtime)
	if err != nil {
		return err
	}
	value, err := runtime.API.GetExperiment(ctx, token, projectID, arguments[0])
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(value)
}

func contextFor(ctx context.Context, runtime *app.Runtime) (string, string, error) {
	if runtime.Config.CurrentProjectID == "" {
		return "", "", apperror.New("PROJECT_NOT_SELECTED", "Select a project with 'mmdash project use <project_id>'", 5)
	}
	token, err := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL).AccessToken(ctx, false)
	return runtime.Config.CurrentProjectID, token, err
}
