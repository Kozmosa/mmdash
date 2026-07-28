package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/datahub"
	"github.com/mmdash/mmdash/backend/internal/events"
	"github.com/mmdash/mmdash/backend/internal/example"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/config"
	"github.com/mmdash/mmdash/backend/internal/platform/coreapp"
	"github.com/mmdash/mmdash/backend/internal/platform/database"
	"github.com/mmdash/mmdash/backend/internal/platform/eventbus"
	"github.com/mmdash/mmdash/backend/internal/platform/health"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/logging"
	"github.com/mmdash/mmdash/backend/internal/platform/module"
	"github.com/mmdash/mmdash/backend/internal/platform/objectstorage"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/server"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

func main() {
	logger := logging.New(os.Stderr, clock.System{})
	if err := run(logger); err != nil {
		logger.Error("core.failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
}

func run(logger *logging.Logger) error {
	processConfig, err := config.Load(os.LookupEnv)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startupContext, cancelStartup := context.WithTimeout(ctx, processConfig.StartupTimeout)
	defer cancelStartup()

	db, err := database.OpenPostgres(startupContext, processConfig.Database)
	if err != nil {
		return err
	}
	defer db.Close()

	storage, err := objectstorage.NewMinIO(processConfig.ObjectStorage)
	if err != nil {
		return err
	}
	if err := storage.Check(startupContext); err != nil {
		return err
	}
	openAPI, err := os.ReadFile(processConfig.OpenAPIPath)
	if err != nil {
		return fmt.Errorf("read Core OpenAPI contract: %w", err)
	}

	systemClock := clock.System{}
	idGenerator := identity.Generator{}
	authService := &auth.Service{
		Clock:      systemClock,
		Generator:  idGenerator,
		JWTSecret:  []byte(processConfig.Auth.JWTSecret),
		SessionTTL: processConfig.Auth.SessionTTL,
		Store:      auth.PostgresStore{DB: db},
	}
	if err := authService.EnsureBootstrapUser(
		startupContext,
		processConfig.Auth.BootstrapEmail,
		processConfig.Auth.BootstrapDisplayName,
		processConfig.Auth.BootstrapPassword,
	); err != nil {
		return fmt.Errorf("ensure bootstrap user: %w", err)
	}
	transactionManager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	outboxWriter := outbox.Writer{Clock: systemClock, Generator: idGenerator}
	eventStore := outbox.PostgresStore{
		Clock:       systemClock,
		DB:          db,
		Generator:   idGenerator,
		Transaction: transactionManager,
	}
	eventBus := eventbus.New()
	if err := eventBus.Register(eventbus.Consumer{
		Name:     "platform.system-test-receipt",
		Patterns: []string{"system.test.emitted"},
		Handler: func(context.Context, contract.EventEnvelope) error {
			return nil
		},
	}); err != nil {
		return err
	}
	projectService := &project.Service{
		Auth: authService,
		Store: project.PostgresStore{
			Clock:       systemClock,
			DB:          db,
			Generator:   idGenerator,
			Outbox:      outboxWriter,
			Transaction: transactionManager,
		},
	}
	dataStore := datahub.PostgresStore{
		Clock:       systemClock,
		DB:          db,
		Generator:   idGenerator,
		Outbox:      outboxWriter,
		Transaction: transactionManager,
	}
	dataAdapters := datahub.NewAdapterRegistry()
	if err := dataAdapters.Register("project", datahub.ReaderFunc(
		func(ctx context.Context, caller auth.Identity, object datahub.Object) (interface{}, error) {
			return projectService.Get(ctx, caller, object.ProjectID)
		},
	)); err != nil {
		return err
	}
	if err := dataAdapters.Register("context-proposal", datahub.ReaderFunc(
		func(ctx context.Context, caller auth.Identity, object datahub.Object) (interface{}, error) {
			if err := projectService.Authorize(
				ctx, caller, object.ProjectID, datahub.PermissionContextPropose,
			); err != nil {
				return nil, err
			}
			return dataStore.GetProposal(ctx, object.ProjectID, object.SourceID)
		},
	)); err != nil {
		return err
	}
	if err := dataAdapters.Register("project-context", datahub.ReaderFunc(
		func(ctx context.Context, _ auth.Identity, object datahub.Object) (interface{}, error) {
			return dataStore.GetContext(ctx, object.ProjectID, object.SourceID)
		},
	)); err != nil {
		return err
	}
	projections := datahub.NewProjectionRegistry()
	if err := projections.Register(
		"project.created",
		datahub.ProjectorFunc(dataStore.ProjectCreated),
	); err != nil {
		return err
	}
	if err := projections.Register(
		"project.updated",
		datahub.ProjectorFunc(dataStore.ProjectUpdated),
	); err != nil {
		return err
	}
	if err := eventBus.Register(eventbus.Consumer{
		Name:     "datahub.projections",
		Patterns: projections.Patterns(),
		Handler:  projections.Handle,
	}); err != nil {
		return err
	}
	dataService := datahub.Service{
		Access: projectService, Adapters: dataAdapters,
		Clock: systemClock, Store: dataStore,
	}
	settingsCodec, err := settings.NewSecretCodec(processConfig.Settings.EncryptionKey)
	if err != nil {
		return fmt.Errorf("initialize settings encryption: %w", err)
	}
	settingsRegistry := settings.NewRegistry()
	settingsService := settings.Service{
		Access:   settings.AccessPolicy{Projects: projectService},
		Clock:    systemClock,
		Codec:    settingsCodec,
		Registry: settingsRegistry,
		Store: settings.PostgresStore{
			Clock:       systemClock,
			DB:          db,
			Generator:   idGenerator,
			Outbox:      outboxWriter,
			Transaction: transactionManager,
		},
	}
	jobService := jobs.Service{
		Auth:     authService,
		Clock:    systemClock,
		Projects: projectService,
		Store: jobs.PostgresStore{
			Clock:       systemClock,
			DB:          db,
			Generator:   idGenerator,
			Outbox:      outboxWriter,
			Transaction: transactionManager,
		},
	}
	eventService := events.Service{
		Auth:        authService,
		Bus:         eventBus,
		Outbox:      outboxWriter,
		Store:       eventStore,
		Transaction: transactionManager,
	}
	modules := module.NewRegistry()
	authService.ProjectTokens = projectService
	if err := modules.Register(auth.Module{Service: authService}); err != nil {
		return err
	}
	if err := modules.Register(example.New(example.PostgresChecker{DB: db})); err != nil {
		return err
	}
	if err := modules.Register(project.Module{Service: *projectService}); err != nil {
		return err
	}
	if err := modules.Register(jobs.Module{Service: jobService}); err != nil {
		return err
	}
	if err := modules.Register(events.Module{Service: eventService}); err != nil {
		return err
	}
	if err := modules.Register(datahub.Module{Service: dataService}); err != nil {
		return err
	}
	if err := modules.Register(settings.Module{
		Auth:    authService,
		Service: settingsService,
	}); err != nil {
		return err
	}
	handler := coreapp.NewHandler(coreapp.Options{
		Health: health.Handler{
			Checkers: []health.Checker{
				database.Checker{DB: db},
				storage,
			},
		},
		IDGenerator: idGenerator,
		Logger:      logger,
		Modules:     modules,
		OpenAPI:     openAPI,
	})
	processorID, err := idGenerator.New()
	if err != nil {
		return fmt.Errorf("create Outbox processor identity: %w", err)
	}
	eventProcessor := outbox.Processor{
		Bus:   eventBus,
		Owner: "core-" + processorID,
		Store: eventStore,
		Options: outbox.ProcessorOptions{
			DeliveryLease: processConfig.Outbox.DeliveryLease,
			EventLease:    processConfig.Outbox.EventLease,
			IdlePoll:      processConfig.Outbox.PollInterval,
			RetryDelay:    processConfig.Outbox.RetryDelay,
		},
	}
	go eventProcessor.Run(ctx, func(processorErr error) {
		logger.Error("outbox.processor.failed", map[string]interface{}{
			"error": processorErr.Error(),
		})
	})

	return server.New(
		processConfig.Addr,
		handler,
		logger,
		processConfig.ShutdownTimeout,
	).Run(ctx)
}
