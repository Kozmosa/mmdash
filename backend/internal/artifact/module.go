package artifact

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/project"
)

// Module exposes Artifact control and signed streaming routes.
type Module struct {
	Service Service
}

func (Module) Name() string { return "artifact" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/artifact-transfers/", module.handleTransfer)
}

// ProjectHandler is mounted by Project because Go 1.17 ServeMux cannot
// register two independent handlers for the same /v1/projects/ subtree.
func (module Module) ProjectHandler() http.Handler {
	return http.HandlerFunc(module.handleProjectResource)
}

func (module Module) handleProjectResource(
	response http.ResponseWriter,
	request *http.Request,
) {
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	segments := strings.Split(
		strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/projects/"), "/"),
		"/",
	)
	if len(segments) < 2 || segments[0] == "" || segments[1] != "artifacts" {
		writeArtifactError(response, request, ErrNotFound)
		return
	}
	projectID := segments[0]
	remaining := segments[2:]
	if len(remaining) == 0 {
		module.handleCollection(response, request, identity, projectID, false)
		return
	}
	switch remaining[0] {
	case "trash":
		if len(remaining) == 1 {
			module.handleCollection(response, request, identity, projectID, true)
			return
		}
	case "uploads":
		module.handleUploads(response, request, identity, projectID, remaining[1:])
		return
	default:
		module.handleArtifact(
			response, request, identity, projectID, remaining[0], remaining[1:],
		)
		return
	}
	writeArtifactError(response, request, ErrNotFound)
}

func (module Module) handleCollection(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	trash bool,
) {
	switch request.Method {
	case http.MethodGet:
		limit, err := queryArtifactLimit(request)
		if err != nil {
			writeArtifactError(response, request, ErrInvalid)
			return
		}
		page, err := module.Service.List(
			request.Context(), identity, projectID, ListFilter{
				Cursor: request.URL.Query().Get("cursor"),
				Kind:   request.URL.Query().Get("kind"),
				Limit:  limit,
				Source: request.URL.Query().Get("source"),
				Status: request.URL.Query().Get("status"),
				Tag:    request.URL.Query().Get("tag"),
				Trash:  trash,
			},
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, page)
	default:
		writeArtifactError(response, request, methodNotAllowed("GET"))
	}
}

func (module Module) handleUploads(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	segments []string,
) {
	if len(segments) == 0 || segments[0] == "" {
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.ArtifactInitializeUploadRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		upload, err := module.Service.Initialize(
			request.Context(), identity, projectID, InitializeUploadInput{
				Filename: body.Filename, Name: optionalString(body.Name),
				SizeBytes: body.SizeBytes, SHA256: body.Sha256,
				MIMEType: optionalString(body.MimeType), Kind: body.Kind,
				Tags: optionalStrings(body.Tags), Description: body.Description,
				IdempotencyKey: body.IdempotencyKey,
			},
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, upload)
		return
	}
	uploadID := segments[0]
	if len(segments) == 1 {
		switch request.Method {
		case http.MethodGet:
			upload, err := module.Service.GetUpload(
				request.Context(), identity, projectID, uploadID,
			)
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, upload)
		case http.MethodDelete:
			if err := module.Service.Abort(
				request.Context(), identity, projectID, uploadID,
			); err != nil {
				writeArtifactError(response, request, err)
				return
			}
			response.WriteHeader(http.StatusNoContent)
		default:
			writeArtifactError(response, request, methodNotAllowed("GET, DELETE"))
		}
		return
	}
	if len(segments) == 3 &&
		segments[1] == "parts" &&
		segments[2] == "sign" {
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.ArtifactSignPartsRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		partNumbers := make([]int, len(body.PartNumbers))
		for index, partNumber := range body.PartNumbers {
			partNumbers[index] = int(partNumber)
		}
		grants, err := module.Service.SignParts(
			request.Context(), identity, projectID, uploadID, partNumbers,
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, grants)
		return
	}
	if len(segments) == 2 && segments[1] == "confirm" {
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body confirmUploadRequest
		if !httpx.DecodeJSONLimit(response, request, &body, 2*1024*1024) {
			return
		}
		parts := make([]ConfirmPart, len(body.Parts))
		for index, part := range body.Parts {
			parts[index] = ConfirmPart{
				PartNumber: part.PartNumber, ETag: part.ETag,
			}
		}
		detail, created, err := module.Service.Confirm(
			request.Context(), identity, projectID, uploadID, parts,
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		httpx.WriteJSON(response, status, detail)
		return
	}
	writeArtifactError(response, request, ErrNotFound)
}

