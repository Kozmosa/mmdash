package agent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/artifact"
	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const (
	SettingTypeAgentHermes      = "agent.hermes"
	settingAPIKey               = "api_key"
	settingProfile              = "profile"
	settingRequestTimeout       = "request_timeout_seconds"
	settingDashboardToken       = "dashboard_session_token"
	settingCFClientID           = "cloudflare_access_client_id"
	settingCFClientSecret       = "cloudflare_access_client_secret"
	projectAccessCleanupPending = "project_access_cleanup_pending"
)

type ProjectAccess interface {
	Authorize(context.Context, auth.Identity, string, project.Permission) error
	Get(context.Context, auth.Identity, string) (project.Project, error)
}

type Service struct {
	Adapters  *Registry
	Artifacts interface {
		AttachAgentRunInputs(context.Context, auth.Identity, string, string, []string) ([]artifact.ChatAttachment, error)
		ListAgentRunAttachments(context.Context, auth.Identity, string, []string) ([]artifact.ChatAttachment, error)
	}
	Audit      audit.Recorder
	Auth       *auth.Service
	Clock      interface{ Now() time.Time }
	GatewayURL string
	Generator  identity.Generator
	Metrics    *metrics.Registry
	Projects   ProjectAccess
	Settings   *settings.Service
	Store      Store
}

func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Auth.Authenticate(ctx, authorization)
}

func (service Service) ListCredentials(ctx context.Context, grantID string) ([]auth.AgentToken, error) {
	return service.Auth.ListAgentTokens(ctx, grantID)
}

func (service Service) GetCredential(ctx context.Context, tokenID string) (auth.AgentToken, error) {
	return service.Auth.GetAgentToken(ctx, tokenID)
}

func (service Service) ListAdapters() []Descriptor {
	if service.Adapters == nil {
		return []Descriptor{}
	}
	return service.Adapters.Descriptors()
}

func (service Service) AgentHomeItems(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
) ([]interface{}, error) {
	instances, err := service.ListInstances(ctx, caller, projectID)
	if err != nil {
		return nil, err
	}
	items := make([]interface{}, 0, len(instances))
	for _, instance := range instances {
		sessions, err := service.Store.ListSessions(ctx, projectID, instance.ID)
		if err != nil {
			return nil, err
		}
		activeSessions := 0
		for _, session := range sessions {
			if session.Status == SessionActive {
				activeSessions++
			}
		}
		items = append(items, map[string]interface{}{
			"active_session_count": activeSessions,
			"agent_instance_id":    instance.ID,
			"display_name":         instance.DisplayName,
			"management_mode":      instance.ManagementMode,
			"mcp_status":           instance.ProjectAccessCheck.Status,
			"runtime_status":       instance.RuntimeCheck.Status,
			"status":               instance.Status,
		})
	}
	return items, nil
}

func (service Service) ListInstances(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
) ([]Instance, error) {
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentRead); err != nil {
		return nil, err
	}
	items, err := service.Store.ListInstances(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for index := range items {
		service.enrichSecretStatus(ctx, caller, projectID, &items[index])
	}
	return items, nil
}

func (service Service) GetInstance(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
) (Instance, error) {
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentRead); err != nil {
		return Instance{}, err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return Instance{}, err
	}
	service.enrichSecretStatus(ctx, caller, projectID, &item)
	return item, nil
}

