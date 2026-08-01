package project

import (
	"context"
	"fmt"

	"github.com/mmdash/mmdash/clients/cli/internal/app"
	"github.com/mmdash/mmdash/clients/cli/internal/apperror"
	cliAuth "github.com/mmdash/mmdash/clients/cli/internal/auth"
)

type Feature struct{}

func (Feature) Register(registry *app.Registry) error {
	for _, command := range []app.Command{listCommand{}, useCommand{}, currentCommand{}} {
		if err := registry.AddCommand(command); err != nil {
			return err
		}
	}
	registry.AddDoctorCheck(currentProjectCheck{})
	return nil
}

type listCommand struct{}

func (listCommand) Name() string    { return "project list" }
func (listCommand) Summary() string { return "List projects visible to the signed-in user" }
func (listCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 0 {
		return apperror.Usage("project list does not accept arguments")
	}
	token, err := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL).AccessToken(ctx, false)
	if err != nil {
		return err
	}
	projects, err := runtime.API.ListProjects(ctx, token)
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(projects)
}

type useCommand struct{}

func (useCommand) Name() string    { return "project use" }
func (useCommand) Summary() string { return "Explicitly select the current project" }
func (useCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 1 {
		return apperror.Usage("Usage: mmdash project use <project_id>")
	}
	token, err := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL).AccessToken(ctx, false)
	if err != nil {
		return err
	}
	project, err := runtime.API.GetProject(ctx, token, arguments[0])
	if err != nil {
		return cliAuth.Translate(err)
	}
	runtime.Config.CurrentProjectID = project.ID
	if err := runtime.SaveConfig(); err != nil {
		return apperror.Wrap("CONFIG_WRITE_FAILED", "Cannot save the current project", 5, err)
	}
	return runtime.Printer.Result(map[string]interface{}{"current_project": project})
}

type currentCommand struct{}

func (currentCommand) Name() string    { return "project current" }
func (currentCommand) Summary() string { return "Show the explicitly selected project" }
func (currentCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 0 {
		return apperror.Usage("project current does not accept arguments")
	}
	if runtime.Config.CurrentProjectID == "" {
		return apperror.New("PROJECT_NOT_SELECTED", "Select a project with 'mmdash project use <project_id>'", 5)
	}
	token, err := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL).AccessToken(ctx, false)
	if err != nil {
		return err
	}
	project, err := runtime.API.GetProject(ctx, token, runtime.Config.CurrentProjectID)
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(project)
}

type currentProjectCheck struct{}

func (currentProjectCheck) Name() string { return "current_project" }
func (currentProjectCheck) Run(ctx context.Context, runtime *app.Runtime) app.CheckResult {
	if runtime.Config.CurrentProjectID == "" {
		return app.CheckResult{Name: "current_project", Status: "fail", Detail: "no project selected"}
	}
	token, err := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL).AccessToken(ctx, false)
	if err != nil {
		return app.CheckResult{Name: "current_project", Status: "fail", Detail: err.Error()}
	}
	project, err := runtime.API.GetProject(ctx, token, runtime.Config.CurrentProjectID)
	if err != nil {
		return app.CheckResult{Name: "current_project", Status: "fail", Detail: err.Error()}
	}
	return app.CheckResult{Name: "current_project", Status: "ok", Detail: fmt.Sprintf("%s (%s)", project.Name, project.ID)}
}
