package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/example"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/config"
	"github.com/mmdash/mmdash/backend/internal/platform/coreapp"
	"github.com/mmdash/mmdash/backend/internal/platform/database"
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

	return server.New(
		processConfig.Addr,
		handler,
		logger,
		processConfig.ShutdownTimeout,
	).Run(ctx)
}
