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
	mux.HandleFunc("/v1/box/releases", module.handleBoxReleases)
	mux.HandleFunc(
		"/v1/internal/artifact-preview-jobs/",
		module.handlePreviewJobTransfer,
	)
	mux.HandleFunc(
		"/v1/internal/artifact-semantic-jobs/",
		module.handleSemanticJobExecute,
	)
}

func (module Module) handleBoxReleases(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	if identity.Kind != "session" && identity.Kind != "api" {
		writeArtifactError(response, request, project.ErrForbidden)
		return
	}
	catalog, err := module.Service.ListBoxReleases(request.Context())
	if err != nil {
		writeArtifactError(response, request, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(response, http.StatusOK, catalog)
}

// ProjectHandler is mounted by Project so the shared /v1/projects/ subtree
// keeps one explicit dispatcher and one module owner for each child route.
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
	case "folders":
		module.handleFolders(response, request, identity, projectID, remaining[1:])
		return
	case "trash":
		if len(remaining) == 1 {
			module.handleCollection(response, request, identity, projectID, true)
			return
		}
	case "uploads":
		module.handleUploads(response, request, identity, projectID, remaining[1:])
		return
	case "agent-uploads":
		module.handleAgentUploadInitialize(
			response, request, identity, projectID, remaining[1:],
		)
		return
	default:
		module.handleArtifact(
			response, request, identity, projectID, remaining[0], remaining[1:],
		)
		return
	}
	writeArtifactError(response, request, ErrNotFound)
}

func (module Module) handleFolders(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	segments []string,
) {
	if len(segments) == 0 {
		switch request.Method {
		case http.MethodGet:
			tree, err := module.Service.ListFolders(request.Context(), identity, projectID)
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, tree)
		case http.MethodPost:
			var body artifactFolderCreateRequest
			if !httpx.DecodeJSON(response, request, &body) {
				return
			}
			folder, err := module.Service.CreateFolder(request.Context(), identity, projectID, CreateFolderInput{
				Name: body.Name, ParentFolderID: body.ParentFolderID,
			})
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusCreated, folder)
		default:
			writeArtifactError(response, request, methodNotAllowed("GET, POST"))
		}
		return
	}
	if len(segments) == 1 {
		folderID := segments[0]
		switch request.Method {
		case http.MethodPatch:
			var body artifactFolderRenameRequest
			if !httpx.DecodeJSON(response, request, &body) {
				return
			}
			folder, err := module.Service.RenameFolder(request.Context(), identity, projectID, folderID, body.Name)
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, folder)
		case http.MethodDelete:
			recursive := false
			if raw := request.URL.Query().Get("recursive"); raw != "" {
				parsed, parseErr := strconv.ParseBool(raw)
				if parseErr != nil {
					writeArtifactError(response, request, ErrInvalid)
					return
				}
				recursive = parsed
			}
			if err := module.Service.DeleteFolder(request.Context(), identity, projectID, folderID, recursive); err != nil {
				writeArtifactError(response, request, err)
				return
			}
			response.WriteHeader(http.StatusNoContent)
		default:
			writeArtifactError(response, request, methodNotAllowed("PATCH, DELETE"))
		}
		return
	}
	if len(segments) == 2 && segments[1] == "move" && request.Method == http.MethodPost {
		var body artifactFolderMoveRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		input, err := body.input()
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		folder, err := module.Service.MoveFolder(request.Context(), identity, projectID, segments[0], input)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, folder)
		return
	}
	writeArtifactError(response, request, ErrNotFound)
}

