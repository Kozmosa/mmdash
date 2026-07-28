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
		entry[key] = Sanitize(key, value)
	}
	contents, err := json.Marshal(entry)
	if err != nil {
		return
	}
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	_, _ = logger.writer.Write(append(contents, '\n'))
}

// Sanitize recursively redacts values whose keys indicate credentials.
func Sanitize(key string, value interface{}) interface{} {
	normalized := strings.ToLower(key)
	for _, fragment := range []string{
		"access_key", "api_key", "apikey", "authorization", "connection_string",
		"cookie", "credential", "database_url", "dsn", "passphrase",
		"password", "private_key", "secret", "token",
	} {
		if strings.Contains(normalized, fragment) {
			return "[REDACTED]"
		}
	}
	switch values := value.(type) {
	case map[string]interface{}:
		sanitized := make(map[string]interface{}, len(values))
		for nestedKey, nestedValue := range values {
			sanitized[nestedKey] = Sanitize(nestedKey, nestedValue)
		}
		return sanitized
	case map[string]string:
		sanitized := make(map[string]string, len(values))
		for nestedKey, nestedValue := range values {
			clean := Sanitize(nestedKey, nestedValue)
			sanitized[nestedKey], _ = clean.(string)
		}
		return sanitized
	case []interface{}:
		sanitized := make([]interface{}, len(values))
		for index, nestedValue := range values {
			sanitized[index] = Sanitize(key, nestedValue)
		}
		return sanitized
	}
	return value
}
