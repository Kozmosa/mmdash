package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmdash/mmdash/clients/cli/internal/api"
	"github.com/mmdash/mmdash/clients/cli/internal/app"
	"github.com/mmdash/mmdash/clients/cli/internal/apperror"
	cliAuth "github.com/mmdash/mmdash/clients/cli/internal/auth"
	"github.com/mmdash/mmdash/clients/cli/internal/config"
	"github.com/mmdash/mmdash/clients/cli/internal/mcpbridge"
)

type Feature struct{}

func (Feature) Register(registry *app.Registry) error {
	for _, command := range []app.Command{loginCommand{}, logoutCommand{}, whoamiCommand{}, setDomainCommand{}, mcpCommand{}, doctorCommand{}} {
		if err := registry.AddCommand(command); err != nil {
			return err
		}
	}
	registry.AddDoctorCheck(configCheck{})
	registry.AddDoctorCheck(identityCheck{})
	registry.AddDoctorCheck(gatewayCheck{})
	return nil
}

type setDomainCommand struct{}

func (setDomainCommand) Name() string { return "config set-domain" }
func (setDomainCommand) Summary() string {
	return "Set the unified mmdash domain (defaults to " + config.DefaultDomain + ")"
}
func (setDomainCommand) Run(_ context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) > 1 {
		return apperror.Usage("Usage: mmdash config set-domain [domain]")
	}
	domain := ""
	if len(arguments) == 1 {
		domain = arguments[0]
	}
	updated, err := config.WithDomain(runtime.Config, domain)
	if err != nil {
		return apperror.Wrap("CONFIG_INVALID", err.Error(), 5, err)
	}
	previous := runtime.Config
	runtime.Config = updated
	if err := runtime.SaveConfig(); err != nil {
		runtime.Config = previous
		return apperror.Wrap("CONFIG_WRITE_ERROR", "Cannot save the CLI configuration", 5, err)
	}
	return runtime.Printer.Result(map[string]string{
		"core_url":   updated.CoreURL,
		"mcp_url":    updated.MCPURL,
		"server_url": updated.ServerURL,
	})
}

type loginCommand struct{}

func (loginCommand) Name() string    { return "login" }
func (loginCommand) Summary() string { return "Authorize this device in a browser" }
func (loginCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	noBrowser := false
	for _, argument := range arguments {
		if argument == "--no-browser" {
			noBrowser = true
		} else {
			return apperror.Usage("Unknown login option %q", argument)
		}
	}
	authorization, err := runtime.API.StartDeviceAuthorization(ctx)
	if err != nil {
		return cliAuth.Translate(err)
	}
	if !noBrowser {
		_ = openBrowser(authorization.VerificationURIComplete)
	}
	_, _ = fmt.Fprintf(runtime.Printer.Stderr, "Open %s and enter code %s\n", authorization.VerificationURI, authorization.UserCode)
	interval := time.Duration(authorization.Interval) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	for {
		result, err := runtime.API.ExchangeDeviceAuthorization(ctx, authorization.DeviceCode)
		if err == nil {
			session := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL)
			if err := session.Save(result); err != nil {
				return apperror.Wrap("CREDENTIAL_STORE_ERROR", "Cannot save the CLI session in the system credential store", 3, err)
			}
			return runtime.Printer.Result(map[string]interface{}{"status": "authenticated", "user": result.User})
		}
		var remote *api.Error
		if !errors.As(err, &remote) || remote.Code != "AUTHORIZATION_PENDING" {
			return cliAuth.Translate(err)
		}
		if !time.Now().Before(authorization.ExpiresAt) {
			return apperror.New("AUTHORIZATION_EXPIRED", "The device authorization expired", 3)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

type logoutCommand struct{}

func (logoutCommand) Name() string    { return "logout" }
func (logoutCommand) Summary() string { return "Revoke and remove the local CLI session" }
func (logoutCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 0 {
		return apperror.Usage("logout does not accept arguments")
	}
	session := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL)
	token, err := session.AccessToken(ctx, false)
	if err == nil {
		if logoutErr := runtime.API.Logout(ctx, token); logoutErr != nil {
			var remote *api.Error
			if !errors.As(logoutErr, &remote) || remote.Status != http.StatusUnauthorized {
				return cliAuth.Translate(logoutErr)
			}
		}
	} else if normalized := apperror.Normalize(err); normalized.Code != "AUTH_REQUIRED" {
		return err
	}
	if err := session.Delete(); err != nil {
		return err
	}
	return runtime.Printer.Result("Logged out")
}

type whoamiCommand struct{}

func (whoamiCommand) Name() string    { return "whoami" }
func (whoamiCommand) Summary() string { return "Show the delegated mmdash identity" }
func (whoamiCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 0 {
		return apperror.Usage("whoami does not accept arguments")
	}
	session := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL)
	token, err := session.AccessToken(ctx, false)
	if err != nil {
		return err
	}
	identity, err := runtime.API.WhoAmI(ctx, token)
	if err != nil {
		return cliAuth.Translate(err)
	}
	return runtime.Printer.Result(identity)
}

