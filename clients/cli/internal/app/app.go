package app

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mmdash/mmdash/clients/cli/internal/api"
	"github.com/mmdash/mmdash/clients/cli/internal/apperror"
	"github.com/mmdash/mmdash/clients/cli/internal/config"
	"github.com/mmdash/mmdash/clients/cli/internal/credentials"
	"github.com/mmdash/mmdash/clients/cli/internal/output"
)

type Command interface {
	Name() string
	Summary() string
	Run(context.Context, *Runtime, []string) error
}

type DoctorCheck interface {
	Name() string
	Run(context.Context, *Runtime) CheckResult
}

type CheckResult struct {
	Detail string `json:"detail"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Feature interface{ Register(*Registry) error }

type Registry struct {
	commands map[string]Command
	checks   []DoctorCheck
}

func (registry *Registry) AddCommand(command Command) error {
	name := strings.TrimSpace(command.Name())
	if name == "" {
		return fmt.Errorf("command name is empty")
	}
	if _, exists := registry.commands[name]; exists {
		return fmt.Errorf("command %q already registered", name)
	}
	registry.commands[name] = command
	return nil
}

func (registry *Registry) AddDoctorCheck(check DoctorCheck) {
	registry.checks = append(registry.checks, check)
}

type Runtime struct {
	API             *api.Client
	Config          config.Config
	CredentialStore credentials.Store
	DoctorChecks    []DoctorCheck
	Paths           config.Paths
	Printer         output.Printer
	Stdin           io.Reader
	Version         string
}

func (runtime *Runtime) SaveConfig() error { return config.Save(runtime.Paths, runtime.Config) }

type Options struct {
	CredentialStore credentials.Store
	Features        []Feature
	Stderr          io.Writer
	Stdin           io.Reader
	Stdout          io.Writer
	Version         string
}

type App struct {
	options  Options
	registry Registry
}

func New(options Options) (*App, error) {
	registry := Registry{commands: map[string]Command{}}
	for _, feature := range options.Features {
		if err := feature.Register(&registry); err != nil {
			return nil, err
		}
	}
	return &App{options: options, registry: registry}, nil
}

func (application *App) Run(ctx context.Context, arguments []string) int {
	jsonOutput, arguments := consumeFlag(arguments, "--json")
	printer := output.Printer{JSON: jsonOutput, Stderr: application.options.Stderr, Stdout: application.options.Stdout}
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		return application.help(printer, arguments)
	}
	if arguments[0] == "--version" || arguments[0] == "version" {
		_ = printer.Result(application.options.Version)
		return 0
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return printer.Error(apperror.Wrap("CONFIG_PATH_ERROR", "Cannot resolve the CLI config directory", 5, err))
	}
	loaded, err := config.Load(paths)
	if err != nil {
		return printer.Error(apperror.Wrap("CONFIG_INVALID", err.Error(), 5, err))
	}
	runtime := &Runtime{API: api.NewClient(loaded.CoreURL), Config: loaded, CredentialStore: application.options.CredentialStore, DoctorChecks: append([]DoctorCheck(nil), application.registry.checks...), Paths: paths, Printer: printer, Stdin: application.options.Stdin, Version: application.options.Version}
	command, remaining := application.resolve(arguments)
	if command == nil {
		return printer.Error(apperror.Usage("Unknown command %q", strings.Join(arguments, " ")))
	}
	if err := command.Run(ctx, runtime, remaining); err != nil {
		return printer.Error(err)
	}
	return 0
}

func (application *App) resolve(arguments []string) (Command, []string) {
	for length := len(arguments); length > 0; length-- {
		if command := application.registry.commands[strings.Join(arguments[:length], " ")]; command != nil {
			return command, arguments[length:]
		}
	}
	return nil, arguments
}

func (application *App) help(printer output.Printer, arguments []string) int {
	if len(arguments) > 1 {
		if command := application.registry.commands[strings.Join(arguments[1:], " ")]; command != nil {
			_ = printer.Result(command.Name() + " - " + command.Summary())
			return 0
		}
		return printer.Error(apperror.Usage("Unknown help topic %q", strings.Join(arguments[1:], " ")))
	}
	names := make([]string, 0, len(application.registry.commands))
	for name := range application.registry.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	builder.WriteString("mmdash - collaborative research CLI\n\nCommands:\n")
	for _, name := range names {
		fmt.Fprintf(&builder, "  %-20s %s\n", name, application.registry.commands[name].Summary())
	}
	builder.WriteString("\nGlobal flags: --json, --version\n")
	_ = printer.Result(strings.TrimRight(builder.String(), "\n"))
	return 0
}

func consumeFlag(arguments []string, flag string) (bool, []string) {
	filtered := make([]string, 0, len(arguments))
	found := false
	for _, argument := range arguments {
		if argument == flag {
			found = true
		} else {
			filtered = append(filtered, argument)
		}
	}
	return found, filtered
}
