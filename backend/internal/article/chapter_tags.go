package article

import (
	"context"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/project"
)

func validChapterTagStatus(status string) bool {
	return status == ChapterTagUnedited || status == ChapterTagUnreviewed || status == ChapterTagReviewed || status == ChapterTagNeedsReview
}

func editableChapterTagStatus(status string) bool {
	return status == ChapterTagUnedited || status == ChapterTagUnreviewed
}

// reconcileChapterTagState is the content-identity rule shared by draft
// persistence and its tests. A missing heading keeps the old binding so the
// UI can explain why the tag became stale instead of silently deleting it.
func reconcileChapterTagState(item ChapterTag, current Block, currentExists bool) (string, string, string, bool) {
	if !currentExists {
		return ChapterTagNeedsReview, "heading_missing_or_id_changed", item.HeadingBlockType, true
	}
	if current.NodeType != "heading" {
		return ChapterTagNeedsReview, "heading_type_changed", current.NodeType, true
	}
	fingerprint := chapterHeadingFingerprint(current)
	if item.HeadingBlockType != current.NodeType || item.HeadingFingerprint != fingerprint {
		if item.HeadingBlockType != current.NodeType {
			return ChapterTagNeedsReview, "heading_type_changed", current.NodeType, true
		}
		if item.Status == ChapterTagReviewed || item.Status == ChapterTagNeedsReview {
			return ChapterTagNeedsReview, "heading_content_changed", current.NodeType, true
		}
		if strings.TrimSpace(current.Text) == "" {
			return ChapterTagUnedited, "", current.NodeType, true
		}
		return ChapterTagUnreviewed, "", current.NodeType, true
	}
	return item.Status, item.StaleReason, item.HeadingBlockType, false
}

func initialChapterTagStatus(block Block) string {
	if strings.TrimSpace(block.Text) == "" {
		return ChapterTagUnedited
	}
	return ChapterTagUnreviewed
}

// ensureChapterTags backfills headings from drafts that predate the chapter
// tag table. CreateChapterTag is idempotent on (project, heading block), so a
// concurrent flush or aggregate read cannot create duplicates.
func (service *Service) ensureChapterTags(ctx context.Context, projectID, actorID string, draft Draft, existing []ChapterTag) ([]ChapterTag, error) {
	byHeading := make(map[string]ChapterTag, len(existing))
	for _, item := range existing {
		byHeading[item.HeadingBlockID] = item
	}
	for _, block := range draft.Blocks {
		if block.NodeType != "heading" {
			continue
		}
		if _, ok := byHeading[block.BlockID]; ok {
			continue
		}
		id, err := service.Generator.New()
		if err != nil {
			return existing, err
		}
		item, _, err := service.Store.CreateChapterTag(ctx, ChapterTag{
			ChapterTagID:       id,
			HeadingBlockID:     block.BlockID,
			HeadingBlockType:   block.NodeType,
			HeadingFingerprint: chapterHeadingFingerprint(block),
			ProjectID:          projectID,
			Status:             initialChapterTagStatus(block),
			UpdatedAt:          service.now(),
			UpdatedBy:          actorID,
		})
		if err != nil {
			return existing, err
		}
		byHeading[block.BlockID] = item
		existing = append(existing, item)
	}
	return existing, nil
}

func findHeadingBlock(draft Draft, blockID string) (Block, bool) {
	for _, block := range draft.Blocks {
		if block.BlockID == blockID {
			return block, block.NodeType == "heading"
		}
	}
	return Block{}, false
}

func (service *Service) ListChapterTags(ctx context.Context, caller auth.Identity, projectID string) ([]ChapterTag, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return nil, err
	}
	return service.Store.ListChapterTags(ctx, projectID)
}

func (service *Service) GetChapterTag(ctx context.Context, caller auth.Identity, projectID, chapterTagID string) (ChapterTag, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleRead); err != nil {
		return ChapterTag{}, err
	}
	if strings.TrimSpace(chapterTagID) == "" {
		return ChapterTag{}, ErrInvalid
	}
	return service.Store.GetChapterTag(ctx, projectID, chapterTagID)
}

func (service *Service) CreateChapterTag(ctx context.Context, caller auth.Identity, projectID, headingBlockID, status string) (ChapterTag, bool, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return ChapterTag{}, false, err
	}
	if strings.TrimSpace(headingBlockID) == "" || (status != "" && !editableChapterTagStatus(status)) {
		return ChapterTag{}, false, ErrInvalid
	}
	draft, err := service.Store.GetDraft(ctx, projectID)
	if err != nil {
		return ChapterTag{}, false, err
	}
	block, ok := findHeadingBlock(draft, headingBlockID)
	if !ok {
		return ChapterTag{}, false, ErrConflict
	}
	if status == "" {
		status = initialChapterTagStatus(block)
	}
	id, err := service.Generator.New()
	if err != nil {
		return ChapterTag{}, false, err
	}
	now := service.now()
	return service.Store.CreateChapterTag(ctx, ChapterTag{
		ChapterTagID:       id,
		HeadingBlockID:     headingBlockID,
		HeadingBlockType:   block.NodeType,
		HeadingFingerprint: chapterHeadingFingerprint(block),
		ProjectID:          projectID,
		Status:             status,
		UpdatedAt:          now,
		UpdatedBy:          caller.ActorID(),
	})
}

func (service *Service) UpdateChapterTag(ctx context.Context, caller auth.Identity, projectID, chapterTagID, status string) (ChapterTag, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return ChapterTag{}, err
	}
	if strings.TrimSpace(chapterTagID) == "" || !editableChapterTagStatus(status) {
		return ChapterTag{}, ErrInvalid
	}
	return service.Store.UpdateChapterTag(ctx, projectID, chapterTagID, status, caller.ActorID())
}

func (service *Service) DeleteChapterTag(ctx context.Context, caller auth.Identity, projectID, chapterTagID string) error {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return err
	}
	if strings.TrimSpace(chapterTagID) == "" {
		return ErrInvalid
	}
	return service.Store.DeleteChapterTag(ctx, projectID, chapterTagID, caller.ActorID())
}

func (service *Service) ReviewChapterTag(ctx context.Context, caller auth.Identity, projectID, chapterTagID string) (ChapterTag, error) {
	if err := service.authorize(ctx, caller, projectID, project.PermissionArticleEdit); err != nil {
		return ChapterTag{}, err
	}
	if strings.TrimSpace(chapterTagID) == "" {
		return ChapterTag{}, ErrInvalid
	}
	return service.Store.ReviewChapterTag(ctx, projectID, chapterTagID, caller.ActorID())
}
