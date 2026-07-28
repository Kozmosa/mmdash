package outbox

import (
	"context"
	"errors"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

// Record is a durable Outbox envelope plus publication state.
type Record struct {
	Attempts       int                    `json:"attempts"`
	AvailableAt    time.Time              `json:"available_at"`
	Envelope       contract.EventEnvelope `json:"event"`
	FailedAt       *time.Time             `json:"failed_at,omitempty"`
	LastError      string                 `json:"last_error,omitempty"`
	LeaseExpiresAt *time.Time             `json:"lease_expires_at,omitempty"`
	LockedBy       string                 `json:"locked_by,omitempty"`
	MaxAttempts    int                    `json:"max_attempts"`
	PublishedAt    *time.Time             `json:"published_at,omitempty"`
	Status         string                 `json:"status"`
}

// Delivery is one durable consumer attempt stream for an event or replay.
type Delivery struct {
	Attempts       int                    `json:"attempts"`
	AvailableAt    time.Time              `json:"available_at"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	ConsumerName   string                 `json:"consumer_name"`
	CreatedAt      time.Time              `json:"created_at"`
	DeliveryKey    string                 `json:"delivery_key"`
	Envelope       contract.EventEnvelope `json:"-"`
	ID             string                 `json:"id"`
	LastError      string                 `json:"last_error,omitempty"`
	LeaseExpiresAt *time.Time             `json:"lease_expires_at,omitempty"`
	LockedBy       string                 `json:"locked_by,omitempty"`
	MaxAttempts    int                    `json:"max_attempts"`
	Status         string                 `json:"status"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// Replay records an explicit operator request to redeliver one event.
type Replay struct {
	ConsumerName string    `json:"consumer_name,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	EventID      string    `json:"event_id"`
	ID           string    `json:"id"`
	Reason       string    `json:"reason"`
	RequestedBy  string    `json:"requested_by"`
}

// Failure is one append-only consumer attempt failure.
type Failure struct {
	Attempt      int       `json:"attempt"`
	ConsumerName string    `json:"consumer_name"`
	DeliveryID   string    `json:"delivery_id"`
	DeliveryKey  string    `json:"delivery_key"`
	ErrorMessage string    `json:"error_message"`
	EventID      string    `json:"event_id"`
	FailedAt     time.Time `json:"failed_at"`
	ID           string    `json:"id"`
}

// State is the inspectable Outbox event and all of its deliveries.
type State struct {
	Deliveries []Delivery `json:"deliveries"`
	Failures   []Failure  `json:"failures"`
	Record     Record     `json:"record"`
	Replays    []Replay   `json:"replays"`
}

// DeliveryStore is the persistence boundary used by Processor.
type DeliveryStore interface {
	ClaimDelivery(context.Context, string, time.Duration) (*Delivery, error)
	ClaimEvent(context.Context, string, time.Duration) (*Record, error)
	CompleteDelivery(context.Context, string, string) error
	FailDelivery(context.Context, Delivery, string, time.Duration) error
	FailEvent(context.Context, Record, string, string, time.Duration) error
	Publish(context.Context, Record, string, []string) error
}

var (
	ErrLeaseLost   = errors.New("outbox lease lost")
	ErrNoConsumers = errors.New("no matching event consumers")
	ErrNotFound    = errors.New("outbox event not found")
)
