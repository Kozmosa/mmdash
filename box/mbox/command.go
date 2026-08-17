// Package mbox implements the cross-platform administrative commands for
// the Box binary. The Gateway itself remains the long-running worker; these
// commands own setup, account binding, configuration and service lifecycle.
package mbox

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/box/config"
	"github.com/mmdash/mmdash/box/contracts"
	"github.com/mmdash/mmdash/box/gateway"
)

const ServiceName = "MmdashBox"

var defaultLimits = contracts.ResourceLimits{
	CPUMillis: 1000, MemoryBytes: 1 << 30, TimeoutSecond: 3600,
	DiskBytes: 10 << 30, PIDs: 256, Network: "disabled",
}

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if strings.HasPrefix(args[0], "--") {
		return false, nil
	}
	switch args[0] {
	case "help", "--help", "-h":
		printHelp(stdout)
		return true, nil
	case "setup":
		return true, setup(args[1:], stdout)
	case "account":
		return true, account(ctx, args[1:], stdout)
	case "config":
		return true, configuration(args[1:], stdout)
	case "service":
		return true, service(args[1:], stdout)
	case "uninstall":
		return true, uninstall(args[1:], stdout)
	default:
		return false, fmt.Errorf("unknown mbox command %q (run mbox help)", args[0])
	}
}

func setup(args []string, stdout io.Writer) error {
	values, flags, err := parseOptions(args)
	if err != nil {
		return err
	}
	root := values["root"]
	if root == "" {
		root = config.DefaultRoot()
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	interactive := !flags["non-interactive"]
	reader := bufio.NewReader(os.Stdin)
	if interactive {
		cfg.ControlURL = prompt(reader, stdout, "mmdash 公网地址（支持 HTTP/HTTPS）", cfg.ControlURL)
		cfg.Name = prompt(reader, stdout, "Box 名称", cfg.Name)
		cfg.LocalDocker.Enabled = promptYesNo(reader, stdout, "启用 Local Docker", cfg.LocalDocker.Enabled)
		if cfg.LocalDocker.Enabled {
			cfg.LocalDocker.Image = prompt(reader, stdout, "Local Docker 镜像", cfg.LocalDocker.Image)
		}
		cfg.E2B.Enabled = promptYesNo(reader, stdout, "启用 E2B Runtime", cfg.E2B.Enabled)
		if cfg.E2B.Enabled {
			cfg.E2B.APIKey = promptSecret(reader, stdout, "E2B API Key", cfg.E2B.APIKey)
			cfg.E2B.Domain = prompt(reader, stdout, "E2B Domain", cfg.E2B.Domain)
			cfg.E2B.APIURL = prompt(reader, stdout, "E2B API URL（可空）", cfg.E2B.APIURL)
			cfg.E2B.SandboxURL = prompt(reader, stdout, "E2B Sandbox URL（可空）", cfg.E2B.SandboxURL)
			cfg.E2B.Template = prompt(reader, stdout, "E2B Template", cfg.E2B.Template)
		}
	}
	applyConfigValues(&cfg, values)
	if flags["no-local-docker"] {
		cfg.LocalDocker.Enabled = false
	}
	if flags["enable-e2b"] {
		cfg.E2B.Enabled = true
	}
	if key := strings.TrimSpace(values["e2b-api-key"]); key != "" {
		cfg.E2B.APIKey = key
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}
	for _, directory := range []string{"logs", "state", "outputs", "sources", "tasks"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			return err
		}
	}
	if err := config.Save(root, cfg); err != nil {
		return err
	}
	statePath := filepath.Join(root, "state.json")
	identity, err := gateway.LoadIdentity(statePath)
	if err != nil {
		return err
	}
	if identity.InstallationID == "" {
		identity.InstallationID = "box-installation-" + randomID()
		if err := gateway.SaveIdentity(statePath, identity); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Box 已初始化：%s\n配置文件：%s\n", root, config.Path(root))
	fmt.Fprintln(stdout, "下一步：mbox account login，然后执行 mbox service init")
	return nil
}

func account(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	values, _, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	root := values["root"]
	if root == "" {
		root = config.DefaultRoot()
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		identity, err := gateway.LoadIdentity(filepath.Join(root, "state.json"))
		if err != nil {
			return err
		}
		if identity.BoxID == "" {
			fmt.Fprintln(stdout, "未绑定账号。请执行：mbox account login")
			return nil
		}
		fmt.Fprintf(stdout, "Box ID: %s\nInstallation ID: %s\n", identity.BoxID, identity.InstallationID)
		return nil
	case "logout":
		identity, err := gateway.LoadIdentity(filepath.Join(root, "state.json"))
		if err != nil {
			return err
		}
		identity.BoxID, identity.BoxToken = "", ""
		if err := gateway.SaveIdentity(filepath.Join(root, "state.json"), identity); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "本地 Box 凭据已清除；如需撤销云端 Box，请在 mmdash Box 管理中执行撤销。")
		return nil
	case "login":
		return accountLogin(ctx, root, stdout)
	default:
		return fmt.Errorf("unknown account command %q (use login, status or logout)", args[0])
	}
}

