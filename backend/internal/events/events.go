// Package events exposes safe Outbox inspection and replay operations.
package events

import (
	"context"
	"errors"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/eventbus"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// Authenticator resolves trusted caller identity.
type Authenticator interface {
	Authenticate(context.Context, string) (auth.Identity, error)
}

// Writer writes a stable event using the caller's business transaction.
type Writer interface {
	Write(context.Context, transaction.Tx, outbox.Event) (outbox.Event, error)
}

// Store exposes durable Outbox operations used by the API.
type Store interface {
	CreateReplay(
		context.Context,
		string,
		string,
		[]string,
		string,
		string,
	) (outbox.Replay, error)
	GetState(context.Context, string) (outbox.State, error)
}

// ConsumerBus exposes registered in-process consumers.
type ConsumerBus interface {
	Consumers() []eventbus.Consumer
	Matching(string) []string
}

// ConsumerInfo is a handler-free registry projection.
type ConsumerInfo struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
}

// Enqueued identifies a test event committed to system_outbox.
type Enqueued struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
}

// Service applies system-administrator policy to operational event controls.
type Service struct {
	Auth        Authenticator
	Bus         ConsumerBus
	Outbox      Writer
	Store       Store
	Transaction transaction.Manager
}

// Authenticate resolves one request identity.
func (service Service) Authenticate(
	ctx context.Context,
	authorization string,
) (auth.Identity, error) {
	return service.Auth.Authenticate(ctx, authorization)
}

// EmitTest commits a stable engineering event through the normal Outbox writer.
func (service Service) EmitTest(
	ctx context.Context,
	identity auth.Identity,
	message string,
	payload map[string]interface{},
) (Enqueued, error) {
	if err := authorizeAdmin(identity); err != nil {
		return Enqueued{}, err
	}
	message = strings.TrimSpace(message)
	if message == "" || len(message) > 500 {
		return Enqueued{}, ErrInvalid
	}
	safePayload := make(map[string]interface{}, len(payload)+1)
	for key, value := range payload {
		safePayload[key] = value
	}
	safePayload["message"] = message
	var written outbox.Event
	err := service.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var err error
		written, err = service.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": identity.User.ID},
			EventType: "system.test.emitted",
			Payload:   safePayload,
			Producer:  "system",
		})
		return err
	})
	if err != nil {
		return Enqueued{}, err
	}
	return Enqueued{EventID: written.EventID, Status: "pending"}, nil
}

// Get returns durable publication and delivery state.
func (service Service) Get(
	ctx context.Context,
	identity auth.Identity,
	eventID string,
) (outbox.State, error) {
	if err := authorizeAdmin(identity); err != nil {
		return outbox.State{}, err
	}
	return service.Store.GetState(ctx, eventID)
}

// Consumers returns a deterministic handler-free registry projection.
func (service Service) Consumers(identity auth.Identity) ([]ConsumerInfo, error) {
	if err := authorizeAdmin(identity); err != nil {
		return nil, err
	}
	consumers := service.Bus.Consumers()
	items := make([]ConsumerInfo, 0, len(consumers))
	for _, consumer := range consumers {
		items = append(items, ConsumerInfo{
			Name:     consumer.Name,
			Patterns: append([]string(nil), consumer.Patterns...),
		})
	}
	return items, nil
}

// Replay creates deliveries with a fresh replay idempotency key.
func (service Service) Replay(
	ctx context.Context,
	identity auth.Identity,
	eventID string,
	consumerName string,
	reason string,
) (outbox.Replay, error) {
	if err := authorizeAdmin(identity); err != nil {
		return outbox.Replay{}, err
	}
	reason = strings.TrimSpace(reason)
	consumerName = strings.TrimSpace(consumerName)
	if reason == "" || len(reason) > 500 {
		return outbox.Replay{}, ErrInvalid
	}
	state, err := service.Store.GetState(ctx, eventID)
	if err != nil {
		return outbox.Replay{}, err
	}
	consumers := service.Bus.Matching(state.Record.Envelope.EventType)
	if consumerName != "" {
		matched := false
		for _, registered := range consumers {
			if registered == consumerName {
				matched = true
				break
			}
		}
		if !matched {
			return outbox.Replay{}, outbox.ErrNoConsumers
		}
		consumers = []string{consumerName}
	}
	return service.Store.CreateReplay(
		ctx,
		eventID,
		consumerName,
		consumers,
		identity.User.ID,
		reason,
	)
}

func authorizeAdmin(identity auth.Identity) error {
	if (identity.Kind != "session" && identity.Kind != "api") ||
		identity.User.SystemRole != "admin" {
		return ErrForbidden
	}
	return nil
}

var (
	ErrForbidden = errors.New("event operations require a system administrator")
	ErrInvalid   = errors.New("invalid event operation")
)
