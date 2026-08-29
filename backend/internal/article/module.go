package article

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

type Module struct{ Service *Service }

func (Module) Name() string                        { return "article" }
func (module Module) ProjectHandler() http.Handler { return http.HandlerFunc(module.handleProject) }
func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/internal/article-build-jobs/", module.handleWorker)
}

func (module Module) handleProject(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/projects/"), "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "article" {
		writeError(w, r, ErrNotFound)
		return
	}
	caller, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	projectID := parts[0]
	tail := parts[2:]
	if len(tail) == 0 && r.Method == http.MethodGet {
		value, err := module.Service.Aggregate(r.Context(), caller, projectID)
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(tail) == 1 {
		switch tail[0] {
		case "draft":
			if r.Method == http.MethodGet {
				value, err := module.Service.Draft(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusOK, value, err)
				return
			}
		case "chapter-tags":
			if r.Method == http.MethodGet {
				value, err := module.Service.ListChapterTags(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusOK, Page[ChapterTag]{Items: value}, err)
				return
			}
			if r.Method == http.MethodPost {
				var body contract.CreateArticleChapterTagRequest
				if !decode(w, r, &body) {
					return
				}
				status := ""
				if body.Status != nil {
					status = *body.Status
				}
				value, _, err := module.Service.CreateChapterTag(r.Context(), caller, projectID, body.HeadingBlockID, status)
				writeResult(w, r, http.StatusCreated, value, err)
				return
			}
		case "patches":
			if r.Method == http.MethodGet {
				value, err := module.Service.ListPatches(r.Context(), caller, projectID, r.URL.Query().Get("status"))
				writeResult(w, r, http.StatusOK, Page[Patch]{Items: value}, err)
				return
			}
			if r.Method == http.MethodPost {
				var body contract.CreateArticlePatchRequest
				if !decode(w, r, &body) {
					return
				}
				value, err := module.Service.CreatePatch(r.Context(), caller, projectID, body.BaseRevision, body.Patch, body.Rationale, body.Provenance)
				writeResult(w, r, http.StatusCreated, value, err)
				return
			}
		case "references":
			if r.Method == http.MethodGet {
				value, err := module.Service.ListReferences(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusOK, Page[Reference]{Items: value}, err)
				return
			}
			if r.Method == http.MethodPost {
				var body contract.CreateArticleReferenceRequest
				if !decode(w, r, &body) {
					return
				}
				item := Reference{ReferenceType: body.ReferenceType, SourceObjectID: body.SourceObjectID, SourceVersionID: body.SourceVersionID, Title: body.Title}
				if body.CitationKey != nil {
					item.CitationKey = *body.CitationKey
				}
				if body.Metadata != nil {
					item.Metadata = *body.Metadata
				}
				value, _, err := module.Service.CreateReference(r.Context(), caller, projectID, item)
				writeResult(w, r, http.StatusCreated, value, err)
				return
			}
		case "commits":
			if r.Method == http.MethodGet {
				value, err := module.Service.ListCommits(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusOK, Page[Commit]{Items: value}, err)
				return
			}
			if r.Method == http.MethodPost {
				var body contract.CreateArticleCommitRequest
				if !decode(w, r, &body) {
					return
				}
				value, err := module.Service.Commit(r.Context(), caller, projectID, body.DraftRevision, body.Message)
				writeResult(w, r, http.StatusCreated, value, err)
				return
			}
		case "builds":
			if r.Method == http.MethodGet {
				value, err := module.Service.ListBuilds(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusOK, Page[Build]{Items: value}, err)
				return
			}
			if r.Method == http.MethodPost {
				var body contract.CreateArticleBuildRequest
				if !decode(w, r, &body) {
					return
				}
				value, _, err := module.Service.CreateBuild(r.Context(), caller, projectID, body.CommitID, body.TemplateID, body.Engine, body.BibliographyTool, body.IdempotencyKey)
				writeResult(w, r, http.StatusAccepted, value, err)
				return
			}
		case "preview-builds":
			if r.Method == http.MethodPost {
				var body contract.CreateArticlePreviewBuildRequest
				if !decode(w, r, &body) {
					return
				}
				value, _, err := module.Service.CreatePreview(r.Context(), caller, projectID, body.DraftRevision, body.TemplateID, body.Engine, body.BibliographyTool)
				writeResult(w, r, http.StatusAccepted, value, err)
				return
			}
		case "releases":
			if r.Method == http.MethodGet {
				value, err := module.Service.ListReleases(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusOK, Page[Release]{Items: value}, err)
				return
			}
			if r.Method == http.MethodPost {
				var body contract.CreateArticleReleaseRequest
				if !decode(w, r, &body) {
					return
				}
				value, _, err := module.Service.CreateRelease(r.Context(), caller, projectID, body.CommitID, body.BuildID, body.Tag, body.Title, body.Notes)
				writeResult(w, r, http.StatusCreated, value, err)
				return
			}
		case "templates":
			if r.Method == http.MethodGet {
				value, err := module.Service.ListTemplates(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusOK, Page[Template]{Items: value}, err)
				return
			}
			if r.Method == http.MethodPost {
				var body contract.RegisterArticleTemplateRequest
				if !decode(w, r, &body) {
					return
				}
				value, _, err := module.Service.RegisterTemplate(r.Context(), caller, projectID, body.ArtifactID, body.VersionID, body.Manifest)
				writeResult(w, r, http.StatusAccepted, value, err)
				return
			}
		case "publications":
			if r.Method == http.MethodPost {
				var body contract.CreateArticlePublicationRequest
				if !decode(w, r, &body) {
					return
				}
				value, _, err := module.Service.Publish(r.Context(), caller, projectID, PublicationInput{DraftRevision: body.DraftRevision, Message: body.Message, TemplateID: body.TemplateID, Engine: body.Engine, BibliographyTool: body.BibliographyTool, Tag: body.Tag, Title: body.Title, Notes: body.Notes, IdempotencyKey: body.IdempotencyKey})
				writeResult(w, r, http.StatusAccepted, value, err)
				return
			}
		case "zotero":
			switch r.Method {
			case http.MethodGet:
				value, err := module.Service.GetZotero(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusOK, value, err)
				return
			case http.MethodPut:
				var body contract.UpdateArticleZoteroBindingRequest
				if !decode(w, r, &body) {
					return
				}
				collection := ""
				if body.CollectionKey != nil {
					collection = *body.CollectionKey
				}
				value, err := module.Service.UpdateZotero(r.Context(), caller, projectID, body.LibraryType, body.LibraryID, collection, body.APIKey)
				writeResult(w, r, http.StatusOK, value, err)
				return
			case http.MethodDelete:
				err := module.Service.DeleteZotero(r.Context(), caller, projectID)
				writeResult(w, r, http.StatusNoContent, nil, err)
				return
			}
		}
	}
	if len(tail) == 2 && tail[0] == "draft" && tail[1] == "flush" && r.Method == http.MethodPut {
		var body contract.PersistArticleDraftRequest
		if !decode(w, r, &body) {
			return
		}
		value, err := module.Service.PersistDraft(r.Context(), caller, projectID, PersistDraftInput{ExpectedRevision: body.ExpectedRevision, YjsUpdate: body.YjsUpdate, StateVector: body.StateVector, TiptapJSON: body.TiptapJson, ActorKind: body.ActorKind, Provenance: body.Provenance})
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(tail) == 2 && tail[0] == "chapter-tags" {
		switch r.Method {
		case http.MethodGet:
			value, err := module.Service.GetChapterTag(r.Context(), caller, projectID, tail[1])
			writeResult(w, r, http.StatusOK, value, err)
			return
		case http.MethodPatch:
			var body contract.UpdateArticleChapterTagRequest
			if !decode(w, r, &body) {
				return
			}
			value, err := module.Service.UpdateChapterTag(r.Context(), caller, projectID, tail[1], body.Status)
			writeResult(w, r, http.StatusOK, value, err)
			return
		case http.MethodDelete:
			err := module.Service.DeleteChapterTag(r.Context(), caller, projectID, tail[1])
			writeResult(w, r, http.StatusNoContent, nil, err)
			return
		}
	}
	if len(tail) == 3 && tail[0] == "chapter-tags" && tail[2] == "review" && r.Method == http.MethodPost {
		value, err := module.Service.ReviewChapterTag(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(tail) == 2 && tail[0] == "references" && r.Method == http.MethodDelete {
		err := module.Service.DeleteReference(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusNoContent, nil, err)
		return
	}
	if len(tail) == 2 && tail[0] == "commits" && r.Method == http.MethodGet {
		value, err := module.Service.CommitDetail(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(tail) == 3 && tail[0] == "commits" && tail[2] == "restore" && r.Method == http.MethodPost {
		value, err := module.Service.RestoreCommit(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(tail) == 3 && tail[0] == "blocks" && tail[2] == "review" && r.Method == http.MethodPost {
		value, err := module.Service.ReviewBlock(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(tail) == 2 && tail[0] == "builds" && r.Method == http.MethodGet {
		value, err := module.Service.GetBuild(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(tail) == 3 && tail[0] == "builds" && tail[2] == "retry" && r.Method == http.MethodPost {
		value, _, err := module.Service.RetryBuild(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusAccepted, value, err)
		return
	}
	if len(tail) == 3 && tail[0] == "publications" && tail[2] == "retry" && r.Method == http.MethodPost {
		value, err := module.Service.RetryPublication(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusAccepted, value, err)
		return
	}
	if len(tail) == 2 && tail[0] == "releases" && r.Method == http.MethodGet {
		value, err := module.Service.GetRelease(r.Context(), caller, projectID, tail[1])
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(tail) == 2 && tail[0] == "zotero" && tail[1] == "collections" && r.Method == http.MethodGet {
		value, err := module.Service.ListZoteroCollections(r.Context(), caller, projectID)
		writeResult(w, r, http.StatusOK, Page[ZoteroCollection]{Items: value}, err)
		return
	}
	if len(tail) == 2 && tail[0] == "zotero" && tail[1] == "items" && r.Method == http.MethodGet {
		value, err := module.Service.ListZoteroItems(r.Context(), caller, projectID, r.URL.Query().Get("collection"), r.URL.Query().Get("q"))
		writeResult(w, r, http.StatusOK, Page[ZoteroItem]{Items: value}, err)
		return
	}
	if len(tail) == 2 && tail[0] == "zotero" && tail[1] == "search" && r.Method == http.MethodGet {
		value, err := module.Service.SearchZotero(r.Context(), caller, projectID, r.URL.Query().Get("q"))
		writeResult(w, r, http.StatusOK, Page[ZoteroItem]{Items: value}, err)
		return
	}
	if len(tail) == 3 && tail[0] == "patches" && tail[2] == "review" && r.Method == http.MethodPost {
		var body contract.ReviewArticlePatchRequest
		if !decode(w, r, &body) {
			return
		}
		var draft *PersistDraftInput
		if body.Decision == "accepted" && body.ExpectedRevision != nil && body.YjsUpdate != nil && body.StateVector != nil && body.TiptapJson != nil {
			draft = &PersistDraftInput{ExpectedRevision: *body.ExpectedRevision, YjsUpdate: *body.YjsUpdate, StateVector: *body.StateVector, TiptapJSON: *body.TiptapJson, ActorKind: "human", Provenance: map[string]interface{}{"patch_id": tail[1]}}
		}
		value, err := module.Service.ReviewPatch(r.Context(), caller, projectID, tail[1], body.Decision, draft)
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	writeError(w, r, ErrNotFound)
}

func (module Module) handleWorker(w http.ResponseWriter, r *http.Request) {
	caller, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/internal/article-build-jobs/"), "/"), "/")
	if len(segments) == 2 && segments[0] != "" && segments[1] == "input" && r.Method == http.MethodGet {
		value, err := module.Service.WorkerInput(r.Context(), caller, segments[0])
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(segments) == 2 && segments[0] != "" && segments[1] == "progress" && r.Method == http.MethodPost {
		var body contract.UpdateArticleBuildProgressRequest
		if !decode(w, r, &body) {
			return
		}
		value, err := module.Service.WorkerProgress(r.Context(), caller, segments[0], int(body.ProgressPercent), body.ProgressStage)
		writeResult(w, r, http.StatusOK, value, err)
		return
	}
	if len(segments) == 3 && segments[0] != "" && segments[1] == "outputs" && r.Method == http.MethodPost {
		size, parseErr := strconv.ParseInt(r.Header.Get("X-Content-Length"), 10, 64)
		if parseErr != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		limited := io.LimitReader(r.Body, size+1)
		value, err := module.Service.WorkerOutput(r.Context(), caller, segments[0], segments[2], r.Header.Get("X-Filename"), r.Header.Get("Content-Type"), r.Header.Get("X-Content-SHA256"), size, limited)
		writeResult(w, r, http.StatusCreated, value, err)
		return
	}
	writeError(w, r, ErrNotFound)
}

type validatable interface{ Validate() error }

func decode[T validatable](w http.ResponseWriter, r *http.Request, body T) bool {
	if !httpx.DecodeJSON(w, r, body) {
		return false
	}
	if body.Validate() != nil {
		writeError(w, r, ErrInvalid)
		return false
	}
	return true
}
func writeResult(w http.ResponseWriter, r *http.Request, status int, value interface{}, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	httpx.WriteJSON(w, status, value)
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "The Article operation failed"
	switch {
	case errors.Is(err, ErrInvalid):
		status, code, message = http.StatusBadRequest, "INVALID_ARTICLE_REQUEST", "Article input is invalid"
	case errors.Is(err, ErrForbidden):
		status, code, message = http.StatusForbidden, "FORBIDDEN", "Article access forbidden"
	case errors.Is(err, ErrNotFound):
		status, code, message = http.StatusNotFound, "ARTICLE_NOT_FOUND", "Article object not found"
	case errors.Is(err, ErrConflict), errors.Is(err, ErrSuperseded):
		status, code, message = http.StatusConflict, "ARTICLE_CONFLICT", "Article state changed; refresh and retry"
	case errors.Is(err, ErrNotReady):
		status, code, message = http.StatusConflict, "ARTICLE_NOT_READY", "Article object is not ready"
	case errors.Is(err, ErrUnavailable):
		status, code, message = http.StatusServiceUnavailable, "ARTICLE_INTEGRATION_UNAVAILABLE", "Article integration is unavailable"
	case errors.Is(err, repo.ErrNotConfigured):
		status, code, message = http.StatusConflict, "ARTICLE_REPOSITORY_NOT_CONFIGURED", "Configure the project repository before creating an Article commit"
	case errors.Is(err, repo.ErrNotReady):
		status, code, message = http.StatusConflict, "ARTICLE_REPOSITORY_NOT_READY", "The Article repository branch is not ready; synchronize Repository settings and retry"
	case errors.Is(err, repo.ErrHeadChanged), errors.Is(err, repo.ErrConflict), errors.Is(err, repo.ErrLocked):
		status, code, message = http.StatusConflict, "ARTICLE_REPOSITORY_CONFLICT", "The Article repository changed; synchronize and retry"
	}
	httpx.WriteError(w, r, apperror.New(status, code, message))
}