func accountLogin(ctx context.Context, root string, stdout io.Writer) error {
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("load Box configuration: %w (run mbox setup first)", err)
	}
	statePath := filepath.Join(root, "state.json")
	identity, err := gateway.LoadIdentity(statePath)
	if err != nil {
		return err
	}
	if identity.InstallationID == "" {
		identity.InstallationID = "box-installation-" + randomID()
	}
	client := gateway.HTTPClient{BaseURL: cfg.ControlURL}
	authorization, err := client.StartDeviceAuthorization(ctx)
	if err != nil {
		return err
	}
	verification := authorization.VerificationURIComplete
	if verification == "" {
		verification = authorization.VerificationURI
	}
	fmt.Fprintf(stdout, "请在浏览器打开：%s\n设备码：%s\n", verification, authorization.UserCode)
	openURL(verification)
	interval := time.Duration(authorization.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	var grant contracts.BoxRegistrationGrant
	for {
		if !authorization.ExpiresAt.IsZero() && !time.Now().Before(authorization.ExpiresAt) {
			return errors.New("Box device authorization expired")
		}
		grant, err = client.ExchangeDeviceAuthorization(ctx, authorization.DeviceCode)
		if err == nil {
			break
		}
		var pending interface{ AuthorizationPending() bool }
		if !errors.As(err, &pending) || !pending.AuthorizationPending() {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	registration, err := client.Register(ctx, grant.RegistrationGrant, gateway.RegistrationInputFor(
		identity.InstallationID, cfg.Name, "dev", cfgToRuntimes(cfg), defaultLimits,
	))
	if err != nil {
		return err
	}
	identity.BoxID, identity.BoxToken = registration.BoxID, registration.Token
	if err := gateway.SaveIdentity(statePath, identity); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Box 绑定成功：%s\n", registration.BoxID)
	return nil
}

func configuration(args []string, stdout io.Writer) error {
	values, _, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	root := values["root"]
	if root == "" {
		root = config.DefaultRoot()
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	switch first := firstArg(args); first {
	case "", "show":
		output := cfg
		output.E2B.APIKey = maskSecret(output.E2B.APIKey)
		return json.NewEncoder(stdout).Encode(output)
	case "set":
		if len(args) < 2 {
			return errors.New("usage: mbox config set key=value")
		}
		for _, assignment := range args[1:] {
			if strings.HasPrefix(assignment, "--") {
				continue
			}
			key, value, ok := strings.Cut(assignment, "=")
			if !ok {
				return fmt.Errorf("invalid setting %q; expected key=value", assignment)
			}
			if err := setConfigValue(&cfg, key, value); err != nil {
				return err
			}
		}
		if err := config.Save(root, cfg); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "配置已保存")
		return nil
	default:
		return fmt.Errorf("unknown config command %q (use show or set)", first)
	}
}

func service(args []string, stdout io.Writer) error {
	values, _, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	root := values["root"]
	if root == "" {
		root = config.DefaultRoot()
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	command := firstArg(args)
	switch command {
	case "init":
		return serviceInit(root, stdout)
	case "start":
		return serviceAction("start", stdout)
	case "stop":
		return serviceAction("stop", stdout)
	case "remove":
		return removeService(root, stdout)
	case "status", "":
		return serviceAction("status", stdout)
	case "logs":
		return serviceLogs(root, stdout)
	default:
		return fmt.Errorf("unknown service command %q (use init, start, stop, status, logs or remove)", command)
	}
}

func serviceLogs(root string, stdout io.Writer) error {
	path := filepath.Join(root, "logs", "service.log")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stdout, "尚无服务日志；可直接运行 mbox --gateway --root PATH 查看启动日志")
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(stdout, file)
	return err
}

func serviceInit(root string, stdout io.Writer) error {
	if _, err := os.Stat(config.Path(root)); err != nil {
		return errors.New("Box 尚未 setup；请先运行 mbox setup")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		binPath := fmt.Sprintf(`"%s" --gateway --root "%s"`, executable, root)
		if err := runSystemCommand("sc.exe", "create", ServiceName, "binPath=", binPath, "start=", "auto", "DisplayName=", "mmdash Box Gateway"); err != nil {
			return err
		}
		_ = runSystemCommand("sc.exe", "description", ServiceName, "mmdash outbound Box Gateway")
	} else {
		unitPath := "/etc/systemd/system/mmdash-box.service"
		unit := fmt.Sprintf("[Unit]\nDescription=mmdash Box Gateway\nAfter=network-online.target\n\n[Service]\nType=simple\nExecStart=%s --gateway --root %s\nRestart=always\nRestartSec=5\n\n[Install]\nWantedBy=multi-user.target\n", shellQuote(executable), shellQuote(root))
		if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
			return fmt.Errorf("write %s: %w (run as root)", unitPath, err)
		}
		if err := runSystemCommand("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := runSystemCommand("systemctl", "enable", "mmdash-box.service"); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(root, "service.json"), []byte(fmt.Sprintf("{\"name\":%q,\"root\":%q}\n", ServiceName, root)), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Box 服务已注册并设置为开机启动")
	return nil
}

func serviceAction(action string, stdout io.Writer) error {
	if runtime.GOOS == "windows" {
		if action == "status" {
			return runSystemCommand("sc.exe", "query", ServiceName)
		}
		return runSystemCommand("sc.exe", action, ServiceName)
	}
	if action == "status" {
		return runSystemCommand("systemctl", "status", "mmdash-box.service", "--no-pager")
	}
	return runSystemCommand("systemctl", action, "mmdash-box.service")
}

func removeService(root string, stdout io.Writer) error {
	marker := filepath.Join(root, "service.json")
	if _, err := os.Stat(marker); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stdout, "未发现已注册的 Box 服务")
			return nil
		}
		return err
	}
	if err := removeRegisteredService(); err != nil {
		return err
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Fprintf(stdout, "Box 服务已移除，配置和数据保留：%s\n", root)
	return nil
}

