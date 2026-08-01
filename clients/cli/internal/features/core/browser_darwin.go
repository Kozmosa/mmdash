//go:build darwin

package core

import "os/exec"

func openBrowser(target string) error { return exec.Command("open", target).Start() }
