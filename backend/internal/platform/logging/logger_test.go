package logging

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

func TestLoggerWritesJSONAndRedactsSecrets(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, clock.Fixed{Time: time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)})

	logger.Info("service.test", map[string]interface{}{
		"metadata": map[string]interface{}{
			"cookie": "nested-cookie",
			"items": []interface{}{
				map[string]interface{}{"api_key": "nested-key"},
			},
		},
		"request_id": "request-1",
		"token":      "do-not-log",
	})

	contents := output.String()
	if !strings.Contains(contents, `"event":"service.test"`) {
		t.Fatalf("missing event: %s", contents)
	}
	if strings.Contains(contents, "do-not-log") ||
		strings.Contains(contents, "nested-cookie") ||
		strings.Contains(contents, "nested-key") ||
		!strings.Contains(contents, "[REDACTED]") {
		t.Fatalf("secret was not redacted: %s", contents)
	}
}
