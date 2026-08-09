package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mmdash/mmdash/clients/cli/internal/app"
	"github.com/mmdash/mmdash/clients/cli/internal/credentials"
	"github.com/mmdash/mmdash/clients/cli/internal/features/core"
	modelFeature "github.com/mmdash/mmdash/clients/cli/internal/features/model"
	"github.com/mmdash/mmdash/clients/cli/internal/features/project"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	application, err := app.New(app.Options{
		CredentialStore: credentials.NewSystemStore(),
		Features: []app.Feature{
			core.Feature{},
			project.Feature{},
			modelFeature.Feature{},
		},
		Stderr:  os.Stderr,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Version: version,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "INTERNAL_ERROR: %v\n", err)
		os.Exit(1)
	}
	os.Exit(application.Run(ctx, os.Args[1:]))
}