type mcpCommand struct{}

func (mcpCommand) Name() string    { return "mcp" }
func (mcpCommand) Summary() string { return "Run the stdio to remote MCP bridge" }
func (mcpCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 0 {
		return apperror.Usage("mcp does not accept arguments")
	}
	session := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL)
	if _, err := session.AccessToken(ctx, false); err != nil {
		return err
	}
	var transport mcpbridge.Transport = &mcpbridge.Bridge{CurrentProjectID: runtime.Config.CurrentProjectID, Endpoint: runtime.Config.MCPURL, Stderr: runtime.Printer.Stderr, Stdin: runtime.Stdin, Stdout: runtime.Printer.Stdout, Tokens: session}
	return transport.Run(ctx)
}

type doctorCommand struct{}

func (doctorCommand) Name() string { return "doctor" }
func (doctorCommand) Summary() string {
	return "Diagnose config, authentication, project, and MCP connectivity"
}
func (doctorCommand) Run(ctx context.Context, runtime *app.Runtime, arguments []string) error {
	if len(arguments) != 0 {
		return apperror.Usage("doctor does not accept arguments")
	}
	results := make([]app.CheckResult, 0, len(runtime.DoctorChecks))
	failed := false
	for _, check := range runtime.DoctorChecks {
		result := check.Run(ctx, runtime)
		results = append(results, result)
		failed = failed || result.Status == "fail"
	}
	if err := runtime.Printer.Result(map[string]interface{}{"checks": results, "status": map[bool]string{true: "failed", false: "ok"}[failed]}); err != nil {
		return err
	}
	if failed {
		return apperror.New("DOCTOR_FAILED", "One or more diagnostics failed", 1)
	}
	return nil
}

type configCheck struct{}

func (configCheck) Name() string { return "config" }
func (configCheck) Run(_ context.Context, runtime *app.Runtime) app.CheckResult {
	return app.CheckResult{Name: "config", Status: "ok", Detail: runtime.Paths.ConfigFile}
}

type identityCheck struct{}

func (identityCheck) Name() string { return "identity" }
func (identityCheck) Run(ctx context.Context, runtime *app.Runtime) app.CheckResult {
	session := cliAuth.New(runtime.API, runtime.CredentialStore, runtime.Config.ServerURL)
	token, err := session.AccessToken(ctx, false)
	if err != nil {
		return app.CheckResult{Name: "identity", Status: "fail", Detail: apperror.Normalize(err).Message}
	}
	identity, err := runtime.API.WhoAmI(ctx, token)
	if err != nil {
		return app.CheckResult{Name: "identity", Status: "fail", Detail: err.Error()}
	}
	return app.CheckResult{Name: "identity", Status: "ok", Detail: identity.User.Email}
}

type gatewayCheck struct{}

func (gatewayCheck) Name() string { return "mcp_gateway" }
func (gatewayCheck) Run(ctx context.Context, runtime *app.Runtime) app.CheckResult {
	endpoint, err := url.Parse(runtime.Config.MCPURL)
	if err != nil {
		return app.CheckResult{Name: "mcp_gateway", Status: "fail", Detail: "invalid MCP URL"}
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/health/live"
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return app.CheckResult{Name: "mcp_gateway", Status: "fail", Detail: "gateway is unreachable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return app.CheckResult{Name: "mcp_gateway", Status: "fail", Detail: fmt.Sprintf("HTTP %d", response.StatusCode)}
	}
	return app.CheckResult{Name: "mcp_gateway", Status: "ok", Detail: endpoint.Host}
}
