// Package identity creates and validates opaque platform identifiers.
package identity

import (
	"crypto/rand"
	"fmt"
	"io"
)

// Generator creates RFC 4122 version 4 UUID strings.
type Generator struct {
	Reader io.Reader
}

// New returns a cryptographically random opaque identifier.
func (generator Generator) New() (string, error) {
	reader := generator.Reader
	if reader == nil {
		reader = rand.Reader
	}
	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}

// MustNew returns a new identifier or panics when system randomness fails.
func (generator Generator) MustNew() string {
	value, err := generator.New()
	if err != nil {
		panic(err)
	}
	return value
}
