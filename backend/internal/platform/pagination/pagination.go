// Package pagination defines stable cursor pagination primitives.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

const (
	// DefaultLimit is used when a caller does not request a page size.
	DefaultLimit = 50
	// MaxLimit bounds list responses.
	MaxLimit = 200
)

// Request is the normalized cursor pagination request.
type Request struct {
	Cursor string
	Limit  int
}

// Normalize validates and fills pagination defaults.
func (request Request) Normalize() (Request, error) {
	if request.Limit == 0 {
		request.Limit = DefaultLimit
	}
	if request.Limit < 1 || request.Limit > MaxLimit {
		return Request{}, fmt.Errorf("limit must be between 1 and %d", MaxLimit)
	}
	return request, nil
}

// Cursor is an opaque, versioned continuation value.
type Cursor struct {
	ID        string `json:"id"`
	SortValue string `json:"sort_value"`
	Version   int    `json:"version"`
}

// Encode serializes a continuation cursor.
func Encode(cursor Cursor) (string, error) {
	if cursor.Version == 0 {
		cursor.Version = 1
	}
	if cursor.Version != 1 || cursor.ID == "" || cursor.SortValue == "" {
		return "", fmt.Errorf("invalid cursor")
	}
	contents, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(contents), nil
}

// Decode parses and validates an opaque continuation cursor.
func Decode(value string) (Cursor, error) {
	contents, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	var cursor Cursor
	if err := json.Unmarshal(contents, &cursor); err != nil {
		return Cursor{}, fmt.Errorf("parse cursor: %w", err)
	}
	if cursor.Version != 1 || cursor.ID == "" || cursor.SortValue == "" {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	return cursor, nil
}
