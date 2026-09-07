package agent

import (
	"fmt"
	"regexp"
)

var hermesProfileID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

var hermesReservedProfiles = map[string]struct{}{
	"hermes": {}, "test": {}, "tmp": {}, "root": {}, "sudo": {},
}

// ValidateHermesProfile enforces the canonical profile identifier accepted by
// Hermes. An empty value selects Hermes' unscoped API; explicit identifiers
// select /p/{profile} routes and must already be lowercase.
func ValidateHermesProfile(profile string) error {
	if profile == "" || profile == "default" {
		return nil
	}
	if !hermesProfileID.MatchString(profile) {
		return fmt.Errorf("%w: invalid Hermes profile", ErrInvalidArgument)
	}
	if _, reserved := hermesReservedProfiles[profile]; reserved {
		return fmt.Errorf("%w: reserved Hermes profile", ErrInvalidArgument)
	}
	return nil
}
