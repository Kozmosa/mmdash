//go:build linux

package credentials

import (
	"errors"
	"os/exec"
	"strings"
)

func platformSet(profile string, value string) error {
	command := exec.Command("secret-tool", "store", "--label=mmdash CLI", "service", "mmdash-cli", "profile", profile)
	command.Stdin = strings.NewReader(value)
	if err := command.Run(); err != nil {
		return errors.New("Linux Secret Service is unavailable; install secret-tool and unlock a keyring")
	}
	return nil
}

func platformGet(profile string) (string, error) {
	output, err := exec.Command("secret-tool", "lookup", "service", "mmdash-cli", "profile", profile).Output()
	if err != nil || len(output) == 0 {
		return "", ErrNotFound
	}
	return strings.TrimSpace(string(output)), nil
}

func platformDelete(profile string) error {
	err := exec.Command("secret-tool", "clear", "service", "mmdash-cli", "profile", profile).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil
	}
	return err
}
