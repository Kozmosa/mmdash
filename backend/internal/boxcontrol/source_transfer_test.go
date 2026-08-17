package boxcontrol

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSourceTransferSignerBindsBoxTaskCommitAndExpiry(t *testing.T) {
	signer, err := NewSourceTransferSigner(strings.Repeat("s", 32), "https://mmdash.example")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	task := Task{
		ID:             "00000000-0000-4000-8000-000000000001",
		BoxID:          "00000000-0000-4000-8000-000000000002",
		ProjectID:      "00000000-0000-4000-8000-000000000003",
		ExecutionEpoch: "00000000-0000-4000-8000-000000000004",
	}
	commit := strings.Repeat("a", 40)
	grant, err := signer.Sign(task, commit, now)
	if err != nil || grant.SourceCommit != commit || !grant.ExpiresAt.Equal(now.Add(sourceTransferTTL)) {
		t.Fatalf("sign source transfer: %#v %v", grant, err)
	}
	token := strings.TrimPrefix(grant.URL, "https://mmdash.example/v1/box-source-transfers/")
	claims, err := signer.Verify(token, now.Add(time.Minute))
	if err != nil || claims.TaskID != task.ID || claims.ExecutionEpoch != task.ExecutionEpoch || claims.SourceCommit != commit {
		t.Fatalf("verify source transfer: %#v %v", claims, err)
	}
	if _, err := signer.Verify(token+"tampered", now); err == nil {
		t.Fatal("tampered source transfer token was accepted")
	}
	if _, err := signer.Verify(token, grant.ExpiresAt); !errors.Is(err, ErrSourceTransferExpired) {
		t.Fatalf("expired source transfer token: %v", err)
	}
}
