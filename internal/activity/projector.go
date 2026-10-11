package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

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
			ActorUserID uint64 `json:"actor_user_id,string"`
		}
		if err := json.Unmarshal(event.Payload, &actorPayload); err != nil {
			return fmt.Errorf("decode activity actor: %w", err)
		}
		var actor *uint64
		if actorPayload.ActorUserID > 0 {
			value := actorPayload.ActorUserID
			actor = &value
		}
		eventID := event.ID
		return p.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
			_, _, err := p.repo.Append(ctx, exec, Input{
				TenantID:      event.TenantID,
				ObjectType:    event.AggregateType,
				ObjectID:      event.AggregateID,
				Type:          entryType,
				ActorUserID:   actor,
				Summary:       summary,
				Data:          event.Payload,
				SourceEventID: &eventID,
				EventTime:     event.CreatedAt,
			})
			return err
		})
	}
}
