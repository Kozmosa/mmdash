// Package clock provides an injectable UTC clock.
package clock

import "time"

// Clock supplies the current time.
type Clock interface {
	Now() time.Time
}

// System is the production UTC clock.
type System struct{}

// Now returns the current time in UTC.
func (System) Now() time.Time {
	return time.Now().UTC()
}

// Fixed is a deterministic test clock.
type Fixed struct {
	Time time.Time
}

// Now returns the configured instant in UTC.
func (clock Fixed) Now() time.Time {
	return clock.Time.UTC()
}
