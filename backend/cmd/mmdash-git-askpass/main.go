// mmdash-git-askpass provides Git credentials to one explicitly configured
// subprocess without embedding the secret in a URL or command line.
package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	usernameEnvironment = "MMDASH_GIT_USERNAME"
	tokenEnvironment    = "MMDASH_GIT_TOKEN"
)

func main() {
	prompt := strings.Join(os.Args[1:], " ")
	answer, ok := answerPrompt(prompt, os.Getenv(usernameEnvironment), os.Getenv(tokenEnvironment))
	if !ok {
		os.Exit(1)
	}
	_, _ = fmt.Fprint(os.Stdout, answer)
}

func answerPrompt(prompt, username, token string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(prompt))
	switch {
	case strings.Contains(normalized, "username"):
		if strings.TrimSpace(username) == "" {
			username = "x-access-token"
		}
		return username, true
	case strings.Contains(normalized, "password"):
		if token == "" {
			return "", false
		}
		return token, true
	default:
		return "", false
	}
}
