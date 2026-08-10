package hermes

import (
	"errors"
	"strings"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

func TestValidateProfileMatchesHermesCanonicalIdentifiers(t *testing.T) {
	valid := []string{
		"default",
		"a",
		"research",
		"research-2",
		"research_agent",
		"chat", // Hermes subcommands are not profile-reserved names.
		"profile",
		strings.Repeat("a", 64),
		"", // omitted profile selects Hermes' unscoped/default API.
	}
	for _, profile := range valid {
		t.Run("valid/"+profile, func(t *testing.T) {
			if err := validateProfile(profile); err != nil {
				t.Fatalf("validateProfile(%q) = %v, want valid", profile, err)
			}
		})
	}
}

func TestValidateProfileRejectsNonCanonicalAndReservedIdentifiers(t *testing.T) {
	invalid := []string{
		"Default",
		"Research",
		" research",
		"research ",
		"research.profile",
		"research/profile",
		"research\\profile",
		"-research",
		"_research",
		"research profile",
		strings.Repeat("a", 65),
		"hermes",
		"test",
		"tmp",
		"root",
		"sudo",
	}
	for _, profile := range invalid {
		t.Run("invalid/"+profile, func(t *testing.T) {
			err := validateProfile(profile)
			if !errors.Is(err, agent.ErrInvalidArgument) {
				t.Fatalf("validateProfile(%q) = %v, want ErrInvalidArgument", profile, err)
			}
		})
	}
}

func TestFactoryDoesNotNormalizeProfileIdentifiers(t *testing.T) {
	factory := NewFactory(FactoryOptions{})
	for _, profile := range []string{" research", "Research", "research.profile", "research/profile"} {
		_, err := factory(nil, agent.AdapterConfig{
			InstanceID: "instance-1",
			Values: map[string]string{
				ConfigRuntimeURL: "https://runtime.example.test",
				ConfigAPIKey:     "runtime-secret",
				ConfigProfile:    profile,
			},
		})
		if !errors.Is(err, agent.ErrInvalidArgument) {
			t.Fatalf("factory accepted non-canonical profile %q: %v", profile, err)
		}
	}
}
