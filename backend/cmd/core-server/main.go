package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mmdash/mmdash/backend/internal/artifact"
	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/datahub"
	"github.com/mmdash/mmdash/backend/internal/events"
	"github.com/mmdash/mmdash/backend/internal/example"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/notification"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/config"
	"github.com/mmdash/mmdash/backend/internal/platform/coreapp"
	"github.com/mmdash/mmdash/backend/internal/platform/database"
	"github.com/mmdash/mmdash/backend/internal/platform/eventbus"
	"github.com/mmdash/mmdash/backend/internal/platform/health"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/logging"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/module"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/server"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/progress"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo"
	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

func main() {
	logger := logging.New(os.Stderr, clock.System{})
	if err := run(logger); err != nil {
		logger.Error("core.failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
}

type registrationSettingsPolicy struct{ Store settings.Store }

func (policy registrationSettingsPolicy) AllowOpenRegistration(ctx context.Context) (bool, error) {
	stored, err := policy.Store.Get(ctx, settings.ScopeSystem, "system", "auth")
	if err != nil {
		if errors.Is(err, settings.ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	value, ok := stored.PublicValues["allow_open_registration"].(bool)
	if !ok {
		return true, nil
	}
	return value, nil
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

	storage, err := artifact.NewBlobStore(
		processConfig.Artifact,
		processConfig.ObjectStorage,
	)
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
		AccessTokenTTL:         processConfig.Auth.AccessTokenTTL,
		Clock:                  systemClock,
		DeviceAuthorizationTTL: processConfig.Auth.DeviceAuthorizationTTL,
		DevicePollInterval:     processConfig.Auth.DevicePollInterval,
		DeviceVerificationURI:  strings.TrimRight(processConfig.PublicURL, "/") + "/cli/authorize",
		Generator:              idGenerator,
		JWTSecret:              []byte(processConfig.Auth.JWTSecret),
		SessionTTL:             processConfig.Auth.SessionTTL,
		Store:                  auth.PostgresStore{DB: db},
	}
	transactionManager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	outboxWriter := outbox.Writer{Clock: systemClock, Generator: idGenerator}
	authService.Store = auth.PostgresStore{DB: db, Outbox: outboxWriter, Transaction: transactionManager}
	if err := authService.EnsureBootstrapUser(startupContext, processConfig.Auth.BootstrapEmail, processConfig.Auth.BootstrapDisplayName, processConfig.Auth.BootstrapPassword); err != nil {
		return fmt.Errorf("ensure bootstrap user: %w", err)
	}
	eventStore := outbox.PostgresStore{
		Clock:       systemClock,
		DB:          db,
		Generator:   idGenerator,
		Transaction: transactionManager,
	}
	eventBus := eventbus.New()
	metricRegistry := metrics.New("core", processConfig.Version)
	auditStore := audit.PostgresStore{
		Clock: systemClock, DB: db, Generator: idGenerator,
	}
	auditRecorder := audit.Recorder{
		Clock: systemClock, Logger: logger,
		Metrics: metricRegistry, Store: auditStore,
	}
	artifactSigner, err := artifact.NewTransferSigner(
		processConfig.Auth.JWTSecret,
		processConfig.PublicURL,
	)
	if err != nil {
		return fmt.Errorf("initialize Artifact transfer signer: %w", err)
	}
	artifactWorkerSigner, err := artifact.NewTransferSigner(
		processConfig.Auth.JWTSecret,
		processConfig.InternalURL,
	)
	if err != nil {
		return fmt.Errorf("initialize Artifact Worker transfer signer: %w", err)
	}
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
		Auth:           authService,
		Clock:          systemClock,
		InvitationTTL:  72 * time.Hour,
		TrashRetention: 30 * 24 * time.Hour,
		Store: project.PostgresStore{
			Clock:       systemClock,
			DB:          db,
			Generator:   idGenerator,
			Outbox:      outboxWriter,
			Transaction: transactionManager,
		},
	}
	jobStore := jobs.PostgresStore{
		Clock:       systemClock,
		DB:          db,
		Generator:   idGenerator,
		Outbox:      outboxWriter,
		Transaction: transactionManager,
	}
	artifactStore := artifact.PostgresStore{
		Audit: auditRecorder, DB: db, Generator: idGenerator,
		Jobs: jobStore, Outbox: &outboxWriter, Transaction: transactionManager,
	}
	artifactService := artifact.Service{
		Access: projectService, Audit: auditRecorder,
		Clock: systemClock, Generator: idGenerator,
		MaxPreviewOutputBytes: processConfig.Artifact.PreviewOutputMaxBytes,
		MaxUploadBytes:        processConfig.Artifact.UploadMaxBytes,
		Metrics:               metricRegistry,
		MultipartPartBytes:    processConfig.Artifact.MultipartPartBytes,
		Signer:                artifactSigner, Storage: storage,
		Store:            artifactStore,
		TransferTTL:      processConfig.Artifact.MultipartURLTTL,
		UploadSessionTTL: processConfig.Artifact.MultipartSessionTTL,
		WorkerSigner:     artifactWorkerSigner,
	}
	previewHook := artifactService
	jobStore.Hooks = []jobs.LifecycleHook{previewHook}
	jobService := jobs.Service{
		Auth: authService, Clock: systemClock,
		Hooks:    []jobs.LifecycleHook{previewHook},
		Projects: projectService, Store: jobStore,
	}
	artifactService.Jobs = jobService
	artifactModule := artifact.Module{Service: artifactService}
	dataStore := datahub.PostgresStore{
		Clock:       systemClock,
		DB:          db,
		Generator:   idGenerator,
		Outbox:      outboxWriter,
		Transaction: transactionManager,
	}
	progressStore := progress.PostgresStore{
		Clock: systemClock, DB: db, Generator: idGenerator,
		Outbox: outboxWriter, ReminderLease: processConfig.Progress.ReminderLease,
		References:         dataStore,
		ReminderRetryDelay: processConfig.Progress.ReminderRetryDelay,
		Transaction:        transactionManager,
	}
	progressService := &progress.Service{
		Access: projectService, Audit: auditRecorder, Clock: systemClock,
		Generator: idGenerator, Store: progressStore,
	}
	notificationRegistry, err := notification.DefaultRegistry()
	if err != nil {
		return fmt.Errorf("register Notification types: %w", err)
	}
	notificationStore := notification.PostgresStore{Clock: systemClock, DB: db, Generator: idGenerator, Transaction: transactionManager}
	notificationService := notification.Service{
		Clock: systemClock, Generator: idGenerator, Registry: notificationRegistry,
		Store: notificationStore, Deliveries: notificationStore,
		Access: projectService, Audit: auditRecorder, Metrics: metricRegistry,
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
	for _, definition := range []struct {
		objectType string
		read       func(context.Context, auth.Identity, string, string) (interface{}, error)
	}{
		{"milestone", func(ctx context.Context, caller auth.Identity, projectID, sourceID string) (interface{}, error) {
			return progressService.ReadMilestone(ctx, caller, projectID, sourceID)
		}},
		{"task", func(ctx context.Context, caller auth.Identity, projectID, sourceID string) (interface{}, error) {
			return progressService.ReadTask(ctx, caller, projectID, sourceID)
		}},
		{"progress_proposal", func(ctx context.Context, caller auth.Identity, projectID, sourceID string) (interface{}, error) {
			return progressService.ReadProposal(ctx, caller, projectID, sourceID)
		}},
	} {
		definition := definition
		if err := dataAdapters.Register(definition.objectType, datahub.ReaderFunc(func(ctx context.Context, caller auth.Identity, object datahub.Object) (interface{}, error) {
			return definition.read(ctx, caller, object.ProjectID, object.SourceID)
		})); err != nil {
			return err
		}
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
	for _, pattern := range []string{"project.member.joined", "project.member.role_changed", "project.member.removed"} {
		if err := projections.Register(pattern, datahub.ProjectorFunc(dataStore.ProjectMemberChanged)); err != nil {
			return err
		}
	}
	dataService := datahub.Service{
		Access: projectService, Adapters: dataAdapters,
		Clock: systemClock, Store: dataStore,
		Progress: progressService,
	}
	auditService := audit.Service{
		Access: projectService, Clock: systemClock, Store: auditStore,
	}
	settingsCodec, err := settings.NewSecretCodec(processConfig.Settings.EncryptionKey)
	if err != nil {
		return fmt.Errorf("initialize settings encryption: %w", err)
	}
	webhookHTTPClient := notification.NewWebhookHTTPClient(http.DefaultClient)
	feishuWebhookAdapter := notification.FeishuWebhook{
		AllowHTTPLoopback: processConfig.Notification.WebhookAllowHTTPLoopback,
		Client:            webhookHTTPClient,
		Clock:             func() time.Time { return systemClock.Now().UTC() },
	}
	genericWebhookAdapter := notification.GenericWebhook{
		AllowHTTPLoopback: processConfig.Notification.WebhookAllowHTTPLoopback,
		Client:            webhookHTTPClient,
		Clock:             func() time.Time { return systemClock.Now().UTC() },
	}
	settingsRegistry := settings.NewRegistry()
	if err := settingsRegistry.Register(settings.TypeDefinition{
		Description: "Controls public account registration.",
		Fields: []settings.FieldDefinition{
			{Key: "allow_open_registration", Kind: settings.FieldBoolean, Label: "Allow open registration", Required: true},
		},
		Key: "auth", Order: 10, Owner: "auth", Scopes: []settings.Scope{settings.ScopeSystem}, Title: "Authentication",
	}); err != nil {
		return err
	}
	if err := settingsRegistry.Register(settings.TypeDefinition{
		Description: "Project Feishu webhook notification channel.",
		Fields: []settings.FieldDefinition{
			{Key: "enabled", Kind: settings.FieldBoolean, Label: "Enabled", Required: true},
			{Key: "webhook_url", Kind: settings.FieldSecret, Label: "Webhook URL", Required: true},
		},
		Key: "notification.feishu_webhook", Order: 70, Owner: "notification", Scopes: []settings.Scope{settings.ScopeProject}, Title: "Feishu Webhook", Tester: notification.SettingTester{Adapter: feishuWebhookAdapter}, Validator: feishuWebhookAdapter,
	}); err != nil {
		return err
	}
	if err := settingsRegistry.Register(settings.TypeDefinition{
		Description: "Project generic signed webhook notification channel.",
		Fields: []settings.FieldDefinition{
			{Key: "enabled", Kind: settings.FieldBoolean, Label: "Enabled", Required: true},
			{Key: "endpoint", Kind: settings.FieldURL, Label: "Endpoint", Required: true},
			{Key: "signing_secret", Kind: settings.FieldSecret, Label: "Signing secret", Required: true},
		},
		Key: "notification.generic_webhook", Order: 71, Owner: "notification", Scopes: []settings.Scope{settings.ScopeProject}, Tester: notification.SettingTester{Adapter: genericWebhookAdapter}, Title: "Generic Webhook", Validator: genericWebhookAdapter,
	}); err != nil {
		return err
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("find Git executable: %w", err)
	}
	askPassPath, err := exec.LookPath(processConfig.Repo.AskPassPath)
	if err != nil {
		return fmt.Errorf("find Repo AskPass executable: %w", err)
	}
	repoStorage, err := gitcli.NewStorage(processConfig.Repo.StorageRoot)
	if err != nil {
		return fmt.Errorf("initialize Repo storage: %w", err)
	}
	gitOutputBytes := int(processConfig.Repo.MaxTextBytes)
	if gitOutputBytes < 16*1024*1024 {
		gitOutputBytes = 16 * 1024 * 1024
	}
	gitClient, err := gitcli.NewClient(
		gitPath,
		askPassPath,
		processConfig.Repo.CommandTimeout,
		processConfig.Repo.MaxConcurrentGit,
		gitOutputBytes,
	)
	if err != nil {
		return fmt.Errorf("initialize Git runtime: %w", err)
	}
	repoProviders := provider.NewRegistry()
	if err := repoProviders.Register("github", provider.GitHub{
		Git: gitClient, RuntimeRoot: repoStorage.Root(),
		UserAgent: "mmdash-core/" + processConfig.Version,
	}); err != nil {
		return err
	}
	if err := repoProviders.Register("local", provider.Local{
		AllowedRoots: processConfig.Repo.LocalAllowedRoots,
		Git:          gitClient,
	}); err != nil {
		return err
	}
	if err := settingsRegistry.Register(repo.SettingDefinition(
		repo.ConnectionTester{Providers: repoProviders},
	)); err != nil {
		return err
	}
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
	notificationService.Settings = settingsService
	authService.Policy = registrationSettingsPolicy{Store: settingsService.Store}
	repoStore := repo.PostgresStore{
		Clock: systemClock, DB: db, Generator: idGenerator,
		Outbox: outboxWriter, Transaction: transactionManager,
	}
	repoRuntime := repo.Runtime{
		Clock: systemClock, CloneTimeout: processConfig.Repo.CloneTimeout,
		Git: gitClient, Storage: repoStorage,
	}
	repoWriter := &repo.WorkspaceWriter{
		Clock: systemClock, Git: gitClient,
		Runtime: repoRuntime, Storage: repoStorage,
	}
	repoService := repo.Service{
		Access: projectService, Audit: auditRecorder,
		Checkouts: repoStore, CheckoutTTL: processConfig.Repo.CheckoutTTL,
		Clock: systemClock, Commits: repoStore,
		DisconnectGrace: processConfig.Repo.DisconnectGrace,
		Generator:       idGenerator,
		MaxWriteBytes:   processConfig.Repo.MaxTextBytes,
		Providers:       repoProviders,
		PublicURL:       processConfig.PublicURL,
		Reads: &repo.Reader{
			Clock: systemClock, Git: gitClient,
			MaxTextBytes: processConfig.Repo.MaxTextBytes,
			Storage:      repoStorage,
		},
		Settings: settingsService,
		Storage:  repoStorage,
		Store:    repoStore, WriteLease: processConfig.Repo.SyncLease,
		Webhooks: repoStore, Writer: repoWriter,
	}
	artifactService.Git = artifact.RepoGitContentReader{Service: &repoService}
	artifactModule.Service = artifactService
	projectService.Artifacts = artifactService
	dataService.Problem = artifact.ProjectHomeReader{
		Projects: projectService, Service: &artifactService,
	}
	repoModule := repo.Module{Service: repoService}
	artifactDataReader := artifact.DataHubReaderAdapter{
		Registry: artifactStore, Service: &artifactService,
	}
	for _, objectType := range []string{
		"artifact", "attachment_registry_entry",
	} {
		if err := dataAdapters.Register(
			objectType, datahub.ReaderFunc(artifactDataReader.Read),
		); err != nil {
			return err
		}
	}
	repoDataReader := repo.DataHubReaderAdapter{Service: &repoService}
	if err := dataAdapters.Register("repository", datahub.ReaderFunc(
		func(
			ctx context.Context,
			caller auth.Identity,
			object datahub.Object,
		) (interface{}, error) {
			return repoDataReader.Repository(
				ctx, caller, object.ProjectID,
			)
		},
	)); err != nil {
		return err
	}
	if err := dataAdapters.Register("repo_commit", datahub.ReaderFunc(
		func(
			ctx context.Context,
			caller auth.Identity,
			object datahub.Object,
		) (interface{}, error) {
			return repoDataReader.Commit(
				ctx, caller, object.ProjectID, object.Metadata,
			)
		},
	)); err != nil {
		return err
	}
	if err := dataAdapters.Register("repo_file", datahub.ReaderFunc(
		func(
			ctx context.Context,
			caller auth.Identity,
			object datahub.Object,
		) (interface{}, error) {
			return repoDataReader.File(
				ctx, caller, object.ProjectID, object.Metadata,
			)
		},
	)); err != nil {
		return err
	}
	repoProjector := repo.DataHubProjector{
		BatchSize:    200,
		Reader:       repoService.Reads,
		Repositories: repoStore,
		Sink:         dataStore,
	}
	for _, eventType := range []string{
		"repo.connected", "repo.commit.created", "repo.commit.detected",
	} {
		if err := projections.Register(
			eventType, datahub.ProjectorFunc(repoProjector.Project),
		); err != nil {
			return err
		}
	}
	artifactProjector := artifact.DataHubProjector{
		Reader: artifactStore, Sink: dataStore,
	}
	for _, eventType := range []string{
		"artifact.created", "artifact.available", "artifact.deleted",
	} {
		if err := projections.Register(
			eventType, datahub.ProjectorFunc(artifactProjector.Project),
		); err != nil {
			return err
		}
	}
	for _, eventType := range []string{
		"progress.milestone.created", "progress.milestone.updated",
		"progress.task.created", "progress.task.updated", "progress.task.deleted",
		"progress.proposal.created", "progress.proposal.reviewed",
	} {
		if err := projections.Register(eventType, datahub.ProjectorFunc(dataStore.ProjectProgress)); err != nil {
			return err
		}
	}
	if err := eventBus.Register(eventbus.Consumer{
		Name:     "datahub.projections",
		Patterns: projections.Patterns(),
		Handler:  projections.Handle,
	}); err != nil {
		return err
	}
	if err := eventBus.Register(eventbus.Consumer{
		Name: "notification.inbox-project-invitations", Patterns: []string{"project.member.invited", "project.member.joined", "project.invitation.revoked", "project.invitation.expired"}, Handler: notificationService.HandleEvent,
	}); err != nil {
		return err
	}
	if err := eventBus.Register(eventbus.Consumer{
		Name: "notification.bind-registered-user", Patterns: []string{"user.registered"}, Handler: notificationService.HandleEvent,
	}); err != nil {
		return err
	}
	if err := eventBus.Register(eventbus.Consumer{
		Name: "notification.progress-reminders", Patterns: []string{"progress.reminder.due"}, Handler: notificationService.HandleEvent,
	}); err != nil {
		return err
	}
	if err := eventBus.Register(eventbus.Consumer{
		Name: "notification.settings", Patterns: []string{"settings.updated", "settings.deleted"}, Handler: notificationService.HandleSettingsEvent,
	}); err != nil {
		return err
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
	authService.Invitations = projectService
	if err := modules.Register(auth.Module{Service: authService}); err != nil {
		return err
	}
	if err := modules.Register(example.New(example.PostgresChecker{DB: db})); err != nil {
		return err
	}
	if err := modules.Register(project.Module{
		Artifact:     artifactModule.ProjectHandler(),
		Notification: notification.Module{Auth: authService, Service: notificationService, Settings: settingsService}.ProjectHandler(),
		Progress:     progress.Module{Service: *progressService}.ProjectHandler(),
		Repository:   repoModule.ProjectHandler(),
		Service:      *projectService,
	}); err != nil {
		return err
	}
	if err := modules.Register(notification.Module{Auth: authService, Service: notificationService, Settings: settingsService}); err != nil {
		return err
	}
	if err := modules.Register(artifactModule); err != nil {
		return err
	}
	if err := modules.Register(repoModule); err != nil {
		return err
	}
	if err := modules.Register(progress.Module{Service: *progressService}); err != nil {
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
	if err := modules.Register(audit.Module{Service: auditService}); err != nil {
		return err
	}
	if err := modules.Register(settings.Module{
		Auth:    authService,
		Service: settingsService,
	}); err != nil {
		return err
	}
	handler := coreapp.NewHandler(coreapp.Options{
		Audit: func(ctx context.Context, observation coreapp.HTTPObservation) error {
			outcome := "success"
			if observation.Status == 401 || observation.Status == 403 {
				outcome = "denied"
			} else if observation.Status >= 400 {
				outcome = "error"
			}
			duration := observation.DurationMS
			return auditRecorder.Record(ctx, audit.Event{
				Action: "http.request.completed", Category: "http",
				DurationMS: &duration, Outcome: outcome,
				Source: "core", ResourceType: "http-route",
				ResourceID: observation.Path,
				Metadata: map[string]interface{}{
					"method": observation.Method,
					"status": observation.Status,
				},
			})
		},
		Health: health.Handler{
			Checkers: []health.Checker{
				database.Checker{DB: db},
				storage,
				repo.GitChecker{Client: gitClient, Directory: repoStorage.Root()},
				repo.StorageChecker{Storage: repoStorage},
			},
			Version: processConfig.Version,
		},
		IDGenerator: idGenerator,
		Logger:      logger,
		Modules:     modules,
		Metrics:     metricRegistry,
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
	progressReminderProcessorID, err := idGenerator.New()
	if err != nil {
		return fmt.Errorf("create Progress reminder processor identity: %w", err)
	}
	startProgressReminderProcessor(ctx, progress.ReminderProcessor{
		BatchSize:  processConfig.Progress.ReminderBatchSize,
		Lease:      processConfig.Progress.ReminderLease,
		Metrics:    metricRegistry,
		Owner:      "core-progress-reminder-" + progressReminderProcessorID,
		Poll:       processConfig.Progress.ReminderPollInterval,
		RetryDelay: processConfig.Progress.ReminderRetryDelay,
		Store:      progressStore,
	}, func(processorErr error) {
		logger.Error("progress.reminder.processor.failed", map[string]interface{}{
			"error": processorErr.Error(),
		})
	})
	notificationAdapters := notification.NewAdapterRegistry()
	if err := notificationAdapters.Register(feishuWebhookAdapter); err != nil {
		return err
	}
	if err := notificationAdapters.Register(genericWebhookAdapter); err != nil {
		return err
	}
	notificationProcessorID, err := idGenerator.New()
	if err != nil {
		return fmt.Errorf("create Notification delivery processor identity: %w", err)
	}
	go (notification.DeliveryProcessor{Adapters: notificationAdapters, Deliveries: notificationStore, Lease: processConfig.Outbox.DeliveryLease, Owner: "core-notification-" + notificationProcessorID, Settings: settingsService, Metrics: metricRegistry}).Run(ctx, func(processorErr error) {
		logger.Error("notification.delivery.failed", map[string]interface{}{"error": processorErr.Error()})
	})
	syncOwnerID, err := idGenerator.New()
	if err != nil {
		return fmt.Errorf("create Repo sync owner identity: %w", err)
	}
	if err := (repo.Reconciler{
		Checkouts: repoStore, Clock: systemClock,
		Repositories: repoStore, Runtime: repoRuntime,
	}).Run(ctx); err != nil {
		return fmt.Errorf("reconcile Repo worktrees: %w", err)
	}
	repoCoordinator := repo.Coordinator{
		BatchSize: processConfig.Repo.MaxConcurrentGit,
		Clock:     systemClock,
		Lease:     processConfig.Repo.SyncLease,
		Metrics:   metricRegistry,
		OnError: func(syncErr error) {
			logger.Error("repo.sync.failed", map[string]interface{}{
				"error": syncErr.Error(),
			})
		},
		Owner:     "core-" + syncOwnerID,
		Poll:      processConfig.Repo.SyncPollInterval,
		Providers: repoProviders,
		Runtime:   repoRuntime,
		Settings:  settingsService,
		Store:     repoStore,
	}
	go repoCoordinator.Run(ctx)
	go (repo.CheckoutReaper{
		Clock: systemClock, Interval: time.Minute, Limit: 50,
		OnError: func(checkoutErr error) {
			logger.Error("repo.checkout.cleanup.failed", map[string]interface{}{
				"error": checkoutErr.Error(),
			})
		},
		Repositories: repoStore, Runtime: repoRuntime, Store: repoStore,
	}).Run(ctx)
	go (repo.CleanupReaper{
		Clock: systemClock, Interval: time.Minute, Lease: 5 * time.Minute,
		Limit: 10, Metrics: metricRegistry,
		OnError: func(cleanupErr error) {
			logger.Error("repo.storage.cleanup.failed", map[string]interface{}{
				"error": cleanupErr.Error(),
			})
		},
		Storage: repoStorage, Store: repoStore,
	}).Run(ctx)
	go (repo.MetricsCollector{
		Clock: systemClock, Interval: time.Minute, Metrics: metricRegistry,
		OnError: func(metricsErr error) {
			logger.Error("repo.metrics.collection.failed", map[string]interface{}{
				"error": metricsErr.Error(),
			})
		},
		Storage: repoStorage, Store: repoStore,
	}).Run(ctx)
	go (artifact.UploadReaper{
		Backend:  serviceBackend(storage),
		Interval: processConfig.Artifact.StagingSweepInterval,
		Limit:    50, Metrics: metricRegistry,
		OnError: func(cleanupErr error) {
			logger.Error("artifact.staging.cleanup.failed", map[string]interface{}{
				"error": cleanupErr.Error(),
			})
		},
		Service: artifactService,
	}).Run(ctx)

	return server.New(
		processConfig.Addr,
		handler,
		logger,
		processConfig.ShutdownTimeout,
	).Run(ctx)
}

type progressReminderRunner interface {
	Run(context.Context, func(error))
}

func startProgressReminderProcessor(ctx context.Context, runner progressReminderRunner, onError func(error)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx, onError)
	}()
	return done
}

func serviceBackend(storage artifact.BlobStore) string {
	if storage == nil {
		return "unknown"
	}
	return storage.Backend()
}
