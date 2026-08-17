//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mmdash/mmdash/box/config"
	"github.com/mmdash/mmdash/box/mbox"
	"golang.org/x/sys/windows/svc"
)

func runAsServiceIfNeeded(args []string) (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false, err
	}
	options, err := parseGatewayOptions(args)
	if err != nil {
		return true, err
	}
	err = svc.Run(mbox.ServiceName, windowsService{root: options.root})
	if err != nil {
		appendServiceLog(options.root, err)
	}
	return true, err
}

type windowsService struct {
	root string
}

func (service windowsService) Execute(
	_ []string,
	requests <-chan svc.ChangeRequest,
	status chan<- svc.Status,
) (bool, uint32) {
	accepted := svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}
	logFile, err := openServiceLog(service.root)
	if err != nil {
		return true, 1
	}
	defer logFile.Close()
	serviceLog(logFile, "service handler started")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runResult := make(chan error, 1)
	go func() {
		runResult <- runWithRootOutput(ctx, service.root, logFile, logFile)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case request, ok := <-requests:
			if !ok {
				serviceLog(logFile, "service control channel closed")
				cancel()
				return false, 0
			}
			switch request.Cmd {
			case svc.Interrogate:
				status <- svc.Status{State: svc.Running, Accepts: accepted}
			case svc.Stop, svc.Shutdown:
				serviceLog(logFile, "service stop requested")
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-runResult
				if errors.Is(err, context.Canceled) {
					serviceLog(logFile, "gateway stopped after cancellation")
					return false, 0
				}
				if err != nil {
					serviceLog(logFile, fmt.Sprintf("gateway stopped with error: %s", err))
					return true, 1
				}
				serviceLog(logFile, "gateway stopped")
				return false, 0
			}
		case err := <-runResult:
			status <- svc.Status{State: svc.Stopped}
			if err != nil && !errors.Is(err, context.Canceled) {
				serviceLog(logFile, fmt.Sprintf("gateway stopped with error: %s", err))
				return true, 1
			}
			serviceLog(logFile, "gateway stopped")
			return false, 0
		}
	}
}

func openServiceLog(root string) (*os.File, error) {
	if root == "" {
		root = config.DefaultRoot()
	}
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(root, "logs", "service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func appendServiceLog(root string, err error) {
	if err == nil {
		return
	}
	file, openErr := openServiceLog(root)
	if openErr != nil {
		return
	}
	defer file.Close()
	serviceLog(file, fmt.Sprintf("service dispatcher error: %s", err))
}

func serviceLog(file *os.File, message string) {
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), message)
}