func (module Module) handleAgentUploadInitialize(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	segments []string,
) {
	if len(segments) != 0 {
		writeArtifactError(response, request, ErrNotFound)
		return
	}
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.AgentArtifactInitializeUploadRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	upload, err := module.Service.Initialize(
		request.Context(), identity, projectID, InitializeUploadInput{
			AgentSessionID: optionalString(body.AgentSessionID),
			AgentRunID:     optionalString(body.AgentRunID),
			Filename:       body.Filename, Name: optionalString(body.Name),
			SizeBytes: body.SizeBytes, SHA256: body.Sha256,
			MIMEType: optionalString(body.MimeType), Kind: KindAgent,
			Tags: optionalStrings(body.Tags), Description: body.Description,
			IdempotencyKey: body.IdempotencyKey,
		},
	)
	if err != nil {
		writeArtifactError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusCreated, upload)
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
	case "description":
		if len(segments) == 1 && httpx.RequireMethod(response, request, http.MethodPost) {
			var body semanticDescriptionRequest
			if request.ContentLength != 0 && !httpx.DecodeJSON(response, request, &body) {
				return
			}
			job, err := module.Service.RequestSemanticDescription(request.Context(), identity, projectID, artifactID, SemanticDescriptionInput{AgentInstanceID: body.AgentInstanceID})
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusAccepted, job)
		}
		return
	case "folder":
		if len(segments) == 1 && httpx.RequireMethod(response, request, http.MethodPut) {
			var body artifactMoveFolderRequest
			if !httpx.DecodeJSON(response, request, &body) {
				return
			}
			folderID, err := body.folderID()
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			detail, err := module.Service.MoveArtifact(request.Context(), identity, projectID, artifactID, folderID)
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, detail)
		}
		return
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
		case "previews":
			if !httpx.RequireMethod(response, request, http.MethodGet) {
				return
			}
			previews, err := module.Service.ListPreviews(
				request.Context(), identity, projectID, artifactID, versionID,
			)
			if err != nil {
				writeArtifactError(response, request, err)
				return
			}
			response.Header().Set("Cache-Control", "no-store")
			httpx.WriteJSON(response, http.StatusOK, previews)
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
		part, err := module.Service.PutSignedTransfer(
			request.Context(), token, request.Body, contentLength,
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		response.Header().Set("ETag", `"`+normalizeETag(part.ETag)+`"`)
		response.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		reader, content, err := module.Service.OpenSignedTransfer(
			request.Context(), token,
		)
		if err != nil {
			writeArtifactError(response, request, err)
			return
		}
		defer reader.Close()
		response.Header().Set("Content-Type", content.MIMEType)
		response.Header().Set(
			"Content-Disposition", contentDisposition(content.Filename),
		)
		response.Header().Set(
			"Content-Length", strconv.FormatInt(content.SizeBytes, 10),
		)
		response.WriteHeader(http.StatusOK)
		buffer := make([]byte, 64*1024)
		_, _ = io.CopyBuffer(response, reader, buffer)
	default:
		writeArtifactError(response, request, methodNotAllowed("GET, PUT"))
	}
}

func (module Module) handlePreviewJobTransfer(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	segments := strings.Split(
		strings.Trim(
			strings.TrimPrefix(
				request.URL.Path,
				"/v1/internal/artifact-preview-jobs/",
			),
			"/",
		),
		"/",
	)
	if len(segments) != 2 || segments[0] == "" || segments[1] != "transfers" {
		writeArtifactError(response, request, ErrNotFound)
		return
	}
	var body contract.ArtifactPreviewJobTransferRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	grant, err := module.Service.PreviewJobTransfer(
		request.Context(), identity, segments[0], PreviewTransferInput{
			Direction: body.Direction, VersionID: body.VersionID,
			PreviewType: optionalString(body.PreviewType),
			Filename:    optionalString(body.Filename),
			MIMEType:    optionalString(body.MimeType),
			SizeBytes:   optionalInt64(body.SizeBytes),
			SHA256:      optionalString(body.Sha256),
		},
	)
	if err != nil {
		writeArtifactError(response, request, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(response, http.StatusCreated, grant)
}

func (module Module) handleSemanticJobExecute(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/internal/artifact-semantic-jobs/"), "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "execute" {
		writeArtifactError(response, request, ErrNotFound)
		return
	}
	result, err := module.Service.ExecuteSemanticDescription(request.Context(), identity, segments[0])
	if err != nil {
		writeArtifactError(response, request, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(response, http.StatusOK, result)
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

type semanticDescriptionRequest struct {
	AgentInstanceID string `json:"agent_instance_id,omitempty"`
}

type artifactFolderCreateRequest struct {
	Name           string  `json:"name"`
	ParentFolderID *string `json:"parent_folder_id"`
}

type artifactFolderRenameRequest struct {
	Name string `json:"name"`
}

type artifactFolderMoveRequest struct {
	ParentFolderID json.RawMessage `json:"parent_folder_id"`
	Position       *int            `json:"position,omitempty"`
}

type artifactMoveFolderRequest struct {
	FolderID json.RawMessage `json:"folder_id"`
}

func (body artifactFolderMoveRequest) input() (MoveFolderInput, error) {
	parentID, err := nullableFolderID(body.ParentFolderID)
	if err != nil {
		return MoveFolderInput{}, err
	}
	return MoveFolderInput{ParentFolderID: parentID, Position: body.Position}, nil
}

func (body artifactMoveFolderRequest) folderID() (*string, error) {
	return nullableFolderID(body.FolderID)
}

func nullableFolderID(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 {
		return nil, ErrInvalid
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, ErrInvalid
	}
	return value, nil
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

func optionalInt64(value *int64) int64 {
	if value == nil {
		return 0
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
		case "ARTIFACT_STORAGE_UNAVAILABLE", "ARTIFACT_SEMANTIC_UNAVAILABLE":
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
	case errors.Is(err, ErrFolderConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_FOLDER_CONFLICT",
			"Artifact folder name conflicts with a sibling folder",
		))
	case errors.Is(err, ErrFolderHasChildren):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_FOLDER_HAS_CHILDREN",
			"Artifact folder has child folders and cannot be deleted",
		))
	case errors.Is(err, ErrFolderCycle):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "ARTIFACT_FOLDER_CYCLE",
			"Artifact folder cannot be moved below itself",
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
