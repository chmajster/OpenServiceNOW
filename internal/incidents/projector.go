package incidents

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/events"
)

var EventSummaries = map[string]string{
	"incident.created":          "Utworzono incydent",
	"incident.updated":          "Zmieniono dane incydentu",
	"incident.assigned":         "Zmieniono przypisanie",
	"incident.state.changed":    "Zmieniono stan",
	"incident.reopened":         "Ponownie otwarto incydent",
	"incident.resolved":         "Rozwiązano incydent",
	"incident.closed":           "Zamknięto incydent",
	"incident.cancelled":        "Anulowano incydent",
	"incident.priority.changed": "Zmieniono priorytet",
}

// RegisterEventHandlers is also used by integration tests, so every emitted
// contract is verified against the handlers that cmd/worker actually runs.
func RegisterEventHandlers(dispatcher *events.Dispatcher, repo *Repository, projector *activity.Projector) error {
	if err := dispatcher.Register("incident.first_response", projector.Handler("incident.first_response", "Pierwsza odpowiedź operatora")); err != nil {
		return err
	}
	for eventType, summary := range EventSummaries {
		project := projector.Handler(eventType, summary)
		handler := func(ctx context.Context, event events.Event) error {
			if _, ok := EventSummaries[event.Type]; !ok || event.Version != 1 || event.TenantID == 0 || event.AggregateType != "incident" {
				return events.ErrInvalid
			}
			var change ChangeEvent
			if err := json.Unmarshal(event.Payload, &change); err != nil {
				return fmt.Errorf("decode incident event: %w", err)
			}
			id, err := strconv.ParseUint(event.AggregateID, 10, 64)
			if err != nil || id == 0 || id != change.After.ID || change.ActorUserID == 0 || change.IncidentVersion == 0 || change.IncidentVersion != change.After.Version || change.OccurredAt.IsZero() || !change.OccurredAt.Equal(change.After.UpdatedAt) {
				return events.ErrInvalid
			}
			if event.Type == "incident.created" {
				if change.Before != nil || change.After.Version != 1 {
					return events.ErrInvalid
				}
			} else if change.Before == nil || change.Before.ID != id || change.Before.Version+1 != change.After.Version {
				return events.ErrInvalid
			}
			var exists bool
			if err := repo.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM incidents WHERE tenant_id=? AND id=? AND version>=?)`, event.TenantID, id, change.IncidentVersion).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return events.ErrInvalid
			}
			return project(ctx, event)
		}
		if err := dispatcher.Register(eventType, handler); err != nil {
			return err
		}
	}
	return nil
}
