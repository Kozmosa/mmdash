package identity

import (
	"bytes"
	"regexp"
	"testing"
)

func TestGeneratorCreatesVersionFourUUID(t *testing.T) {
	generator := Generator{Reader: bytes.NewReader(make([]byte, 16))}
	value, err := generator.New()
	if err != nil {
		t.Fatalf("generate ID: %v", err)
	}
	pattern := regexp.MustCompile(`^00000000-0000-4000-8000-000000000000$`)
	if !pattern.MatchString(value) {
		t.Fatalf("unexpected UUID: %s", value)
	}
}
