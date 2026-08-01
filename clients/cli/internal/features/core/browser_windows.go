//go:build windows

package core

import "os/exec"

func openBrowser(target string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
}
