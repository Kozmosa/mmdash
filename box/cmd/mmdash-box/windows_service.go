//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

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
	return true, svc.Run(mbox.ServiceName, windowsService{root: options.root})
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runResult := make(chan error, 1)
	go func() {
		runResult <- runWithRoot(ctx, service.root)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case request, ok := <-requests:
			if !ok {
				cancel()
				return false, 0
			}
			switch request.Cmd {
			case svc.Interrogate:
				status <- svc.Status{State: svc.Running, Accepts: accepted}
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-runResult
				if errors.Is(err, context.Canceled) {
					return false, 0
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return true, 1
				}
				return false, 0
			}
		case err := <-runResult:
			status <- svc.Status{State: svc.Stopped}
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, err)
				return true, 1
			}
			return false, 0
		}
	}
}
