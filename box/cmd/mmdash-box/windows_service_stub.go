//go:build !windows

package main

func runAsServiceIfNeeded(_ []string) (bool, error) {
	return false, nil
}