func (module Module) handleArtifact(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	artifactID string,
	segments []string,
) {
	if len(segments) == 0 {
		switch request.Method {
		case http.MethodGet:
			detail, err := module.Service.Get(
				request.Context(), identity, projectID, artifactID, false,
			)
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, detail)
		case http.MethodPatch:
			module.handleUpdate(response, request, identity, projectID, artifactID)
		case http.MethodDelete:
			if err := module.Service.Trash(
				request.Context(), identity, projectID, artifactID,
			); err != nil {
				writeArtifactError(response, request, err)
				return
			}
			response.WriteHeader(http.StatusNoContent)
		default:
			writeArtifactError(
				response, request, methodNotAllowed("GET, PATCH, DELETE"),
			)
		}
		return
	}
	switch segments[0] {
	case "download":
		if len(segments) == 1 {
			module.handleDownload(
				response, request, identity, projectID, artifactID, "",
			)
			return
		}
	case "restore":
		if len(segments) == 1 &&
			httpx.RequireMethod(response, request, http.MethodPost) {
			detail, err := module.Service.Restore(
				request.Context(), identity, projectID, artifactID,
			)
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, detail)
		}
		return
	case "purge":
		if len(segments) == 1 &&
			httpx.RequireMethod(response, request, http.MethodDelete) {
			if err := module.Service.Purge(
				request.Context(), identity, projectID, artifactID,
			); err != nil {
				writeArtifactError(response, request, err)
				return
			}
			response.WriteHeader(http.StatusNoContent)
		}
		return
	case "versions":
		module.handleVersions(
			response, request, identity, projectID, artifactID, segments[1:],
		)
		return
	}
	writeArtifactError(response, request, ErrNotFound)
}

func (module Module) handleVersions(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	artifactID string,
	segments []string,
) {
	if len(segments) == 0 {
		if !httpx.RequireMethod(response, request, http.MethodGet) {
			return
		}
		versions, err := module.Service.ListVersions(
			request.Context(), identity, projectID, artifactID,
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, versions)
		return
	}
	if len(segments) == 1 && segments[0] == "uploads" {
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.ArtifactInitializeVersionUploadRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		upload, err := module.Service.InitializeVersion(
			request.Context(), identity, projectID, artifactID,
			InitializeVersionInput{
				Filename: body.Filename, SizeBytes: body.SizeBytes,
				SHA256: body.Sha256, MIMEType: optionalString(body.MimeType),
				IdempotencyKey: body.IdempotencyKey,
			},
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, upload)
		return
	}
	if len(segments) == 2 {
		versionID := segments[0]
		switch segments[1] {
		case "download":
			module.handleDownload(
				response, request, identity, projectID, artifactID, versionID,
			)
			return
		case "restore":
			if !httpx.RequireMethod(response, request, http.MethodPost) {
				return
			}
			var body contract.ArtifactRestoreVersionRequest
			if !httpx.DecodeJSON(response, request, &body) {
				return
			}
			detail, err := module.Service.RestoreVersion(
				request.Context(), identity, projectID, artifactID,
				versionID, body.IdempotencyKey,
			)
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusCreated, detail)
			return
		}
	}
	writeArtifactError(response, request, ErrNotFound)
}

