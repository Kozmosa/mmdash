package gitcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClientBoundsOutputRedactsCredentialsAndReportsTimeout(t *testing.T) {
	client, err := NewClient(os.Args[0], "askpass", 2*time.Second, 1, 16)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	result, err := client.Run(context.Background(), Command{
		Args:        []string{"-test.run=TestGitClientHelper", "--", "secret"},
		Credentials: &Credentials{Token: "top-secret"},
		Directory:   t.TempDir(),
		Operation:   "test.secret",
	})
	if err != nil {
		t.Fatalf("run redaction helper: %v", err)
	}
	if strings.Contains(string(result.Stdout), "top-secret") ||
		!strings.Contains(string(result.Stdout), "[REDACTED]") {
		t.Fatalf("credential was not redacted: %q", result.Stdout)
	}

	_, err = client.Run(context.Background(), Command{
		Args:      []string{"-test.run=TestGitClientHelper", "--", "large"},
		Directory: t.TempDir(),
		Operation: "test.large",
	})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("large output should be bounded: %v", err)
	}

	_, err = client.Run(context.Background(), Command{
		Args:      []string{"-test.run=TestGitClientHelper", "--", "sleep"},
		Directory: t.TempDir(),
		Operation: "test.timeout",
		Timeout:   20 * time.Millisecond,
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("slow command should time out: %v", err)
	}
}

func TestGitClientHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	switch os.Args[separator+1] {
	case "secret":
		_, _ = fmt.Fprint(os.Stdout, "top-secret")
	case "large":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", 1024))
	case "sleep":
		time.Sleep(time.Second)
	}
	os.Exit(0)
}
