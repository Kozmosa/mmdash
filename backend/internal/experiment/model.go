// Package experiment owns frozen experiment specifications, lifecycle state,
// result pointers, retry lineage, and the human/API read surface.
package experiment

import (
	"fmt"
	"time"

	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
)

const (
	TypeBox   = "box"
	TypeBoxRe = "box-re"
	TypeSelf  = "self"

	StatusCreated          = "created"
	StatusQueued           = "queued"
	StatusPreparing        = "preparing"
	StatusRunning          = "running"
	StatusUploading        = "uploading"
	StatusProcessingResult = "processing_result"
	StatusAwaitingResult   = "awaiting_result"
	StatusVerifyingResult  = "verifying_result"
	StatusSucceeded        = "succeeded"
	StatusFailed           = "failed"
	StatusCanceled         = "canceled"
	StatusTimedOut         = "timed_out"
	StatusArchived         = "archived"

	ConnectivityOnline     = "online"
	ConnectivityBoxOffline = "box_offline"

	RetryWarningCode     = "EXPERIMENT_HAS_NEWER_RETRY"
	ResultBranch         = "result"
	ManifestSchema       = "https://mmdash.dev/schemas/v0.1/manifest.schema.json"
	PointerPath          = ".mmdash/artifacts.json"
	JobTypeResultProcess = "experiment.result.process"
)

type ResourceLimits = boxcontrol.ResourceLimits
type Failure = boxcontrol.Failure
type ArtifactPointer = boxcontrol.ArtifactPointer

type Settings struct {
	ProjectID                  string         `json:"project_id"`
	Timezone                   string         `json:"timezone"`
	DefaultRuntimePolicy       string         `json:"default_runtime_policy"`
	DefaultLimits              ResourceLimits `json:"default_limits"`
	GitLargeFileThresholdBytes int64          `json:"git_large_file_threshold_bytes"`
	UpdatedBy                  string         `json:"updated_by"`
	UpdatedAt                  time.Time      `json:"updated_at"`
}

type SettingsPatch struct {
	Timezone                   *string
	DefaultRuntimePolicy       *string
	DefaultLimits              *ResourceLimits
	GitLargeFileThresholdBytes *int64
}

type ResultContract struct {
	Branch                     string `json:"branch"`
	Directory                  string `json:"directory"`
	ManifestSchema             string `json:"manifest_schema"`
	GitLargeFileThresholdBytes int64  `json:"git_large_file_threshold_bytes"`
	ArtifactPointerPath        string `json:"artifact_pointer_path"`
	PushRequired               bool   `json:"push_required"`
	BindTool                   string `json:"bind_tool"`
	Instructions               string `json:"instructions"`
}

type Retry struct {
	RetryOfExperimentID      string `json:"retry_of_experiment_id,omitempty"`
	RootExperimentID         string `json:"root_experiment_id"`
	SupersededByExperimentID string `json:"superseded_by_experiment_id,omitempty"`
	LatestExperimentID       string `json:"latest_experiment_id"`
	RetrySequence            int    `json:"retry_sequence"`
	WarningCode              string `json:"warning_code,omitempty"`
}

type ResultFile struct {
	Path              string `json:"path"`
	StorageKind       string `json:"storage_kind"`
	SHA256            string `json:"sha256"`
	SizeBytes         int64  `json:"size_bytes"`
	MediaType         string `json:"media_type"`
	ArtifactID        string `json:"artifact_id,omitempty"`
	ArtifactVersionID string `json:"artifact_version_id,omitempty"`
	RepositoryPath    string `json:"repository_path,omitempty"`
}

type ResultBundle struct {
	ExperimentID         string           `json:"experiment_id"`
	ExecutionStatus      string           `json:"execution_status"`
	ResultDirectory      string           `json:"result_directory"`
	ResultCommitSHA      string           `json:"result_commit_sha,omitempty"`
	ResultManifestSHA256 string           `json:"result_manifest_sha256,omitempty"`
	Manifest             map[string]any   `json:"manifest,omitempty"`
	ExecutionBundle      *ArtifactPointer `json:"execution_bundle,omitempty"`
	Files                []ResultFile     `json:"files"`
	Retry                Retry            `json:"retry"`
	Summary              string           `json:"summary,omitempty"`
	Analysis             string           `json:"analysis,omitempty"`
}

