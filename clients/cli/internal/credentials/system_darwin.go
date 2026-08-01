//go:build darwin

package credentials

import (
	"errors"
	"os/exec"
	"strings"
)

func platformSet(profile string, value string) error {
	return exec.Command("security", "add-generic-password", "-U", "-a", profile, "-s", "mmdash-cli", "-w", value).Run()
}

func platformGet(profile string) (string, error) {
	output, err := exec.Command("security", "find-generic-password", "-a", profile, "-s", "mmdash-cli", "-w").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", ErrNotFound
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func platformDelete(profile string) error {
	err := exec.Command("security", "delete-generic-password", "-a", profile, "-s", "mmdash-cli").Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil
	}
	return err
}