func (module Module) handleUpdate(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	artifactID string,
) {
	var body updateRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	input, err := body.input()
	if err != nil {
		writeArtifactError(response, request, ErrInvalid)
		return
	}
	detail, err := module.Service.Update(
		request.Context(), identity, projectID, artifactID, input,
	)
	if err != nil {
		writeArtifactError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, detail)
}

func (module Module) handleDownload(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	artifactID string,
	versionID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	grant, err := module.Service.Download(
		request.Context(), identity, projectID, artifactID, versionID,
	)
	if err != nil {
		writeArtifactError(response, request, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(response, http.StatusOK, grant)
}

func (module Module) handleTransfer(
	response http.ResponseWriter,
	request *http.Request,
) {
	raw := strings.TrimPrefix(request.URL.Path, "/v1/artifact-transfers/")
	token, err := transferToken(raw)
	if err != nil {
		writeArtifactError(response, request, ErrNotFound)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	switch request.Method {
	case http.MethodPut:
		contentLength, err := parseContentLength(
			request.Header.Get("Content-Length"),
		)
		if err != nil {
			writeArtifactError(response, request, ErrPartInvalid)
			return
		}
		part, err := module.Service.PutSignedPart(
			request.Context(), token, request.Body, contentLength,
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		response.Header().Set("ETag", `"`+normalizeETag(part.ETag)+`"`)
		response.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		reader, version, err := module.Service.OpenSignedDownload(
			request.Context(), token,
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		defer reader.Close()
		response.Header().Set("Content-Type", version.MIMEType)
		response.Header().Set(
			"Content-Disposition", contentDisposition(version.Filename),
		)
		response.Header().Set(
			"Content-Length", strconv.FormatInt(version.SizeBytes, 10),
		)
		response.WriteHeader(http.StatusOK)
		buffer := make([]byte, 64*1024)
		_, _ = io.CopyBuffer(response, reader, buffer)
	default:
		writeArtifactError(response, request, methodNotAllowed("GET, PUT"))
	}
}

func (module Module) identity(
	response http.ResponseWriter,
	request *http.Request,
) (auth.Identity, bool) {
	identity, err := module.Service.Authenticate(
		request.Context(), request.Header.Get("Authorization"),
	)
	if err != nil {
		writeArtifactError(response, request, err)
		return auth.Identity{}, false
	}
	return identity, true
}

type confirmUploadPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

type confirmUploadRequest struct {
	Parts []confirmUploadPart `json:"parts"`
}

func (body confirmUploadRequest) Validate() error {
	if len(body.Parts) < 1 || len(body.Parts) > MultipartMaxParts {
		return ErrInvalid
	}
	for _, part := range body.Parts {
		if part.PartNumber < 1 || part.PartNumber > MultipartMaxParts ||
			strings.TrimSpace(part.ETag) == "" ||
			len(part.ETag) > 1024 {
			return ErrInvalid
		}
	}
	return nil
}

type updateRequest struct {
	Name        *string         `json:"name,omitempty"`
	Kind        *string         `json:"kind,omitempty"`
	Tags        *[]string       `json:"tags,omitempty"`
	Description json.RawMessage `json:"description,omitempty"`
}

func (body updateRequest) Validate() error {
	if body.Name == nil && body.Kind == nil &&
		body.Tags == nil && len(body.Description) == 0 {
		return ErrInvalid
	}
	return nil
}

func (body updateRequest) input() (UpdateInput, error) {
	input := UpdateInput{Name: body.Name, Kind: body.Kind, Tags: body.Tags}
	if len(body.Description) > 0 {
		if string(body.Description) == "null" {
			var value *string
			input.Description = &value
		} else {
			var value string
			if err := json.Unmarshal(body.Description, &value); err != nil {
				return UpdateInput{}, err
			}
			pointer := &value
			input.Description = &pointer
		}
	}
	return input, nil
}

func queryArtifactLimit(request *http.Request) (int, error) {
	value := request.URL.Query().Get("limit")
	if value == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > 200 {
		return 0, ErrInvalid
	}
	return limit, nil
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalStrings(value *[]string) []string {
	if value == nil {
		return []string{}
	}
	return *value
}

func methodNotAllowed(allow string) error {
	return apperror.New(
		http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
		"Method not allowed",
	).WithDetails(map[string]interface{}{"allow": allow})
}

func writeArtifactError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		if applicationError.Status == http.StatusMethodNotAllowed {
			if details, ok := applicationError.Details.(map[string]interface{}); ok {
				if allow, ok := details["allow"].(string); ok {
					response.Header().Set("Allow", allow)
				}
			}
		}
		httpx.WriteError(response, request, applicationError)
		return
	}
	var safeError *SafeError
	if errors.As(err, &safeError) {
		status := http.StatusConflict
		switch safeError.Code {
		case "ARTIFACT_NOT_FOUND":
			status = http.StatusNotFound
		case "ARTIFACT_TOO_LARGE":
			status = http.StatusRequestEntityTooLarge
		case "ARTIFACT_KIND_INVALID", "ARTIFACT_SOURCE_INVALID",
			"ARTIFACT_TAG_INVALID", "ARTIFACT_MIME_NOT_ALLOWED",
			"ARTIFACT_PART_INVALID":
			status = http.StatusBadRequest
		case "ARTIFACT_STORAGE_UNAVAILABLE":
			status = http.StatusServiceUnavailable
		}
		httpx.WriteError(response, request, apperror.New(
			status, safeError.Code, safeError.Message,
		))
		return
	}
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		httpx.WriteError(response, request, apperror.New(
			http.StatusUnauthorized, "UNAUTHENTICATED",
			"Authentication is required",
		))
	case errors.Is(err, project.ErrForbidden), errors.Is(err, ErrForbidden):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden, "FORBIDDEN", "Artifact permission denied",
		))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound, "ARTIFACT_NOT_FOUND", "Artifact was not found",
		))
	case errors.Is(err, ErrTransferExpired):
		httpx.WriteError(response, request, apperror.New(
			http.StatusGone, "ARTIFACT_UPLOAD_EXPIRED",
			"Artifact transfer grant has expired",
		))
	case errors.Is(err, ErrNotTrashed):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_NOT_TRASHED",
			"Artifact is not in the recycle bin",
		))
	case errors.Is(err, ErrUploadConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_UPLOAD_CONFLICT",
			"Artifact upload conflicts with current state",
		))
	case errors.Is(err, ErrUploadExpired):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_UPLOAD_EXPIRED",
			"Artifact upload has expired",
		))
	case errors.Is(err, ErrUploadAborted):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_UPLOAD_ABORTED",
			"Artifact upload was aborted",
		))
	case errors.Is(err, ErrUploadIncomplete):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_UPLOAD_INCOMPLETE",
			"Artifact upload is incomplete",
		))
	case errors.Is(err, ErrPartMissing):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_PART_MISSING",
			"Artifact upload part is missing",
		))
	case errors.Is(err, ErrSizeMismatch):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_SIZE_MISMATCH",
			"Artifact size does not match",
		))
	case errors.Is(err, ErrHashMismatch):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_HASH_MISMATCH",
			"Artifact hash does not match",
		))
	case errors.Is(err, ErrNotAvailable):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_NOT_AVAILABLE",
			"Artifact is not available",
		))
	case errors.Is(err, ErrPurgeConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_PURGE_CONFLICT",
			"Artifact purge conflicts with current references",
		))
	case errors.Is(err, ErrStorage):
		httpx.WriteError(response, request, apperror.New(
			http.StatusServiceUnavailable, "ARTIFACT_STORAGE_UNAVAILABLE",
			"Artifact storage is temporarily unavailable",
		))
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrPartInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "INVALID_REQUEST",
			"Artifact input is invalid",
		))
	default:
		httpx.WriteError(response, request, err)
	}
}
