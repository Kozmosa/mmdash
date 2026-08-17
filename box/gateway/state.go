package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mmdash/mmdash/box/contracts"
)

const stateSchemaVersion = 2

type statusCallback struct {
	Status        string                 `json:"status"`
	OccurredAt    time.Time              `json:"occurred_at"`
	ExitCode      *int                   `json:"exit_code,omitempty"`
	Failure       *contracts.Failure     `json:"failure,omitempty"`
	ResourceUsage map[string]interface{} `json:"resource_usage,omitempty"`
	Summary       string                 `json:"summary,omitempty"`
}

type taskState struct {
	Task                   contracts.Task             `json:"task"`
	Phase                  string                     `json:"phase"`
	StartedAt              time.Time                  `json:"started_at,omitempty"`
	RuntimeStarted         bool                       `json:"runtime_started"`
	RuntimeFinished        bool                       `json:"runtime_finished"`
	NextSequence           int64                      `json:"next_sequence"`
	Acknowledged           int64                      `json:"acknowledged_sequence"`
	Logs                   []contracts.LogEntry       `json:"logs,omitempty"`
	LogBytes               int64                      `json:"log_bytes"`
	LogsTruncated          bool                       `json:"logs_truncated"`
	TruncationAcknowledged bool                       `json:"truncation_acknowledged"`
	TruncatedAt            *time.Time                 `json:"truncated_at,omitempty"`
	PendingStatuses        []statusCallback           `json:"pending_statuses,omitempty"`
	BundlePath             string                     `json:"bundle_path,omitempty"`
	BundleSHA256           string                     `json:"bundle_sha256,omitempty"`
	BundleSize             int64                      `json:"bundle_size,omitempty"`
	ManifestSHA256         string                     `json:"manifest_sha256,omitempty"`
	Artifact               *contracts.ArtifactPointer `json:"artifact,omitempty"`
	ResultPending          bool                       `json:"result_pending"`
	TerminalOnly           bool                       `json:"terminal_only"`
}

func (state *taskState) bundleState() string {
	switch {
	case state.Artifact != nil:
		return "uploaded"
	case state.BundlePath != "":
		return "ready"
	default:
		return "none"
	}
}

type persistedState struct {
	SchemaVersion  int                   `json:"schema_version"`
	InstallationID string                `json:"installation_id"`
	BoxID          string                `json:"box_id,omitempty"`
	BoxToken       string                `json:"box_token,omitempty"`
	Tasks          map[string]*taskState `json:"tasks"`
}

// Identity is the durable account-level identity used by the Gateway. The
// mbox account command uses these helpers so account binding can be completed
// without starting a worker process.
type Identity struct {
	InstallationID string `json:"installation_id"`
	BoxID          string `json:"box_id,omitempty"`
	BoxToken       string `json:"box_token,omitempty"`
}

func LoadIdentity(path string) (Identity, error) {
	state, err := loadState(path)
	if err != nil {
		return Identity{}, err
	}
	return Identity{InstallationID: state.InstallationID, BoxID: state.BoxID, BoxToken: state.BoxToken}, nil
}

func SaveIdentity(path string, identity Identity) error {
	state, err := loadState(path)
	if err != nil {
		return err
	}
	state.InstallationID = identity.InstallationID
	state.BoxID = identity.BoxID
	state.BoxToken = identity.BoxToken
	return saveState(path, state)
}

func loadState(path string) (persistedState, error) {
	state := persistedState{SchemaVersion: stateSchemaVersion, Tasks: map[string]*taskState{}}
	if path == "" {
		return state, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("decode Box state: %w", err)
	}
	if state.SchemaVersion != stateSchemaVersion {
		return state, fmt.Errorf("unsupported Box state schema %d", state.SchemaVersion)
	}
	if state.Tasks == nil {
		state.Tasks = map[string]*taskState{}
	}
	return state, nil
}

func saveState(path string, state persistedState) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary := path + ".tmp-" + randomSuffix()
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
