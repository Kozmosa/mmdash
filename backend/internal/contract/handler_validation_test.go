package contract_test

import (
	"testing"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

func TestArticlePublicationAndReleaseNotesMayBeEmpty(t *testing.T) {
	empty := ""
	publication := contract.CreateArticlePublicationRequest{
		DraftRevision:    1,
		Message:          "publish",
		TemplateID:       "00000000-0000-4000-8000-000000000001",
		Engine:           "auto",
		BibliographyTool: "auto",
		Tag:              "v1",
		Title:            "Paper",
		Notes:            &empty,
		IdempotencyKey:   "publication-1",
	}
	if err := publication.Validate(); err != nil {
		t.Fatalf("empty publication notes should be valid: %v", err)
	}

	release := contract.CreateArticleReleaseRequest{
		CommitID: "00000000-0000-4000-8000-000000000002",
		BuildID:  "00000000-0000-4000-8000-000000000003",
		Tag:      "v1",
		Title:    "Paper",
		Notes:    &empty,
	}
	if err := release.Validate(); err != nil {
		t.Fatalf("empty release notes should be valid: %v", err)
	}

	publication.Notes = nil
	if err := publication.Validate(); err != nil {
		t.Fatalf("omitted publication notes should be valid: %v", err)
	}
	release.Notes = nil
	if err := release.Validate(); err != nil {
		t.Fatalf("omitted release notes should be valid: %v", err)
	}
}
