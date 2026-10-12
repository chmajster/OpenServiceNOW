package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Projector struct {
	tx   *database.TxManager
	repo *Repository
}

func NewProjector(tx *database.TxManager, repo *Repository) *Projector {
	return &Projector{tx: tx, repo: repo}
}

func (p *Projector) Handler(entryType, summary string) events.Handler {
	return func(ctx context.Context, event events.Event) error {
		if event.TenantID == 0 {
			return ErrInvalid
		}
		var actorPayload struct {
			ActorUserID uint64     `json:"actor_user_id,string"`
			OccurredAt  *time.Time `json:"occurred_at"`
			Visibility  string     `json:"visibility"`
			MessageType string     `json:"message_type"`
		}
		if err := json.Unmarshal(event.Payload, &actorPayload); err != nil {
			return fmt.Errorf("decode activity actor: %w", err)
		}
		var actor *uint64
		if actorPayload.ActorUserID > 0 {
			value := actorPayload.ActorUserID
			actor = &value
		}
		visibility := "INTERNAL"
		switch event.Type {
		case "incident.created", "incident.updated", "incident.assigned", "incident.state.changed", "incident.reopened", "incident.resolved", "incident.closed", "incident.cancelled", "incident.priority.changed", "work.comment.added":
			visibility = "PUBLIC"
		}
		if actorPayload.Visibility == "PUBLIC" {
			visibility = "PUBLIC"
		}
		if actorPayload.Visibility == "INTERNAL" || actorPayload.MessageType == "WORK_NOTE" || event.Type == "work.work_note.added" {
			visibility = "INTERNAL"
		}
		eventID := event.ID
		eventTime := event.CreatedAt
		if actorPayload.OccurredAt != nil {
			if actorPayload.OccurredAt.IsZero() {
				return ErrInvalid
			}
			eventTime = actorPayload.OccurredAt.UTC()
		}
		return p.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
			_, _, err := p.repo.Append(ctx, exec, Input{
				TenantID:      event.TenantID,
				Visibility:    visibility,
				ObjectType:    event.AggregateType,
				ObjectID:      event.AggregateID,
				Type:          entryType,
				ActorUserID:   actor,
				Summary:       summary,
				Data:          event.Payload,
				SourceEventID: &eventID,
				EventTime:     eventTime,
			})
			return err
		})
	}
}