func (service Service) CreateInstance(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	input CreateInstanceInput,
) (InstanceResult, error) {
	if err := requireHumanSession(caller); err != nil {
		return InstanceResult{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentManage); err != nil {
		return InstanceResult{}, err
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.RuntimeURL = strings.TrimSpace(input.RuntimeURL)
	input.DashboardURL = strings.TrimSpace(input.DashboardURL)
	if input.Profile == "" {
		input.Profile = "default"
	}
	if err := ValidateHermesProfile(input.Profile); err != nil {
		return InstanceResult{}, ErrInvalid
	}
	if input.DisplayName == "" || input.APIKey == "" ||
		!validOrigin(input.RuntimeURL) || !validManagementMode(input.ManagementMode) {
		return InstanceResult{}, ErrInvalid
	}
	if input.DashboardURL != "" && !validOrigin(input.DashboardURL) {
		return InstanceResult{}, ErrInvalid
	}
	if input.ManagementMode == ManagementAuto &&
		(input.DashboardURL == "" || input.DashboardToken == "") {
		return InstanceResult{}, ErrInvalid
	}
	if input.ManagementMode == ManagementManual &&
		(input.DashboardToken != "" || input.CloudflareClientID != "" || input.CloudflareClientSecret != "") {
		return InstanceResult{}, ErrInvalid
	}
	if input.RequestTimeoutSeconds < 0 || input.RequestTimeoutSeconds > 300 {
		return InstanceResult{}, ErrInvalid
	}
	requestTimeout := input.RequestTimeoutSeconds
	if requestTimeout == 0 {
		requestTimeout = 30
	}
	descriptor, exists := service.Adapters.Descriptor(AdapterHermes)
	if !exists {
		return InstanceResult{}, ErrNotConfigured
	}
	if input.ManagementMode == ManagementAuto &&
		(!descriptor.Capabilities.ProjectAccess.Configure ||
			!descriptor.Capabilities.ProjectAccess.Rotate ||
			!descriptor.Capabilities.ProjectAccess.Verify) {
		return InstanceResult{}, ErrInvalid
	}
	tools, err := normalizeTools(input.AllowedTools)
	if err != nil {
		return InstanceResult{}, err
	}
	instanceID, err := service.Generator.New()
	if err != nil {
		return InstanceResult{}, err
	}
	grantID, err := service.Generator.New()
	if err != nil {
		return InstanceResult{}, err
	}
	now := service.now()
	adapter, err := service.Adapters.New(ctx, AdapterHermes, AdapterConfig{
		InstanceID: instanceID,
		Values: adapterValues(input.RuntimeURL, input.APIKey, input.Profile,
			input.DashboardURL, input.DashboardToken,
			input.CloudflareClientID, input.CloudflareClientSecret,
			requestTimeoutSeconds(input.RequestTimeoutSeconds)),
	})
	if err != nil {
		return InstanceResult{}, mapAdapterError(err)
	}
	probe, err := adapter.Probe(ctx)
	if err != nil || !probe.Healthy || !probe.Authenticated ||
		!probe.Capabilities.Sessions || !probe.Capabilities.Runs ||
		!probe.Capabilities.RunStreaming || !probe.Capabilities.RunStop {
		service.observe("probe", err)
		return InstanceResult{}, mapAdapterErrorOr(err, "runtime_capability_missing")
	}
	if err := adapter.CheckRuntime(ctx); err != nil {
		service.observe("runtime_check", err)
		return InstanceResult{}, mapAdapterError(err)
	}
	instance := Instance{
		AdapterType:  AdapterHermes,
		Capabilities: capabilityMap(probe.Capabilities),
		CreatedAt:    now, CreatedBy: caller.User.ID,
		DashboardURL: input.DashboardURL, DisplayName: input.DisplayName,
		ID: instanceID, ManagementMode: input.ManagementMode, Profile: input.Profile,
		ManagementPath: "unreachable", RuntimeURL: input.RuntimeURL,
		RequestTimeoutSeconds: requestTimeout,
		RuntimeCheck:          CheckSnapshot{CheckedAt: now, Status: "passed"},
		Status:                InstanceSetupPending, UpdatedAt: now, Version: 1,
	}
	if input.ManagementMode == ManagementAuto {
		instance.Status = InstanceConfiguring
	}
	grant := ProjectGrant{
		AgentInstanceID: instanceID, AllowedTools: tools,
		CreatedAt: now, CreatedBy: caller.User.ID, GrantID: grantID,
		ProjectID: projectID, Role: string(project.RoleAgent), Status: "active",
		UpdatedAt: now, Version: 1,
	}
	instance, err = service.Store.CreateInstance(ctx, caller.User.ID, caller.Kind, instance, grant)
	if err != nil {
		return InstanceResult{}, err
	}
	if _, err := service.Settings.UpdateResource(ctx, caller, settings.ScopeProject,
		projectID, SettingTypeAgentHermes, instanceID, settingsPatch(input)); err != nil {
		_ = service.Store.SaveChecks(ctx, instanceID, instance.RuntimeCheck,
			CheckSnapshot{CheckedAt: now, Status: "failed", Code: "settings_failed"},
			CheckSnapshot{}, "unreachable", instance.Capabilities,
			InstanceDegraded, now)
		return InstanceResult{}, err
	}
	issued, rotation, err := service.issuePendingToken(ctx, caller, instance, grant, "", RotateTokenInput{})
	if err != nil {
		return InstanceResult{}, err
	}
	result := InstanceResult{Instance: instance}
	if input.ManagementMode == ManagementManual {
		result.OneTimeToken = &OneTimeTokenMaterial{
			AllowedTools: append([]string(nil), tools...),
			GatewayURL:   verificationEndpoint(service.GatewayURL, issued.Challenge),
			Token:        issued.Secret, TokenID: issued.Token.ID,
		}
		return result, nil
	}
	if err := service.configureAndActivate(ctx, caller, &instance, grant, issued, rotation, adapter); err != nil {
		// Auto mode never returns the one-time secret to the browser. The pending
		// token remains available for an explicit retry while no old token is lost.
		result.Instance = instance
		return result, err
	}
	result.Instance = instance
	return result, nil
}

func (service Service) UpdateInstance(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	input UpdateInstanceInput,
) (InstanceResult, error) {
	if err := requireHumanSession(caller); err != nil {
		return InstanceResult{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentManage); err != nil {
		return InstanceResult{}, err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return InstanceResult{}, err
	}
	if input.Profile != nil && (*input.Profile == "" || ValidateHermesProfile(*input.Profile) != nil) {
		return InstanceResult{}, ErrInvalid
	}
	resolved, err := service.Settings.ResolveResource(ctx, settings.ScopeProject,
		projectID, SettingTypeAgentHermes, instanceID)
	if err != nil {
		return InstanceResult{}, err
	}
	previousRuntimeURL := item.RuntimeURL
	previousDashboardURL := item.DashboardURL
	previousManagementMode := item.ManagementMode
	configurationChanged := input.ManagementMode != nil || input.RuntimeURL != nil ||
		input.APIKey != nil || input.Profile != nil || input.DashboardURL != nil ||
		input.DashboardToken != nil || input.CloudflareClientID != nil ||
		input.CloudflareClientSecret != nil || input.RequestTimeoutSeconds != nil
	patch := map[string]interface{}{}
	if input.DisplayName != nil {
		item.DisplayName = strings.TrimSpace(*input.DisplayName)
	}
	if input.RuntimeURL != nil {
		item.RuntimeURL = strings.TrimSpace(*input.RuntimeURL)
	}
	if input.DashboardURL != nil {
		item.DashboardURL = strings.TrimSpace(*input.DashboardURL)
	}
	if input.ManagementMode != nil {
		item.ManagementMode = strings.TrimSpace(*input.ManagementMode)
	}
	if item.DisplayName == "" || !validOrigin(item.RuntimeURL) ||
		!validManagementMode(item.ManagementMode) ||
		(item.DashboardURL != "" && !validOrigin(item.DashboardURL)) {
		return InstanceResult{}, ErrInvalid
	}
	if originChanged(previousRuntimeURL, item.RuntimeURL) &&
		!explicitSecretUpdate(input.APIKey) {
		return InstanceResult{}, ErrInvalid
	}
	dashboardOriginChanged := originChanged(previousDashboardURL, item.DashboardURL)
	if dashboardOriginChanged && item.ManagementMode != ManagementManual {
		for key, update := range map[string]*string{
			settingDashboardToken: input.DashboardToken,
			settingCFClientID:     input.CloudflareClientID,
			settingCFClientSecret: input.CloudflareClientSecret,
		} {
			if stringValue(resolved.Values[key]) != "" && !explicitSecretUpdate(update) {
				return InstanceResult{}, ErrInvalid
			}
		}
	}
	// Cloudflare Access service credentials are one logical credential. Never
	// combine one newly supplied half with the other half from a prior update.
	if (input.CloudflareClientID == nil) != (input.CloudflareClientSecret == nil) {
		return InstanceResult{}, ErrInvalid
	}
	prospectiveSecrets := map[string]string{
		settingAPIKey:         stringValue(resolved.Values[settingAPIKey]),
		settingDashboardToken: stringValue(resolved.Values[settingDashboardToken]),
		settingCFClientID:     stringValue(resolved.Values[settingCFClientID]),
		settingCFClientSecret: stringValue(resolved.Values[settingCFClientSecret]),
	}
	applySecret := func(key string, value *string) {
		if value == nil || *value == settings.RedactedSecret {
			return
		}
		prospectiveSecrets[key] = *value
		if *value == "" {
			patch[key] = nil
			return
		}
		patch[key] = *value
	}
	applySecret(settingAPIKey, input.APIKey)
	applySecret(settingDashboardToken, input.DashboardToken)
	applySecret(settingCFClientID, input.CloudflareClientID)
	applySecret(settingCFClientSecret, input.CloudflareClientSecret)
	if input.Profile != nil {
		item.Profile = *input.Profile
		patch[settingProfile] = item.Profile
	}
	if input.RequestTimeoutSeconds != nil {
		if *input.RequestTimeoutSeconds < 1 || *input.RequestTimeoutSeconds > 300 {
			return InstanceResult{}, ErrInvalid
		}
		patch[settingRequestTimeout] = float64(*input.RequestTimeoutSeconds)
		item.RequestTimeoutSeconds = *input.RequestTimeoutSeconds
	}
	if item.ManagementMode == ManagementManual {
		patch[settingDashboardToken] = nil
		patch[settingCFClientID] = nil
		patch[settingCFClientSecret] = nil
		prospectiveSecrets[settingDashboardToken] = ""
		prospectiveSecrets[settingCFClientID] = ""
		prospectiveSecrets[settingCFClientSecret] = ""
		item.ManagementPath = "unreachable"
	}
	if item.Profile == "" || prospectiveSecrets[settingAPIKey] == "" {
		return InstanceResult{}, ErrInvalid
	}
	if item.ManagementMode == ManagementAuto &&
		(item.DashboardURL == "" || prospectiveSecrets[settingDashboardToken] == "") {
		return InstanceResult{}, ErrInvalid
	}
	if (prospectiveSecrets[settingCFClientID] == "") !=
		(prospectiveSecrets[settingCFClientSecret] == "") {
		return InstanceResult{}, ErrInvalid
	}
	var requestedTools []string
	toolsChanged := false
	if input.AllowedTools != nil {
		requestedTools, err = normalizeTools(*input.AllowedTools)
		if err != nil {
			return InstanceResult{}, err
		}
		toolsChanged = !sameTools(requestedTools, item.Grant.AllowedTools)
	}
	needsAutoConfiguration := previousManagementMode != ManagementAuto &&
		item.ManagementMode == ManagementAuto
	// Every validation above is read-only. Persist secrets first only after the
	// complete prospective configuration is known to be safe; this prevents an
	// invalid origin/credential patch from leaving a partial settings update.
	if len(patch) > 0 {
		if _, err := service.Settings.UpdateResource(ctx, caller, settings.ScopeProject,
			projectID, SettingTypeAgentHermes, instanceID, patch); err != nil {
			return InstanceResult{}, err
		}
	}
	if configurationChanged {
		item.Status = InstanceSetupPending
		if item.ManagementMode == ManagementAuto {
			item.Status = InstanceConfiguring
		}
	}
	updated, err := service.Store.UpdateInstance(ctx, caller.User.ID, caller.Kind, item, service.now())
	if err != nil {
		return InstanceResult{}, err
	}
	service.enrichSecretStatus(ctx, caller, projectID, &updated)
	result := InstanceResult{Instance: updated}
	if !toolsChanged && !needsAutoConfiguration {
		return result, nil
	}
	grant := *updated.Grant
	if toolsChanged {
		grant.AllowedTools = requestedTools
	}
	oldTokenID, err := service.activeTokenID(ctx, grant.GrantID)
	if err != nil {
		return InstanceResult{}, err
	}
	issued, rotation, err := service.issuePendingToken(ctx, caller, updated, grant,
		oldTokenID, RotateTokenInput{})
	if err != nil {
		return InstanceResult{}, err
	}
	result.Rotation = &rotation
	if updated.ManagementMode == ManagementManual {
		result.OneTimeToken = &OneTimeTokenMaterial{
			AllowedTools: append([]string(nil), requestedTools...),
			GatewayURL:   verificationEndpoint(service.GatewayURL, issued.Challenge),
			Token:        issued.Secret, TokenID: issued.Token.ID,
		}
		return result, nil
	}
	adapter, err := service.adapterFor(ctx, projectID, updated)
	if err != nil {
		_, _ = service.Store.UpdateRotation(ctx, rotation.RotationID, "failed",
			"adapter_unavailable", service.now())
		return result, err
	}
	if err := service.configureAndActivate(ctx, caller, &updated, grant, issued,
		rotation, adapter); err != nil {
		result.Instance = updated
		return result, err
	}
	if completed, findErr := service.Store.FindRotationByToken(
		ctx, grant.GrantID, issued.Token.ID,
	); findErr == nil {
		result.Rotation = &completed
	}
	result.Instance, err = service.GetInstance(ctx, caller, projectID, instanceID)
	return result, err
}

func (service Service) DisableInstance(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
) error {
	if err := requireHumanSession(caller); err != nil {
		return err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentManage); err != nil {
		return err
	}
	grant, err := service.Store.GetGrant(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	tokens, err := service.Auth.ListAgentTokens(ctx, grant.GrantID)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if token.RevokedAt == nil {
			if err := service.Auth.RevokeAgentToken(ctx, caller, projectID, token.ID); err != nil {
				return err
			}
		}
	}
	return service.Store.DisableInstance(ctx, caller.User.ID, caller.Kind,
		projectID, instanceID, service.now())
}

func (service Service) CheckConnections(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	scope string,
) (Instance, error) {
	if err := requireHumanSession(caller); err != nil {
		return Instance{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentManage); err != nil {
		return Instance{}, err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return Instance{}, err
	}
	adapter, err := service.adapterFor(ctx, projectID, item)
	if err != nil {
		return Instance{}, err
	}
	scope = strings.TrimSpace(scope)
	if scope != "runtime" && scope != "management" &&
		scope != "project_access" && scope != "all" {
		return Instance{}, ErrInvalid
	}
	now := service.now()
	runtimeCheck := item.RuntimeCheck
	managementCheck := item.ManagementCheck
	accessCheck := item.ProjectAccessCheck
	path := item.ManagementPath
	status := item.Status
	capabilities := item.Capabilities
	if scope == "runtime" || scope == "all" {
		runtimeCheck = CheckSnapshot{CheckedAt: now, Status: "failed"}
		probe, probeErr := adapter.Probe(ctx)
		if probeErr == nil && probe.Healthy && probe.Authenticated {
			capabilities = capabilityMap(probe.Capabilities)
			if !probe.Capabilities.Sessions || !probe.Capabilities.Runs ||
				!probe.Capabilities.RunStreaming || !probe.Capabilities.RunStop {
				probeErr = &AdapterError{Code: ErrorUnsupported, Operation: "runtime.capability_check"}
			} else {
				probeErr = adapter.CheckRuntime(ctx)
			}
		}
		if probeErr == nil && probe.Healthy && probe.Authenticated {
			runtimeCheck.Status = "passed"
		} else {
			runtimeCheck.Code = safeAdapterCode(probeErr, "runtime_failed")
		}
		service.observeCheck("runtime", runtimeCheck.Status)
	}
	if scope == "management" || scope == "project_access" || scope == "all" {
		managementCheck = CheckSnapshot{CheckedAt: now, Status: "unsupported"}
		accessCheck = CheckSnapshot{CheckedAt: now, Status: "failed"}
	}
	if item.ManagementMode == ManagementAuto &&
		(scope == "management" || scope == "project_access" || scope == "all") {
		access, accessErr := adapter.VerifyProjectAccess(ctx, ProjectAccessRequest{
			BindingID: item.Grant.GrantID, Endpoint: service.GatewayURL,
			ExpectedTools:   item.Grant.AllowedTools,
			CurrentRemoteID: item.Grant.RemoteAccessID,
		})
		if accessErr == nil && access.Verified &&
			sameTools(access.Tools, item.Grant.AllowedTools) {
			managementCheck.Status = "passed"
			accessCheck.Status = "passed"
			path = managementPath(access.Route)
			status = InstanceActive
		} else {
			code := safeAdapterCode(accessErr, "project_access_failed")
			managementCheck.Code, accessCheck.Code = code, code
			managementCheck.Status = "failed"
		}
	} else if item.ManagementMode == ManagementManual &&
		(scope == "project_access" || scope == "all") {
		if service.hasVerifiedActiveAgentAccess(ctx, item.Grant.GrantID) {
			accessCheck.Status = "passed"
			status = InstanceActive
		} else {
			accessCheck.Code = "gateway_verification_missing"
		}
	}
	if runtimeCheck.Status == "passed" && accessCheck.Status == "passed" {
		status = InstanceActive
	} else if runtimeCheck.Status == "failed" || accessCheck.Status == "failed" {
		status = InstanceDegraded
	}
	if err := service.Store.SaveChecks(ctx, instanceID, runtimeCheck,
		managementCheck, accessCheck, path, capabilities, status, now); err != nil {
		return Instance{}, err
	}
	if scope == "management" || scope == "all" {
		service.observeCheck("management", managementCheck.Status)
	}
	if scope == "project_access" || scope == "all" {
		service.observeCheck("project_access", accessCheck.Status)
	}
	service.recordAudit(ctx, caller, "agent.connection.check", "agent-instance",
		instanceID, projectID, map[string]interface{}{
			"management_status":     managementCheck.Status,
			"project_access_status": accessCheck.Status,
			"runtime_status":        runtimeCheck.Status,
			"scope":                 scope,
		})
	return service.GetInstance(ctx, caller, projectID, instanceID)
}

func (service Service) RotateToken(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	input RotateTokenInput,
) (InstanceResult, error) {
	if err := requireHumanSession(caller); err != nil {
		return InstanceResult{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentTokensManage); err != nil {
		return InstanceResult{}, err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return InstanceResult{}, err
	}
	service.enrichSecretStatus(ctx, caller, projectID, &item)
	oldTokenID, err := service.activeTokenID(ctx, item.Grant.GrantID)
	if err != nil {
		return InstanceResult{}, err
	}
	issued, rotation, err := service.issuePendingToken(ctx, caller, item, *item.Grant, oldTokenID, input)
	if err != nil {
		return InstanceResult{}, err
	}
	result := InstanceResult{Instance: item, Rotation: &rotation}
	if item.ManagementMode == ManagementManual {
		result.OneTimeToken = &OneTimeTokenMaterial{
			AllowedTools: append([]string(nil), item.Grant.AllowedTools...),
			GatewayURL:   verificationEndpoint(service.GatewayURL, issued.Challenge), Token: issued.Secret,
			TokenID: issued.Token.ID,
		}
		return result, nil
	}
	adapter, err := service.adapterFor(ctx, projectID, item)
	if err != nil {
		_, _ = service.Store.UpdateRotation(ctx, rotation.RotationID, "failed",
			"adapter_unavailable", service.now())
		return result, err
	}
	if err := service.configureAndActivate(ctx, caller, &item, *item.Grant,
		issued, rotation, adapter); err != nil {
		result.Instance = item
		return result, err
	}
	if completed, findErr := service.Store.FindRotationByToken(
		ctx, item.Grant.GrantID, issued.Token.ID,
	); findErr == nil {
		result.Rotation = &completed
	}
	result.Instance = item
	return result, nil
}

func (service Service) VerifyToken(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	tokenID string,
) (Instance, error) {
	if err := requireHumanSession(caller); err != nil {
		return Instance{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentTokensManage); err != nil {
		return Instance{}, err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return Instance{}, err
	}
	token, err := service.Auth.GetAgentToken(ctx, tokenID)
	if err != nil || token.AgentInstanceID != instanceID || token.ProjectID != projectID ||
		token.GrantID != item.Grant.GrantID || token.Status != "pending" {
		return Instance{}, ErrNotFound
	}
	rotation, err := service.Store.FindRotationByToken(ctx, item.Grant.GrantID, tokenID)
	if err != nil {
		return Instance{}, err
	}
	if token.ExpiresAt != nil && !token.ExpiresAt.After(service.now()) {
		_, _ = service.Store.UpdateRotation(ctx, rotation.RotationID,
			"failed", "token_expired", service.now())
		return Instance{}, ErrConflict
	}
	if !hasAgentVerification(token) {
		return Instance{}, ErrConflict
	}
	if _, err := service.Store.UpdateRotation(ctx, rotation.RotationID,
		"verifying", "", service.now()); err != nil {
		return Instance{}, err
	}
	if _, err := service.Auth.ActivateAgentToken(ctx, caller, projectID,
		tokenID, rotation.OldTokenID, ""); err != nil {
		if service.Metrics != nil {
			service.Metrics.ObserveAgentToken("activate", "error")
		}
		_, _ = service.Store.UpdateRotation(ctx, rotation.RotationID,
			"failed", "activation_failed", service.now())
		return Instance{}, err
	}
	if service.Metrics != nil {
		service.Metrics.ObserveAgentToken("activate", "success")
	}
	now := service.now()
	if err := service.Store.SaveChecks(ctx, instanceID, item.RuntimeCheck,
		item.ManagementCheck, CheckSnapshot{CheckedAt: now, Status: "passed"},
		item.ManagementPath, item.Capabilities, InstanceActive, now); err != nil {
		return Instance{}, err
	}
	return service.GetInstance(ctx, caller, projectID, instanceID)
}

func (service Service) AbortToken(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	tokenID string,
) error {
	if err := requireHumanSession(caller); err != nil {
		return err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentTokensManage); err != nil {
		return err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	token, err := service.Auth.GetAgentToken(ctx, tokenID)
	if err != nil || token.GrantID != item.Grant.GrantID || token.Status != "pending" {
		return ErrNotFound
	}
	if err := service.Auth.RevokeAgentToken(ctx, caller, projectID, tokenID); err != nil {
		if service.Metrics != nil {
			service.Metrics.ObserveAgentToken("revoke", "error")
		}
		return err
	}
	if service.Metrics != nil {
		service.Metrics.ObserveAgentToken("revoke", "success")
	}
	rotation, err := service.Store.FindRotationByToken(ctx, item.Grant.GrantID, tokenID)
	if err == nil {
		_, err = service.Store.UpdateRotation(ctx, rotation.RotationID,
			"cancelled", "", service.now())
	}
	return err
}

func (service Service) RevokeToken(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	tokenID string,
) error {
	if err := requireHumanSession(caller); err != nil {
		return err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentTokensManage); err != nil {
		return err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	token, err := service.Auth.GetAgentToken(ctx, tokenID)
	if err != nil || token.GrantID != item.Grant.GrantID {
		return ErrNotFound
	}
	if err := service.Auth.RevokeAgentToken(ctx, caller, projectID, tokenID); err != nil {
		return err
	}
	if token.Status == "active" {
		now := service.now()
		return service.Store.SaveChecks(ctx, instanceID, item.RuntimeCheck,
			item.ManagementCheck, CheckSnapshot{CheckedAt: now, Status: "failed", Code: "token_revoked"},
			item.ManagementPath, item.Capabilities, InstanceDegraded, now)
	}
	return nil
}

func (service Service) GetPrompt(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
) (Prompt, error) {
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentRead); err != nil {
		return Prompt{}, err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return Prompt{}, err
	}
	projectItem, err := service.Projects.Get(ctx, caller, projectID)
	if err != nil {
		return Prompt{}, err
	}
	defaultPrompt := buildDefaultPrompt(projectItem)
	override, updatedAt, version, err := service.Store.GetPromptOverride(ctx, item.Grant.GrantID)
	if err != nil {
		return Prompt{}, err
	}
	effective := defaultPrompt
	if override != "" {
		effective = override
	}
	return Prompt{Default: defaultPrompt, Effective: effective, Override: override,
		UpdatedAt: updatedAt, Version: version}, nil
}

func (service Service) UpdatePrompt(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	override string,
) (Prompt, error) {
	if err := requireHumanSession(caller); err != nil {
		return Prompt{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentManage); err != nil {
		return Prompt{}, err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return Prompt{}, err
	}
	override = strings.TrimSpace(override)
	if override == "" || len(override) > 50000 {
		return Prompt{}, ErrInvalid
	}
	if err := service.Store.UpdatePrompt(ctx, caller.User.ID,
		item.Grant.GrantID, override, service.now()); err != nil {
		return Prompt{}, err
	}
	return service.GetPrompt(ctx, caller, projectID, instanceID)
}

func (service Service) ResetPrompt(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
) (Prompt, error) {
	if err := requireHumanSession(caller); err != nil {
		return Prompt{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentManage); err != nil {
		return Prompt{}, err
	}
	item, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return Prompt{}, err
	}
	if err := service.Store.ResetPrompt(ctx, caller.User.ID,
		item.Grant.GrantID, service.now()); err != nil {
		return Prompt{}, err
	}
	return service.GetPrompt(ctx, caller, projectID, instanceID)
}

func (service Service) ListSessions(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
) ([]SessionRecord, error) {
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentRead); err != nil {
		return nil, err
	}
	return service.Store.ListSessions(ctx, projectID, instanceID)
}

func (service Service) GetSession(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
) (SessionRecord, error) {
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentRead); err != nil {
		return SessionRecord{}, err
	}
	return service.Store.GetSession(ctx, projectID, instanceID, sessionID)
}

func (service Service) CreateSession(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	input CreateSessionInput,
) (SessionRecord, error) {
	if err := requireHumanSession(caller); err != nil {
		return SessionRecord{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return SessionRecord{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.SessionType = strings.TrimSpace(input.SessionType)
	// Human-facing Agent conversations are always main Sessions. Progress and
	// experiment Sessions are reserved for their owning internal workflows,
	// which create them through dedicated in-process services.
	if input.Title == "" || input.SessionType != "main" {
		return SessionRecord{}, ErrInvalid
	}
	instance, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil || instance.Status != InstanceActive {
		return SessionRecord{}, ErrNotConfigured
	}
	prompt, err := service.GetPrompt(ctx, caller, projectID, instanceID)
	if err != nil {
		return SessionRecord{}, err
	}
	adapter, err := service.adapterFor(ctx, projectID, instance)
	if err != nil {
		return SessionRecord{}, err
	}
	remoteCandidate, err := service.Generator.New()
	if err != nil {
		return SessionRecord{}, err
	}
	remote, err := adapter.CreateSession(ctx, CreateSessionRequest{
		RemoteID: remoteCandidate, Source: "mmdash", Title: input.Title,
		SystemPrompt: prompt.Effective,
	})
	if err != nil {
		return SessionRecord{}, mapAdapterError(err)
	}
	sessionID, err := service.Generator.New()
	if err != nil {
		return SessionRecord{}, err
	}
	now := service.now()
	item := SessionRecord{
		AgentInstanceID: instanceID, CreatedAt: now, CreatedBy: caller.User.ID,
		GrantID: instance.Grant.GrantID, ID: sessionID, ProjectID: projectID,
		RemoteSessionID: remote.RemoteID, SessionType: input.SessionType,
		Status: SessionActive, Title: input.Title, UpdatedAt: now, Version: 1,
	}
	return service.Store.CreateSession(ctx, caller.User.ID, item, "agent.session.created")
}

func (service Service) RenameSession(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	title string,
) (SessionRecord, error) {
	if err := requireHumanSession(caller); err != nil {
		return SessionRecord{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return SessionRecord{}, err
	}
	item, adapter, err := service.sessionAdapter(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return SessionRecord{}, ErrInvalid
	}
	if _, err := adapter.UpdateSession(ctx, item.RemoteSessionID,
		UpdateSessionRequest{Title: &title}); err != nil {
		return SessionRecord{}, mapAdapterError(err)
	}
	item.Title = title
	return service.Store.UpdateSession(ctx, caller.User.ID, item,
		"agent.session.renamed", service.now())
}

func (service Service) EndSession(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	reason string,
) (SessionRecord, error) {
	if err := requireHumanSession(caller); err != nil {
		return SessionRecord{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return SessionRecord{}, err
	}
	item, adapter, err := service.sessionAdapter(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "ended_by_user"
	}
	if _, err := adapter.UpdateSession(ctx, item.RemoteSessionID,
		UpdateSessionRequest{EndReason: &reason}); err != nil {
		return SessionRecord{}, mapAdapterError(err)
	}
	now := service.now()
	item.Status, item.EndReason, item.EndedAt = SessionEnded, reason, &now
	return service.Store.UpdateSession(ctx, caller.User.ID, item,
		"agent.session.ended", now)
}

func (service Service) ContinueSession(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
) (SessionRecord, error) {
	if err := requireHumanSession(caller); err != nil {
		return SessionRecord{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return SessionRecord{}, err
	}
	item, err := service.Store.GetSession(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	item.Status, item.EndReason, item.EndedAt = SessionActive, "", nil
	return service.Store.UpdateSession(ctx, caller.User.ID, item,
		"agent.session.continued", service.now())
}

func (service Service) ForkSession(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	title string,
) (SessionRecord, error) {
	if err := requireHumanSession(caller); err != nil {
		return SessionRecord{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return SessionRecord{}, err
	}
	parent, adapter, err := service.sessionAdapter(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = parent.Title + " (fork)"
	}
	remoteID, err := service.Generator.New()
	if err != nil {
		return SessionRecord{}, err
	}
	remote, err := adapter.ForkSession(ctx, parent.RemoteSessionID,
		ForkSessionRequest{RemoteID: remoteID, Title: title})
	if err != nil {
		return SessionRecord{}, mapAdapterError(err)
	}
	now := service.now()
	parent.Status = SessionEnded
	parent.EndReason = "branched"
	parent.EndedAt = &now
	if _, err := service.Store.UpdateSession(ctx, caller.User.ID, parent,
		"agent.session.ended", now); err != nil {
		return SessionRecord{}, err
	}
	localID, err := service.Generator.New()
	if err != nil {
		return SessionRecord{}, err
	}
	item := SessionRecord{
		AgentInstanceID: instanceID, CreatedAt: now, CreatedBy: caller.User.ID,
		GrantID: parent.GrantID, ID: localID, ParentSessionID: parent.ID,
		ProjectID: projectID, RemoteSessionID: remote.RemoteID,
		SessionType: parent.SessionType, Status: SessionActive, Title: title,
		UpdatedAt: now, Version: 1,
	}
	return service.Store.CreateSession(ctx, caller.User.ID, item, "agent.session.forked")
}

func (service Service) SetDefaultSession(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
) error {
	if err := requireHumanSession(caller); err != nil {
		return err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return err
	}
	return service.Store.SetDefaultSession(ctx, caller.User.ID, projectID,
		instanceID, sessionID, service.now())
}

func (service Service) ListMessages(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
) ([]Message, error) {
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentRead); err != nil {
		return nil, err
	}
	item, adapter, err := service.sessionAdapter(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	messages, err := adapter.ListMessages(ctx, item.RemoteSessionID)
	if err != nil {
		return nil, mapAdapterError(err)
	}
	if messages == nil {
		messages = []Message{}
	}
	if service.Artifacts != nil {
		runs, listErr := service.Store.ListRuns(ctx, sessionID)
		if listErr != nil {
			return nil, listErr
		}
		runIDs := make([]string, 0, len(runs))
		for _, run := range runs {
			runIDs = append(runIDs, run.ID)
		}
		attachments, listErr := service.Artifacts.ListAgentRunAttachments(
			ctx, caller, projectID, runIDs,
		)
		if listErr != nil {
			return nil, listErr
		}
		messages = append(messages, attachmentMessages(attachments, runs)...)
		sort.SliceStable(messages, func(left, right int) bool {
			if messages[left].Timestamp == nil {
				return false
			}
			if messages[right].Timestamp == nil {
				return true
			}
			return messages[left].Timestamp.Before(*messages[right].Timestamp)
		})
	}
	return messages, nil
}

func (service Service) StartRun(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	input StartRunInput,
) (RunRecord, error) {
	return service.startRun(ctx, caller, projectID, instanceID, sessionID,
		input, "message", "")
}

func (service Service) GetRun(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	runID string,
) (RunRecord, error) {
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentRead); err != nil {
		return RunRecord{}, err
	}
	item, adapter, err := service.runAdapter(ctx, projectID, instanceID, sessionID, runID)
	if err != nil {
		return RunRecord{}, err
	}
	if terminalRunStatus(item.Status) {
		return item, nil
	}
	remote, err := adapter.GetRun(ctx, item.RemoteRunID)
	if err != nil {
		return RunRecord{}, mapAdapterError(err)
	}
	status := normalizeRunStatus(remote.Status)
	code := ""
	if remote.Error != nil {
		code = remote.Error.Code
	}
	if status != "" && (status != item.Status || code != item.SafeErrorCode) {
		item, err = service.Store.UpdateRun(ctx, runID, status, code, service.now())
		if err != nil {
			return RunRecord{}, err
		}
		service.observeRun(status)
	}
	return item, nil
}

func (service Service) StopRun(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	runID string,
) (RunRecord, error) {
	if err := requireHumanSession(caller); err != nil {
		return RunRecord{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return RunRecord{}, err
	}
	item, adapter, err := service.runAdapter(ctx, projectID, instanceID, sessionID, runID)
	if err != nil {
		return RunRecord{}, err
	}
	remote, err := adapter.StopRun(ctx, item.RemoteRunID)
	if err != nil {
		return RunRecord{}, mapAdapterError(err)
	}
	status := normalizeRunStatus(remote.Status)
	if status == "" || status == RunRecordRunning {
		status = RunRecordStopping
	}
	updated, err := service.Store.UpdateRun(ctx, runID, status, "", service.now())
	if err != nil {
		return RunRecord{}, err
	}
	service.recordAudit(ctx, caller, "agent.run.stop.request", "agent-run", runID,
		projectID, map[string]interface{}{"status": updated.Status})
	return updated, nil
}

func (service Service) ReplayRun(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	runID string,
	messageID string,
	regenerate bool,
) (ReplayResult, error) {
	if err := requireHumanSession(caller); err != nil {
		return ReplayResult{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return ReplayResult{}, err
	}
	if _, err := service.Store.GetRun(ctx, sessionID, runID); err != nil {
		return ReplayResult{}, err
	}
	messages, err := service.ListMessages(ctx, caller, projectID, instanceID, sessionID)
	if err != nil {
		return ReplayResult{}, err
	}
	messageID = strings.TrimSpace(messageID)
	lastUser := ""
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" && strings.TrimSpace(messages[index].Content) != "" &&
			(messageID == "" || messages[index].RemoteID == messageID) {
			lastUser = messages[index].Content
			break
		}
	}
	if lastUser == "" {
		return ReplayResult{}, ErrConflict
	}
	target, err := service.Store.GetSession(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return ReplayResult{}, err
	}
	source := "rerun"
	if regenerate {
		target, err = service.ForkSession(ctx, caller, projectID, instanceID,
			sessionID, target.Title+" (regenerated)")
		if err != nil {
			return ReplayResult{}, err
		}
		source = "regenerate"
	}
	run, err := service.startRun(ctx, caller, projectID, instanceID, target.ID,
		StartRunInput{Input: lastUser}, source, runID)
	if err != nil {
		return ReplayResult{}, err
	}
	return ReplayResult{Run: run, Session: target}, nil
}

func (service Service) ApproveRun(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	runID string,
	approvalID string,
	choice ApprovalChoice,
) (RunRecord, error) {
	if err := requireHumanSession(caller); err != nil {
		return RunRecord{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return RunRecord{}, err
	}
	approvalID = strings.TrimSpace(approvalID)
	if !validApprovalID(approvalID) || !validApprovalChoice(choice) {
		return RunRecord{}, ErrInvalid
	}
	item, adapter, err := service.runAdapter(ctx, projectID, instanceID, sessionID, runID)
	if err != nil {
		return RunRecord{}, err
	}
	if item.Status != RunRecordWaitingForApproval {
		return RunRecord{}, ErrConflict
	}
	claimID, err := service.Generator.New()
	if err != nil {
		return RunRecord{}, err
	}
	if _, err := service.Store.ClaimRunApproval(
		ctx, runID, approvalID, claimID, service.now(),
	); err != nil {
		return RunRecord{}, err
	}
	result, err := adapter.ApproveRun(ctx, item.RemoteRunID,
		ApprovalRequest{RemoteID: approvalID, Choice: choice})
	if err != nil {
		_, _ = service.Store.ReleaseRunApprovalClaim(
			context.WithoutCancel(ctx), runID, approvalID, claimID, service.now(),
		)
		return RunRecord{}, mapAdapterError(err)
	}
	if result.Resolved < 1 ||
		(result.RemoteID != "" && result.RemoteID != approvalID) ||
		(result.RunRemoteID != "" && result.RunRemoteID != item.RemoteRunID) ||
		(result.Choice != "" && result.Choice != choice) {
		_, _ = service.Store.ReleaseRunApprovalClaim(
			context.WithoutCancel(ctx), runID, approvalID, claimID, service.now(),
		)
		return RunRecord{}, ErrRuntime
	}
	updated, err := service.Store.CompleteRunApproval(
		ctx, runID, approvalID, claimID, service.now(),
	)
	if err != nil {
		return RunRecord{}, err
	}
	service.recordAudit(ctx, caller, "agent.run.approve", "agent-run", runID,
		projectID, map[string]interface{}{
			"approval_id": approvalID,
			"choice":      choice,
		})
	return updated, nil
}

func (service Service) StreamRun(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	runID string,
	lastEventID string,
	handler EventHandler,
) error {
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentRead); err != nil {
		return err
	}
	if handler == nil {
		return ErrInvalid
	}
	item, adapter, err := service.runAdapter(ctx, projectID, instanceID, sessionID, runID)
	if err != nil {
		return err
	}
	if service.Metrics != nil {
		service.Metrics.AddAgentStream(1)
		defer service.Metrics.AddAgentStream(-1)
	}
	return adapter.StreamRun(ctx, item.RemoteRunID,
		StreamOptions{LastEventID: lastEventID}, func(eventContext context.Context, event Event) error {
			now := service.now()
			if event.Approval != nil {
				approvalID := strings.TrimSpace(event.Approval.RemoteID)
				switch event.Type {
				case EventApprovalRequested:
					if !validApprovalID(approvalID) {
						return ErrRuntime
					}
					event.Approval.RemoteID = approvalID
					updated, persistErr := service.Store.RecordRunApproval(
						eventContext, runID, approvalID, now,
					)
					if errors.Is(persistErr, ErrConflict) {
						return nil
					}
					if persistErr != nil {
						return persistErr
					}
					if !containsApprovalID(updated.PendingApprovalIDs, approvalID) {
						return nil
					}
				case EventApprovalResponded:
					var persistErr error
					if approvalID == "" {
						_, approvalID, persistErr = service.Store.ApplyNextRunApprovalResponse(
							eventContext, runID, now,
						)
					} else if validApprovalID(approvalID) {
						_, persistErr = service.Store.ApplyRunApprovalResponse(
							eventContext, runID, approvalID, now,
						)
					} else {
						return ErrRuntime
					}
					if persistErr != nil {
						if errors.Is(persistErr, ErrConflict) {
							return nil
						}
						return persistErr
					}
					event.Approval.RemoteID = approvalID
				}
			}
			if event.Tool != nil && event.Tool.RemoteID != "" {
				callID, generateErr := service.Generator.New()
				if generateErr != nil {
					return generateErr
				}
				status := normalizeToolStatus(event.Type, event.Tool.Status)
				call := ToolCallRecord{
					ID: callID, RunID: runID, RemoteToolCallID: event.Tool.RemoteID,
					ToolName: event.Tool.Name, Status: status,
					StartedAt: now, UpdatedAt: now,
				}
				if status == "completed" || status == "failed" {
					call.CompletedAt = &now
				}
				if _, persistErr := service.Store.UpsertToolCall(eventContext, call); persistErr != nil {
					return persistErr
				}
			}
			if status := eventRunStatus(event.Type); status != "" {
				code := ""
				if event.Error != nil {
					code = event.Error.Code
				}
				if _, persistErr := service.Store.UpdateRun(eventContext, runID, status, code, now); persistErr != nil {
					return persistErr
				}
				service.observeRun(status)
			}
			return handler(eventContext, event)
		})
}

func (service Service) startRun(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	input StartRunInput,
	source string,
	sourceRunID string,
) (RunRecord, error) {
	if err := requireHumanSession(caller); err != nil {
		return RunRecord{}, err
	}
	if err := service.Projects.Authorize(ctx, caller, projectID, project.PermissionAgentUse); err != nil {
		return RunRecord{}, err
	}
	input.Input = strings.TrimSpace(input.Input)
	if input.Input == "" || len(input.Input) > 200000 {
		return RunRecord{}, ErrInvalid
	}
	input.ReasoningEffort = strings.TrimSpace(input.ReasoningEffort)
	if !validReasoningEffort(input.ReasoningEffort) {
		return RunRecord{}, ErrInvalid
	}
	session, adapter, err := service.sessionAdapter(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return RunRecord{}, err
	}
	if session.Status != SessionActive {
		return RunRecord{}, ErrConflict
	}
	remoteMessages, err := adapter.ListMessages(ctx, session.RemoteSessionID)
	if err != nil {
		return RunRecord{}, mapAdapterError(err)
	}
	history := conversationHistory(remoteMessages)
	previousAttachments, err := service.sessionAttachments(ctx, caller, projectID, sessionID)
	if err != nil {
		return RunRecord{}, err
	}
	localID, err := service.Generator.New()
	if err != nil {
		return RunRecord{}, err
	}
	now := service.now()
	reserved := RunRecord{
		CreatedAt: now, CreatedBy: caller.User.ID, ID: localID,
		RemoteRunID: "pending:" + localID, SessionID: sessionID, Source: source,
		SourceRunID: sourceRunID, Status: RunRecordQueued,
		ToolCalls: []ToolCallRecord{}, UpdatedAt: now, Version: 1,
	}
	if _, err := service.Store.ReserveRun(ctx, reserved); err != nil {
		return RunRecord{}, err
	}
	attachments := []artifact.ChatAttachment{}
	if len(input.ArtifactIDs) > 0 {
		if service.Artifacts == nil {
			_ = service.Store.FailRunReservation(ctx, caller.User.ID, localID,
				"artifact_service_unavailable", service.now())
			return RunRecord{}, ErrNotConfigured
		}
		attachments, err = service.Artifacts.AttachAgentRunInputs(
			ctx, caller, projectID, localID, input.ArtifactIDs,
		)
		if err != nil {
			_ = service.Store.FailRunReservation(ctx, caller.User.ID, localID,
				"artifact_attachment_failed", service.now())
			return RunRecord{}, err
		}
	}
	instructions := runInstructions(strings.TrimSpace(input.Instructions),
		projectID, sessionID, localID, attachments, previousAttachments)
	remote, err := adapter.StartRun(ctx, StartRunRequest{
		Input: input.Input, Instructions: instructions,
		SessionRemoteID:     session.RemoteSessionID,
		ConversationHistory: history,
		ReasoningEffort:     input.ReasoningEffort,
	})
	if err != nil {
		code := safeAdapterCode(err, "runtime_failed")
		_ = service.Store.FailRunReservation(ctx, caller.User.ID, localID, code, service.now())
		return RunRecord{}, mapAdapterError(err)
	}
	started := service.now()
	item := RunRecord{
		CreatedAt: now, CreatedBy: caller.User.ID, ID: localID,
		RemoteRunID: remote.RemoteID, SessionID: sessionID, Source: source,
		SourceRunID: sourceRunID, StartedAt: &started,
		Status: normalizeRunStatus(remote.Status), ToolCalls: []ToolCallRecord{},
		UpdatedAt: now, Version: 1,
	}
	if item.Status == "" || item.Status == RunRecordQueued {
		item.Status = RunRecordRunning
	}
	service.observeRun(item.Status)
	activated, err := service.Store.ActivateRun(ctx, caller.User.ID, item, started)
	if err != nil {
		_, _ = adapter.StopRun(ctx, remote.RemoteID)
		_ = service.Store.FailRunReservation(ctx, caller.User.ID, localID,
			"persistence_failed", service.now())
		return RunRecord{}, err
	}
	return activated, nil
}

func (service Service) sessionAdapter(
	ctx context.Context,
	projectID string,
	instanceID string,
	sessionID string,
) (SessionRecord, Adapter, error) {
	item, err := service.Store.GetSession(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return SessionRecord{}, nil, err
	}
	instance, err := service.Store.GetInstance(ctx, projectID, instanceID)
	if err != nil {
		return SessionRecord{}, nil, err
	}
	adapter, err := service.adapterFor(ctx, projectID, instance)
	return item, adapter, err
}

func (service Service) runAdapter(
	ctx context.Context,
	projectID string,
	instanceID string,
	sessionID string,
	runID string,
) (RunRecord, Adapter, error) {
	item, err := service.Store.GetRun(ctx, sessionID, runID)
	if err != nil {
		return RunRecord{}, nil, err
	}
	_, adapter, err := service.sessionAdapter(ctx, projectID, instanceID, sessionID)
	return item, adapter, err
}

func (service Service) adapterFor(
	ctx context.Context,
	projectID string,
	instance Instance,
) (Adapter, error) {
	resolved, err := service.Settings.ResolveResource(ctx, settings.ScopeProject,
		projectID, SettingTypeAgentHermes, instance.ID)
	if err != nil {
		return nil, err
	}
	values := map[string]string{
		"runtime_url":                     instance.RuntimeURL,
		"api_key":                         stringValue(resolved.Values[settingAPIKey]),
		"profile":                         stringValue(resolved.Values[settingProfile]),
		"management_url":                  instance.DashboardURL,
		"dashboard_session_token":         stringValue(resolved.Values[settingDashboardToken]),
		"cloudflare_access_client_id":     stringValue(resolved.Values[settingCFClientID]),
		"cloudflare_access_client_secret": stringValue(resolved.Values[settingCFClientSecret]),
		"request_timeout_seconds":         stringValue(resolved.Values[settingRequestTimeout]),
	}
	return service.Adapters.New(ctx, instance.AdapterType,
		AdapterConfig{InstanceID: instance.ID, Values: values})
}

func (service Service) issuePendingToken(
	ctx context.Context,
	caller auth.Identity,
	instance Instance,
	grant ProjectGrant,
	oldTokenID string,
	input RotateTokenInput,
) (auth.IssuedAgentToken, TokenRotation, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = instance.DisplayName + " MCP"
	}
	issued, err := service.Auth.IssueAgentToken(ctx, caller, auth.IssueAgentTokenInput{
		AgentInstanceID: instance.ID, AllowedTools: grant.AllowedTools,
		ExpiresAt: input.ExpiresAt, GrantID: grant.GrantID, Name: name,
		ProjectID: grant.ProjectID, ReplacesTokenID: oldTokenID,
	})
	if err != nil {
		if service.Metrics != nil {
			service.Metrics.ObserveAgentToken("issue", "error")
		}
		return auth.IssuedAgentToken{}, TokenRotation{}, err
	}
	if service.Metrics != nil {
		service.Metrics.ObserveAgentToken("issue", "success")
	}
	rotationID, err := service.Generator.New()
	if err != nil {
		_ = service.Auth.RevokeAgentToken(ctx, caller, grant.ProjectID, issued.Token.ID)
		return auth.IssuedAgentToken{}, TokenRotation{}, err
	}
	now := service.now()
	status := "awaiting_user"
	if instance.ManagementMode == ManagementAuto {
		status = "configuring"
	}
	rotation := TokenRotation{
		CreatedAt: now, CreatedBy: caller.User.ID, GrantID: grant.GrantID,
		ManagementMode: instance.ManagementMode, NewTokenID: issued.Token.ID,
		OldTokenID: oldTokenID, RotationID: rotationID, Status: status,
		UpdatedAt: now,
	}
	rotation, err = service.Store.CreateRotation(ctx, caller.User.ID, rotation)
	return issued, rotation, err
}

func (service Service) configureAndActivate(
	ctx context.Context,
	caller auth.Identity,
	instance *Instance,
	grant ProjectGrant,
	issued auth.IssuedAgentToken,
	rotation TokenRotation,
	adapter Adapter,
) error {
	now := service.now()
	request := ProjectAccessRequest{
		BindingID: rotation.RotationID, Credential: issued.Secret,
		Endpoint:        verificationEndpoint(service.GatewayURL, issued.Challenge),
		ExpectedTools:   grant.AllowedTools,
		CurrentRemoteID: grant.RemoteAccessID,
	}
	var access ProjectAccessResult
	var err error
	if strings.TrimSpace(grant.RemoteAccessID) != "" {
		access, err = adapter.RotateProjectAccess(ctx, request)
	} else {
		access, err = adapter.ConfigureProjectAccess(ctx, request)
	}
	if err != nil || !access.Verified || !sameTools(access.Tools, grant.AllowedTools) {
		code := safeAdapterCode(err, "project_access_failed")
		_, _ = service.Store.UpdateRotation(ctx, rotation.RotationID, "failed", code, now)
		_ = service.Store.SaveChecks(ctx, instance.ID, instance.RuntimeCheck,
			CheckSnapshot{CheckedAt: now, Status: "failed", Code: code},
			CheckSnapshot{CheckedAt: now, Status: "failed", Code: code},
			instance.ManagementPath, instance.Capabilities, InstanceDegraded, now)
		instance.Status = InstanceDegraded
		return mapAdapterErrorOr(err, code)
	}
	if _, err := service.Store.UpdateRotation(ctx, rotation.RotationID,
		"verifying", "", now); err != nil {
		return err
	}
	token, err := service.Auth.GetAgentToken(ctx, issued.Token.ID)
	if err != nil || !hasAgentVerification(token) {
		code := "gateway_verification_missing"
		_, _ = service.Store.UpdateRotation(ctx, rotation.RotationID, "failed", code, now)
		instance.Status = InstanceDegraded
		return ErrConflict
	}
	if _, err := service.Auth.ActivateAgentToken(ctx, caller, grant.ProjectID,
		issued.Token.ID, rotation.OldTokenID, access.RemoteID); err != nil {
		if service.Metrics != nil {
			service.Metrics.ObserveAgentToken("activate", "error")
		}
		_, _ = service.Store.UpdateRotation(ctx, rotation.RotationID,
			"failed", "activation_failed", now)
		return err
	}
	if service.Metrics != nil {
		service.Metrics.ObserveAgentToken("activate", "success")
	}
	// Auth activates the replacement Token, revokes the old Token, completes
	// the rotation, and persists access.RemoteID through AgentCredentialLifecycle
	// in one transaction. Finalization is therefore safe only after this call.
	postActivationCtx := context.WithoutCancel(ctx)
	cleanupPending := false
	if previousRemoteID := strings.TrimSpace(grant.RemoteAccessID); previousRemoteID != "" && previousRemoteID != access.RemoteID {
		finalized, finalizeErr := adapter.FinalizeProjectAccess(
			postActivationCtx,
			ProjectAccessFinalizeRequest{
				ActiveRemoteID: access.RemoteID, PreviousRemoteID: previousRemoteID,
			},
		)
		cleanupPending = finalizeErr != nil || finalized.CleanupPending
		service.observe("project_access", finalizeErr)
		if cleanupPending {
			_, _ = service.Store.UpdateRotation(postActivationCtx, rotation.RotationID,
				"completed", projectAccessCleanupPending, now)
			service.recordAudit(postActivationCtx, caller, "agent.project_access.cleanup",
				"agent-instance", instance.ID, grant.ProjectID, map[string]interface{}{
					"safe_error_code": projectAccessCleanupPending,
					"status":          "pending",
				})
		}
	}
	path := managementPath(access.Route)
	projectAccessCheck := CheckSnapshot{CheckedAt: now, Status: "passed"}
	if cleanupPending {
		projectAccessCheck.Code = projectAccessCleanupPending
	}
	if err := service.Store.SaveChecks(postActivationCtx, instance.ID, instance.RuntimeCheck,
		CheckSnapshot{CheckedAt: now, Status: "passed"},
		projectAccessCheck, path,
		instance.Capabilities, InstanceActive, now); err != nil {
		return err
	}
	instance.Status = InstanceActive
	instance.ManagementPath = path
	instance.ManagementCheck = CheckSnapshot{CheckedAt: now, Status: "passed"}
	instance.ProjectAccessCheck = projectAccessCheck
	return nil
}

func (service Service) enrichSecretStatus(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	instance *Instance,
) {
	setting, err := service.Settings.GetResource(ctx, caller, settings.ScopeProject,
		projectID, SettingTypeAgentHermes, instance.ID)
	if err != nil {
		return
	}
	configured := func(key string) bool {
		value, ok := setting.Values[key].(string)
		return ok && value == settings.RedactedSecret
	}
	instance.SecretStatus = SecretStatus{
		HermesAPIKeyConfigured:           configured(settingAPIKey),
		DashboardTokenConfigured:         configured(settingDashboardToken),
		CloudflareClientIDConfigured:     configured(settingCFClientID),
		CloudflareClientSecretConfigured: configured(settingCFClientSecret),
	}
	instance.Profile = stringValue(setting.Values[settingProfile])
	if timeout, parseErr := strconv.Atoi(stringValue(setting.Values[settingRequestTimeout])); parseErr == nil && timeout >= 1 && timeout <= 300 {
		instance.RequestTimeoutSeconds = timeout
	}
}

func (service Service) hasVerifiedActiveAgentAccess(ctx context.Context, grantID string) bool {
	tokens, err := service.Auth.ListAgentTokens(ctx, grantID)
	if err != nil {
		return false
	}
	now := service.now()
	for _, token := range tokens {
		if token.Status == "active" && token.RevokedAt == nil &&
			(token.ExpiresAt == nil || token.ExpiresAt.After(now)) &&
			hasAgentVerification(token) {
			return true
		}
	}
	return false
}

func (service Service) activeTokenID(ctx context.Context, grantID string) (string, error) {
	tokens, err := service.Auth.ListAgentTokens(ctx, grantID)
	if err != nil {
		return "", err
	}
	now := service.now()
	for _, token := range tokens {
		if token.Status == "active" && token.RevokedAt == nil &&
			(token.ExpiresAt == nil || token.ExpiresAt.After(now)) {
			return token.ID, nil
		}
	}
	return "", nil
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock.Now().UTC()
}

func requireHumanSession(caller auth.Identity) error {
	if caller.Kind != "session" || caller.User.ID == "" {
		return ErrForbidden
	}
	return nil
}

func validOrigin(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.User == nil && parsed.Host != "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}

func originChanged(previous, next string) bool {
	return normalizedOrigin(previous) != normalizedOrigin(next)
}

func normalizedOrigin(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		// The caller validates new values separately. Returning the raw value is
		// conservative for legacy data because any textual change then requires
		// an explicit credential decision.
		return value
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	port := parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return strings.ToLower(parsed.Scheme) + "\x00" + host + "\x00" + port
}

func explicitSecretUpdate(value *string) bool {
	return value != nil && *value != settings.RedactedSecret
}

func managementPath(route AccessRoute) string {
	switch route {
	case AccessRouteAuthenticatedProxy:
		return "cloudflare_access"
	case AccessRouteDirect:
		return "direct"
	default:
		return "unreachable"
	}
}

func validManagementMode(value string) bool {
	return value == ManagementManual || value == ManagementAuto
}

func validSessionType(value string) bool {
	return value == SessionMain || value == SessionProgress || value == SessionExperiment
}

func validReasoningEffort(value string) bool {
	switch value {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		return true
	default:
		return false
	}
}

func validApprovalChoice(value ApprovalChoice) bool {
	return value == ApprovalOnce || value == ApprovalSession ||
		value == ApprovalAlways || value == ApprovalDeny
}

func normalizeTools(values []string) ([]string, error) {
	if len(values) == 0 {
		values = DefaultAllowedTools
	}
	allowed := map[string]bool{}
	for _, tool := range DefaultAllowedTools {
		allowed[tool] = true
	}
	seen := map[string]bool{}
	result := []string{}
	for _, tool := range values {
		tool = strings.TrimSpace(tool)
		if !allowed[tool] || seen[tool] {
			return nil, ErrInvalid
		}
		seen[tool] = true
		result = append(result, tool)
	}
	sort.Strings(result)
	return result, nil
}

func settingsPatch(input CreateInstanceInput) map[string]interface{} {
	patch := map[string]interface{}{
		settingAPIKey:         input.APIKey,
		settingProfile:        input.Profile,
		settingRequestTimeout: float64(input.RequestTimeoutSeconds),
	}
	if input.RequestTimeoutSeconds <= 0 {
		patch[settingRequestTimeout] = float64(30)
	}
	if input.ManagementMode == ManagementAuto {
		patch[settingDashboardToken] = input.DashboardToken
		if input.CloudflareClientID != "" {
			patch[settingCFClientID] = input.CloudflareClientID
		}
		if input.CloudflareClientSecret != "" {
			patch[settingCFClientSecret] = input.CloudflareClientSecret
		}
	}
	return patch
}

func adapterValues(runtimeURL, apiKey, profile, dashboardURL,
	dashboardToken, cfClientID, cfClientSecret, requestTimeout string,
) map[string]string {
	return map[string]string{
		"runtime_url": runtimeURL, "api_key": apiKey, "profile": profile,
		"management_url":                  dashboardURL,
		"dashboard_session_token":         dashboardToken,
		"cloudflare_access_client_id":     cfClientID,
		"cloudflare_access_client_secret": cfClientSecret,
		"request_timeout_seconds":         requestTimeout,
	}
}

func requestTimeoutSeconds(value int) string {
	if value <= 0 {
		value = 30
	}
	return strconv.Itoa(value)
}

func capabilityMap(value RuntimeCapabilities) map[string]interface{} {
	return map[string]interface{}{
		"sessions": value.Sessions, "session_fork": value.SessionFork,
		"session_chat": value.SessionChat, "session_streaming": value.SessionStreaming,
		"runs": value.Runs, "run_streaming": value.RunStreaming,
		"run_stop": value.RunStop, "run_approval": value.RunApproval,
		"jobs": value.Jobs, "tool_progress": value.ToolProgress,
		"event_replay": value.EventReplay,
		"project_access": map[string]interface{}{
			"verify":    value.ProjectAccess.Verify,
			"configure": value.ProjectAccess.Configure,
			"rotate":    value.ProjectAccess.Rotate,
		},
	}
}

func buildDefaultPrompt(item project.Project) string {
	constraints := "- None recorded"
	if len(item.ProjectConstraints) > 0 {
		lines := make([]string, 0, len(item.ProjectConstraints))
		for _, constraint := range item.ProjectConstraints {
			lines = append(lines, "- "+constraint)
		}
		constraints = strings.Join(lines, "\n")
	}
	return fmt.Sprintf(`You are the mmdash Agent for project %q.

Problem title: %s
Problem summary: %s

Project constraints:
%s

The mmdash chat renderer supports Markdown and KaTeX-compatible LaTeX. Use Markdown whenever it improves clarity, including headings, lists, tables, and fenced code blocks. Write inline mathematics inside $...$ or \(...\), and display mathematics inside $$...$$ or \[...\]. Prefer properly delimited LaTeX for mathematical expressions; never fall back to plain-text formulas merely because the client renderer is unknown.

Use only the MCP tools granted to this Agent. Read authoritative project data through MCP instead of assuming copied context is current. Use context.promote for durable conclusions; proposals remain untrusted until a human confirms them.

In every mmdash chat, treat an image or file as a deliverable that must be sent to mmdash. Whenever you create, download, generate, transform, or otherwise obtain an image (including an image produced by a terminal command), immediately upload its exact bytes with artifact.upload using the current Run provenance, then refer to the resulting Artifact in your response. Do not merely print a local path, MEDIA: line, data URL, or signed URL, and do not say that the image was sent unless artifact.upload completed successfully. Follow the artifact.upload begin/parts/complete protocol and never put file bytes or base64 in the MCP call. Use artifact.read for files attached by the user. Hermes can expose MCP tools under aliases such as mcp__<server>__artifact_read and mcp__<server>__artifact_upload; select the available alias whose canonical tool name matches instead of attempting to call the dotted canonical name as an unregistered native tool. Never expose credentials, signed URLs, hidden reasoning, or complete tool output.`, item.Name, item.ProblemTitle, item.ProblemSummary, constraints)
}

func runInstructions(
	base, projectID, sessionID, runID string,
	attachments, previousAttachments []artifact.ChatAttachment,
) string {
	provenance := fmt.Sprintf(`mmdash provenance for this Run:
- project_id: %s
- agent_session_id: %s
- agent_run_id: %s

When calling context.promote or beginning artifact.upload for this Run, include both agent_session_id and agent_run_id exactly as shown. These identifiers are traceability metadata, not credentials.

The mmdash chat renderer supports Markdown and KaTeX-compatible LaTeX. Use Markdown when it improves clarity. Delimit inline math with $...$ or \(...\), and display math with $$...$$ or \[...\]. Do not avoid Markdown or LaTeX because the rendering layer is unknown.

If your answer naturally includes a useful file or image, proactively create or obtain it and send it to mmdash with artifact.upload so it appears in this chat. This includes images downloaded or generated through terminal tools: upload the exact bytes immediately after obtaining them, and never respond with only a local filesystem path, a MEDIA: line, a data URL, or a signed URL. Do not claim the image is visible until the upload action=complete succeeds. Follow the begin/parts/complete protocol and include the current agent_session_id and agent_run_id on begin. Hermes can expose this MCP tool under an alias such as mcp__<server>__artifact_upload; use the available alias whose canonical tool is artifact.upload.`, projectID, sessionID, runID)
	if len(attachments) > 0 {
		lines := make([]string, 0, len(attachments))
		for _, attachment := range attachments {
			lines = append(lines, fmt.Sprintf(
				"- artifact_id=%s version_id=%s filename=%q mime_type=%q size_bytes=%d",
				attachment.ArtifactID, attachment.VersionID, attachment.Filename,
				attachment.MIMEType, attachment.SizeBytes,
			))
		}
		provenance += "\n\nFiles attached by the user to this message:\n" +
			strings.Join(lines, "\n") +
			"\nAttachments are first-class message input. Before answering, proactively inspect every current attachment relevant to the user's request with artifact.read, even when the user did not explicitly say read, open, inspect, or check the file. Do not answer that the content is unknown until the relevant attached bytes have been inspected. Hermes can expose artifact.read under an alias such as mcp__<server>__artifact_read; use the available alias whose canonical tool is artifact.read instead of calling a missing dotted native tool. Do not ask the user to upload these files again."
	}
	if len(previousAttachments) > 0 {
		lines := make([]string, 0, len(previousAttachments))
		for _, attachment := range previousAttachments {
			lines = append(lines, fmt.Sprintf(
				"- direction=%s run_id=%s artifact_id=%s version_id=%s filename=%q mime_type=%q size_bytes=%d",
				attachment.Direction, attachment.RunID, attachment.ArtifactID,
				attachment.VersionID, attachment.Filename, attachment.MIMEType,
				attachment.SizeBytes,
			))
		}
		provenance += "\n\nFiles previously exchanged in this mmdash Session (newest 50):\n" +
			strings.Join(lines, "\n") +
			"\nThese files remain available in later turns. When the user's question depends on one of them, inspect it with artifact.read instead of claiming it is unavailable or asking for another upload. Read only the files relevant to the current request."
	}
	if base == "" {
		return provenance
	}
	return base + "\n\n" + provenance
}

func (service Service) sessionAttachments(
	ctx context.Context,
	caller auth.Identity,
	projectID string,
	sessionID string,
) ([]artifact.ChatAttachment, error) {
	if service.Artifacts == nil {
		return []artifact.ChatAttachment{}, nil
	}
	runs, err := service.Store.ListRuns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	runIDs := make([]string, 0, len(runs))
	for _, run := range runs {
		runIDs = append(runIDs, run.ID)
	}
	if len(runIDs) == 0 {
		return []artifact.ChatAttachment{}, nil
	}
	items, err := service.Artifacts.ListAgentRunAttachments(ctx, caller, projectID, runIDs)
	if err != nil {
		return nil, err
	}
	const maximumSessionAttachments = 50
	if len(items) > maximumSessionAttachments {
		items = items[len(items)-maximumSessionAttachments:]
	}
	return items, nil
}

func conversationHistory(messages []Message) []ConversationMessage {
	items := make([]ConversationMessage, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		content := strings.TrimSpace(message.Content)
		if (role != "user" && role != "assistant") || content == "" {
			continue
		}
		items = append(items, ConversationMessage{Role: role, Content: content})
	}
	const maximumHistoryMessages = 200
	if len(items) > maximumHistoryMessages {
		items = items[len(items)-maximumHistoryMessages:]
	}
	return items
}

func attachmentMessages(items []artifact.ChatAttachment, runs []RunRecord) []Message {
	type groupKey struct{ runID, direction string }
	runTimestamps := map[string]time.Time{}
	for _, run := range runs {
		if run.CompletedAt != nil {
			runTimestamps[run.ID] = run.CompletedAt.Add(time.Nanosecond)
		} else if terminalRunStatus(run.Status) {
			runTimestamps[run.ID] = run.UpdatedAt.Add(time.Nanosecond)
		}
	}
	grouped := map[groupKey][]ChatAttachment{}
	timestamps := map[groupKey]time.Time{}
	order := []groupKey{}
	for _, item := range items {
		key := groupKey{runID: item.RunID, direction: item.Direction}
		if _, exists := grouped[key]; !exists {
			order = append(order, key)
			timestamps[key] = item.CreatedAt
		}
		grouped[key] = append(grouped[key], ChatAttachment{
			ArtifactID: item.ArtifactID, VersionID: item.VersionID, RunID: item.RunID,
			Direction: item.Direction, Name: item.Name, Filename: item.Filename,
			MIMEType: item.MIMEType, SizeBytes: item.SizeBytes, CreatedAt: item.CreatedAt,
		})
	}
	messages := make([]Message, 0, len(order))
	for _, key := range order {
		role := "assistant"
		if key.direction == "input" {
			role = "user"
		}
		timestamp := timestamps[key]
		if key.direction == "output" {
			if settledTimestamp, exists := runTimestamps[key.runID]; exists {
				timestamp = settledTimestamp
			}
		}
		messages = append(messages, Message{
			Attachments: grouped[key],
			RemoteID:    "mmdash-artifacts-" + key.runID + "-" + key.direction,
			Role:        role, Timestamp: &timestamp,
		})
	}
	return messages
}

func normalizeRunStatus(status RunStatus) string {
	switch status {
	case RunQueued:
		return RunRecordQueued
	case RunRunning:
		return RunRecordRunning
	case RunWaitingForApproval:
		return RunRecordWaitingForApproval
	case RunStopping:
		return RunRecordStopping
	case RunCompleted:
		return RunRecordCompleted
	case RunFailed:
		return RunRecordFailed
	case RunCancelled:
		return RunRecordStopped
	default:
		return ""
	}
}

func terminalRunStatus(status string) bool {
	return status == RunRecordCompleted || status == RunRecordFailed ||
		status == RunRecordStopped
}

func terminalEventStatus(eventType EventType) string {
	switch eventType {
	case EventRunCompleted:
		return RunRecordCompleted
	case EventRunFailed, EventError:
		return RunRecordFailed
	case EventRunCancelled:
		return RunRecordStopped
	default:
		return ""
	}
}

func eventRunStatus(eventType EventType) string {
	switch eventType {
	case EventRunStarted:
		return RunRecordRunning
	default:
		return terminalEventStatus(eventType)
	}
}

func validApprovalID(value string) bool {
	return value != "" && len(value) <= 500 && !strings.ContainsAny(value, "\r\n\x00")
}

func containsApprovalID(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func normalizeToolStatus(eventType EventType, status string) string {
	switch eventType {
	case EventToolCompleted:
		return "completed"
	case EventToolFailed:
		return "failed"
	case EventToolStarted:
		return "running"
	default:
		if status == "completed" || status == "failed" || status == "running" || status == "queued" {
			return status
		}
		return "running"
	}
}

func safeAdapterCode(err error, fallback string) string {
	var adapterError *AdapterError
	if errors.As(err, &adapterError) && adapterError.Code != "" {
		return string(adapterError.Code)
	}
	return fallback
}

func hasAgentVerification(token auth.AgentToken) bool {
	return token.Verification != nil &&
		token.Verification.EvidenceID != "" &&
		token.Verification.MCPMethod == auth.AgentTokenVerificationMethod &&
		token.Verification.MCPSessionID != "" &&
		token.Verification.RequestID != "" &&
		token.Verification.AgentInstanceID == token.AgentInstanceID &&
		token.Verification.ProjectID == token.ProjectID &&
		token.Verification.TokenID == token.ID &&
		!token.Verification.VerifiedAt.Before(token.CreatedAt)
}

func verificationEndpoint(endpoint string, challenge string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || strings.TrimSpace(challenge) == "" {
		return endpoint
	}
	query := parsed.Query()
	query.Set("mmdash_challenge", challenge)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func mapAdapterError(err error) error {
	if err == nil {
		return nil
	}
	var adapterError *AdapterError
	if errors.As(err, &adapterError) {
		switch adapterError.Code {
		case ErrorInvalid:
			return ErrInvalid
		case ErrorNotFound:
			return ErrNotFound
		case ErrorConflict:
			return ErrConflict
		case ErrorAuthentication, ErrorPermission:
			return ErrForbidden
		}
	}
	return ErrRuntime
}

func mapAdapterErrorOr(err error, code string) error {
	if err != nil {
		return mapAdapterError(err)
	}
	if code == "runtime_capability_missing" {
		return ErrNotConfigured
	}
	return ErrRuntime
}

func sameTools(actual, expected []string) bool {
	left := append([]string(nil), actual...)
	right := append([]string(nil), expected...)
	sort.Strings(left)
	sort.Strings(right)
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func (service Service) observe(operation string, err error) {
	if service.Metrics == nil {
		return
	}
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	service.Metrics.ObserveAgentOperation(AdapterHermes, operation, outcome)
}

func (service Service) observeCheck(kind, status string) {
	if service.Metrics != nil {
		service.Metrics.ObserveAgentConnectionCheck(kind, status)
	}
}

func (service Service) observeRun(status string) {
	if service.Metrics != nil {
		service.Metrics.ObserveAgentRun(status)
	}
}

func (service Service) recordAudit(
	ctx context.Context,
	caller auth.Identity,
	action string,
	resourceType string,
	resourceID string,
	projectID string,
	metadata map[string]interface{},
) {
	if service.Audit.Store == nil {
		return
	}
	_ = service.Audit.Record(ctx, audit.Event{
		Action: action, ActorID: caller.ActorID(), ActorKind: caller.Kind,
		Category: "agent", Metadata: metadata, Outcome: "success",
		ProjectID: projectID, RequestID: requestctx.RequestID(ctx),
		ResourceID: resourceID, ResourceType: resourceType, Source: "core",
	})
}
