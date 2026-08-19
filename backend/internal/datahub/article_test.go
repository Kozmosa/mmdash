package datahub

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

func TestArticleProjectionCoversThePublicDataHubTypes(t *testing.T) {
	for _, item := range []struct {
		eventType, idField, objectType, status string
	}{
		{"article.block.reviewed", "block_id", "article_block", "active"},
		{"article.commit.created", "commit_id", "article_commit", "committed"},
		{"article.build.completed", "build_id", "article_build", "succeeded"},
		{"article.release.created", "release_id", "article_release", "released"},
	} {
		object := articleProjection(contract.EventEnvelope{EventType: item.eventType, OccurredAt: time.Now(), Payload: map[string]interface{}{item.idField: "object-1", "status": item.status, "tag": "v1"}})
		if object.objectType != item.objectType || object.sourceID != "object-1" || object.status != item.status {
			t.Fatalf("%s projection = %#v", item.eventType, object)
		}
	}
}

func TestArticleBlockTitlesAreUTF8Safe(t *testing.T) {
	value := strings.Repeat("论文", 100)
	truncated := truncateRunes(value, 160)
	if !utf8.ValidString(truncated) || len([]rune(truncated)) != 160 {
		t.Fatalf("invalid UTF-8 truncation: %q", truncated)
	}
}