func removeRegisteredService() error {
	if runtime.GOOS == "windows" {
		if err := runSystemCommand("sc.exe", "stop", ServiceName); err != nil && !isServiceStateError(err, 1062) {
			return err
		}
		if err := runSystemCommand("sc.exe", "delete", ServiceName); err != nil && !isServiceStateError(err, 1060) {
			return err
		}
		return nil
	}
	if err := runSystemCommand("systemctl", "disable", "--now", "mmdash-box.service"); err != nil {
		return err
	}
	if err := os.Remove("/etc/systemd/system/mmdash-box.service"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove systemd unit: %w", err)
	}
	return runSystemCommand("systemctl", "daemon-reload")
}

func isServiceStateError(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ProcessState.ExitCode() == code
}

func uninstall(args []string, stdout io.Writer) error {
	values, flags, err := parseOptions(args)
	if err != nil {
		return err
	}
	root := values["root"]
	if root == "" {
		root = config.DefaultRoot()
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	if _, err := os.Stat(config.Path(root)); err != nil {
		return errors.New("拒绝清理：目标不是已初始化的 Box 目录")
	}
	if !flags["yes"] {
		fmt.Fprintf(stdout, "将停止服务并删除 Box 目录 %s，继续？[y/N] ", root)
		var answer string
		_, _ = fmt.Fscanln(os.Stdin, &answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return errors.New("已取消")
		}
	}
	serviceMarker := filepath.Join(root, "service.json")
	if _, markerErr := os.Stat(serviceMarker); markerErr == nil {
		if err := removeRegisteredService(); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Box 已卸载并清理：%s\n", root)
	return nil
}

func parseOptions(args []string) (map[string]string, map[string]bool, error) {
	values := map[string]string{}
	flags := map[string]bool{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if index+1 < len(args) && !strings.HasPrefix(args[index+1], "--") {
			values[key] = args[index+1]
			index++
			continue
		}
		flags[key] = true
	}
	return values, flags, nil
}

func applyConfigValues(cfg *config.Config, values map[string]string) {
	if value := values["control-url"]; value != "" {
		cfg.ControlURL = value
	}
	if value := values["name"]; value != "" {
		cfg.Name = value
	}
	if value := values["local-docker-image"]; value != "" {
		cfg.LocalDocker.Image = value
	}
	if value := values["e2b-domain"]; value != "" {
		cfg.E2B.Domain = value
	}
	if value := values["e2b-api-url"]; value != "" {
		cfg.E2B.APIURL = value
	}
	if value := values["e2b-sandbox-url"]; value != "" {
		cfg.E2B.SandboxURL = value
	}
	if value := values["e2b-template"]; value != "" {
		cfg.E2B.Template = value
	}
}

func setConfigValue(cfg *config.Config, key, value string) error {
	switch key {
	case "control-url":
		cfg.ControlURL = value
	case "name":
		cfg.Name = value
	case "local-docker.enabled":
		cfg.LocalDocker.Enabled = value == "true" || value == "1" || value == "yes"
	case "local-docker.image":
		cfg.LocalDocker.Image = value
	case "e2b.enabled":
		cfg.E2B.Enabled = value == "true" || value == "1" || value == "yes"
	case "e2b.api-key":
		cfg.E2B.APIKey = value
	case "e2b.domain":
		cfg.E2B.Domain = value
	case "e2b.api-url":
		cfg.E2B.APIURL = value
	case "e2b.sandbox-url":
		cfg.E2B.SandboxURL = value
	case "e2b.template":
		cfg.E2B.Template = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return config.Validate(*cfg)
}

func cfgToRuntimes(cfg config.Config) []contracts.Runtime {
	runtimes := make([]contracts.Runtime, 0, 2)
	if cfg.LocalDocker.Enabled {
		runtimes = append(runtimes, contracts.Runtime{Name: "local-docker", Version: "1", Image: cfg.LocalDocker.Image})
	}
	if cfg.E2B.Enabled {
		runtimes = append(runtimes, contracts.Runtime{Name: "e2b", Version: "1", Image: cfg.E2B.Template})
	}
	return runtimes
}

func prompt(reader *bufio.Reader, out io.Writer, label, fallback string) string {
	fmt.Fprintf(out, "%s [%s]: ", label, fallback)
	value, _ := reader.ReadString('\n')
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func promptSecret(reader *bufio.Reader, out io.Writer, label, fallback string) string {
	masked := "已保存"
	if fallback == "" {
		masked = "未设置"
	}
	return prompt(reader, out, label+"（输入空白保留，当前"+masked+"）", fallback)
}

func promptYesNo(reader *bufio.Reader, out io.Writer, label string, fallback bool) bool {
	defaultValue := "y"
	if !fallback {
		defaultValue = "n"
	}
	fmt.Fprintf(out, "%s [y/n，默认 %s]: ", label, defaultValue)
	value, _ := reader.ReadString('\n')
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value == "y" || value == "yes"
}

func printHelp(out io.Writer) {
	fmt.Fprintln(out, `mbox - mmdash Box 管理命令

用法：
  mbox gateway [--root PATH]
  mbox setup [--root PATH] [--control-url URL] [--name NAME]
  mbox account login|status|logout [--root PATH]
  mbox config show|set key=value [--root PATH]
  mbox service init|start|stop|status|logs|remove [--root PATH]
  mbox uninstall [--root PATH] [--yes]

setup 会引导配置 mmdash 公网地址（支持 HTTP/HTTPS）、Box 名称、Local Docker
和 E2B。配置、Token、日志、离线任务和输出都保存在 Box 根目录内。
gateway 会在当前终端以前台方式运行 Gateway 并输出启动、Runtime 探测和停止日志。
service init 会注册开机启动服务；uninstall 会停止并移除服务后清理 Box 根目录。`)
}

func firstArg(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			return arg
		}
	}
	return ""
}

func randomID() string {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(data)
}

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	return "********"
}

func openURL(address string) {
	parsed, err := url.Parse(address)
	if err != nil {
		return
	}
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", parsed.String()).Start()
	case "darwin":
		_ = exec.Command("open", parsed.String()).Start()
	default:
		_ = exec.Command("xdg-open", parsed.String()).Start()
	}
}

func runSystemCommand(command string, args ...string) error {
	process := exec.Command(command, args...)
	process.Stdout, process.Stderr, process.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := process.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}
	return nil
}

func shellQuote(value string) string {
	return strconv.Quote(value)
}
