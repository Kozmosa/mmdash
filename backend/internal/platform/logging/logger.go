// Package logging writes structured JSON process logs with key-based redaction.
package logging

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

// Logger writes one JSON object per line.
type Logger struct {
	clock  clock.Clock
	mutex  sync.Mutex
	writer io.Writer
}

// New creates a JSON logger.
func New(writer io.Writer, source clock.Clock) *Logger {
	return &Logger{clock: source, writer: writer}
}

// Info writes an informational event.
func (logger *Logger) Info(event string, fields map[string]interface{}) {
	logger.write("info", event, fields)
}

// Error writes an error event.
func (logger *Logger) Error(event string, fields map[string]interface{}) {
	logger.write("error", event, fields)
}

func (logger *Logger) write(level, event string, fields map[string]interface{}) {
	entry := map[string]interface{}{
		"event":     event,
		"level":     level,
		"timestamp": logger.clock.Now().UTC().Format(time.RFC3339Nano),
	}
	for key, value := range fields {
		entry[key] = redact(key, value)
	}
	contents, err := json.Marshal(entry)
	if err != nil {
		return
	}
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	_, _ = logger.writer.Write(append(contents, '\n'))
}

func redact(key string, value interface{}) interface{} {
	normalized := strings.ToLower(key)
	for _, fragment := range []string{"authorization", "credential", "password", "secret", "token"} {
		if strings.Contains(normalized, fragment) {
			return "[REDACTED]"
		}
	}
	if values, ok := value.(map[string]interface{}); ok {
		sanitized := make(map[string]interface{}, len(values))
		for nestedKey, nestedValue := range values {
			sanitized[nestedKey] = redact(nestedKey, nestedValue)
		}
		return sanitized
	}
	return value
}