type Experiment struct {
	ID                     string                 `json:"experiment_id"`
	ProjectID              string                 `json:"project_id"`
	Name                   string                 `json:"name"`
	Type                   string                 `json:"experiment_type"`
	ExecutionStatus        string                 `json:"execution_status"`
	ConnectivityStatus     string                 `json:"connectivity_status,omitempty"`
	CreatedBy              string                 `json:"created_by"`
	SourceCommit           string                 `json:"source_commit"`
	Entrypoint             string                 `json:"entrypoint"`
	Parameters             map[string]interface{} `json:"parameters"`
	Environment            map[string]string      `json:"environment"`
	Inputs                 map[string]interface{} `json:"inputs"`
	RequestedRuntimePolicy string                 `json:"requested_runtime_policy"`
	RequestedBoxID         string                 `json:"requested_box_id,omitempty"`
	ActualRuntime          string                 `json:"actual_runtime,omitempty"`
	RuntimeVersion         string                 `json:"runtime_version,omitempty"`
	Limits                 ResourceLimits         `json:"limits"`
	BoxID                  string                 `json:"box_id,omitempty"`
	TaskID                 string                 `json:"task_id,omitempty"`
	ExitCode               *int                   `json:"exit_code,omitempty"`
	Failure                *Failure               `json:"failure,omitempty"`
	ResourceUsage          map[string]interface{} `json:"resource_usage,omitempty"`
	Summary                string                 `json:"summary,omitempty"`
	ProjectTimezone        string                 `json:"project_timezone"`
	ResultDirectory        string                 `json:"result_directory"`
	ResultCommitSHA        string                 `json:"result_commit_sha,omitempty"`
	StagingCommitSHA       string                 `json:"-"`
	StagingPaths           []string               `json:"-"`
	RevertCommitSHA        string                 `json:"-"`
	ExecutionBundle        *ArtifactPointer       `json:"execution_bundle,omitempty"`
	ResultManifestSHA256   string                 `json:"result_manifest_sha256,omitempty"`
	ResultContract         *ResultContract        `json:"result_contract,omitempty"`
	Retry                  Retry                  `json:"retry"`
	LogsTruncated          bool                   `json:"logs_truncated"`
	LogsTruncatedAt        *time.Time             `json:"logs_truncated_at,omitempty"`
	Progress               int                    `json:"progress"`
	CreatedAt              time.Time              `json:"created_at"`
	StartedAt              *time.Time             `json:"started_at,omitempty"`
	FinishedAt             *time.Time             `json:"finished_at,omitempty"`
	UpdatedAt              time.Time              `json:"updated_at"`
	IdempotencyKey         string                 `json:"-"`
	RunIdempotencyKey      string                 `json:"-"`
	ResultBindIdempotency  string                 `json:"-"`
	MaxAttempts            int                    `json:"-"`
	ResultManifest         map[string]interface{} `json:"-"`
	ResultAnalysis         string                 `json:"-"`
	GitLargeFileThreshold  int64                  `json:"-"`
}

type Page struct {
	Items      []Experiment `json:"items"`
	HasMore    bool         `json:"has_more"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type Comparison struct {
	Items []Experiment `json:"items"`
}

type RerunOverrides struct {
	Name                   *string
	SourceCommit           *string
	Entrypoint             *string
	Parameters             *map[string]interface{}
	Environment            *map[string]string
	Inputs                 *map[string]interface{}
	RequestedRuntimePolicy *string
	RequestedBoxID         *string
	Limits                 *ResourceLimits
	IdempotencyKey         string
}

type ResultVerification struct {
	CommitSHA      string
	ManifestSHA256 string
	Manifest       map[string]interface{}
	Files          []ResultFile
	Summary        string
	Analysis       string
}

func (item Experiment) resultContract() *ResultContract {
	if item.Type != TypeSelf {
		return nil
	}
	return &ResultContract{
		Branch:                     ResultBranch,
		Directory:                  item.ResultDirectory,
		ManifestSchema:             ManifestSchema,
		GitLargeFileThresholdBytes: item.GitLargeFileThreshold,
		ArtifactPointerPath:        PointerPath,
		PushRequired:               true,
		BindTool:                   "experiment.result.bind",
		Instructions: fmt.Sprintf(
			"将结果写入 %s（manifest.json 必须符合 %s）。小于 %d 字节的文件直接提交到 %s 分支；更大的文件先通过 artifact.upload 上传，并在 %s 中记录 path、artifact_id、artifact_version_id、sha256、size 和 media_type。完成后必须 commit 并 push，再调用 experiment.result.bind，传入远端可获取的完整 commit SHA。绑定验证成功前实验不会显示为完成。",
			item.ResultDirectory, ManifestSchema, item.GitLargeFileThreshold,
			ResultBranch, PointerPath,
		),
	}
}

func progressFor(status string) int {
	switch status {
	case StatusCreated:
		return 0
	case StatusQueued, StatusAwaitingResult:
		return 10
	case StatusPreparing:
		return 25
	case StatusRunning:
		return 50
	case StatusUploading:
		return 75
	case StatusProcessingResult, StatusVerifyingResult:
		return 90
	case StatusSucceeded, StatusFailed, StatusCanceled, StatusTimedOut, StatusArchived:
		return 100
	default:
		return 0
	}
}

func decorate(item Experiment) Experiment {
	item.Progress = progressFor(item.ExecutionStatus)
	if item.Retry.RootExperimentID == "" {
		item.Retry.RootExperimentID = item.ID
	}
	if item.Retry.LatestExperimentID == "" {
		item.Retry.LatestExperimentID = item.ID
	}
	if item.Retry.LatestExperimentID != item.ID {
		item.Retry.WarningCode = RetryWarningCode
	}
	item.ResultContract = item.resultContract()
	return item
}
