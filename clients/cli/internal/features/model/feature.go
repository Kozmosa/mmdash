package model

import (
	"context"

	"github.com/mmdash/mmdash/clients/cli/internal/app"
	"github.com/mmdash/mmdash/clients/cli/internal/apperror"
	cliAuth "github.com/mmdash/mmdash/clients/cli/internal/auth"
)

// Feature registers Stage 7 human Model commands at compile time.
type Feature struct{}

func (Feature) Register(registry *app.Registry) error {
	for _, command := range []app.Command{listCommand{}, showCommand{}, syncCommand{}} {
		if err := registry.AddCommand(command); err != nil {
			return err
		}
	}
	return nil
}

type listCommand struct{}

func (listCommand) Name() string    { return "model list" }
func (listCommand) Summary() string { return "List Model questions and Notion synchronization state" }
func (listCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 0 {
		return apperror.Usage("model list does not accept arguments")
	}
	projectID, token, err := modelContext(ctx, runtime)
	if err != nil {
		return err
	}
	value, err := runtime.API.GetModels(ctx, token, projectID)
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(value)
}

type showCommand struct{}

func (showCommand) Name() string    { return "model show" }
func (showCommand) Summary() string { return "Show one Model question and its Snapshot timeline" }
func (showCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 1 {
		return apperror.Usage("Usage: mmdash model show <question_id>")
	}
	projectID, token, err := modelContext(ctx, runtime)
	if err != nil {
		return err
	}
	value, err := runtime.API.GetModelQuestion(ctx, token, projectID, arguments[0])
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(value)
}

type syncCommand struct{}

func (syncCommand) Name() string { return "model sync" }
func (syncCommand) Summary() string {
	return "Synchronize all models or one question and reset the countdown"
}
func (syncCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) > 1 {
		return apperror.Usage("Usage: mmdash model sync [question_id]")
	}
	projectID, token, err := modelContext(ctx, runtime)
	if err != nil {
		return err
	}
	questionID := ""
	if len(arguments) == 1 {
		questionID = arguments[0]
	}
	value, err := runtime.API.SyncModels(ctx, token, projectID, questionID)
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(value)
}

func modelContext(ctx context.Context, runtime *app.Runtime) (string, string, error) {
	if runtime.Config.CurrentProjectID == "" {
		return "", "", apperror.New("PROJECT_NOT_SELECTED", "Select a project with 'mmdash project use <project_id>'", 5)
	}
	token, err := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL).AccessToken(ctx, false)
	return runtime.Config.CurrentProjectID, token, err
}
