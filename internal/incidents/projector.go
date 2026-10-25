package incidents

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/events"
)

var EventSummaries = map[string]string{
	"incident.major.action.note.added":    "Dodano notatkę zadania koordynacji",
	"incident.created":                    "Utworzono incydent",
	"incident.updated":                    "Zmieniono dane incydentu",
	"incident.assigned":                   "Zmieniono przypisanie",
	"incident.state.changed":              "Zmieniono stan",
	"incident.reopened":                   "Ponownie otwarto incydent",
	"incident.resolved":                   "Rozwiązano incydent",
	"incident.closed":                     "Zamknięto incydent",
	"incident.cancelled":                  "Anulowano incydent",
	"incident.priority.changed":           "Zmieniono priorytet",
	"incident.major.candidate":            "Zgłoszono kandydaturę dużej awarii",
	"incident.major.promoted":             "Zatwierdzono dużą awarię",
	"incident.major.rejected":             "Odrzucono kandydaturę dużej awarii",
	"incident.major.demoted":              "Zakończono koordynację dużej awarii",
	"incident.major.coordination.changed": "Zmieniono kartę koordynacji dużej awarii",
	"incident.major.update.published":     "Opublikowano aktualizację dużej awarii",
	"incident.major.update.internal":      "Dodano komunikat wewnętrzny dużej awarii",
	"incident.major.restored":             "Potwierdzono przywrócenie usługi",
	"incident.major.review.saved":         "Zapisano przegląd po awarii",
	"incident.major.action.changed":       "Zmieniono działanie po awarii",
	"incident.major.reminder.due":         "Termin aktualizacji dużej awarii",
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
			if strings.HasPrefix(event.Type, "incident.major.") {
				var major MajorDomainEvent
				if err := json.Unmarshal(event.Payload, &major); err != nil {
					return fmt.Errorf("decode major incident event: %w", err)
				}
				id, err := strconv.ParseUint(event.AggregateID, 10, 64)
				if err != nil || id == 0 || id != major.IncidentID || major.CaseVersion == 0 || major.OccurredAt.IsZero() || (major.ActorUserID == 0 && event.Type != "incident.major.reminder.due") {
					return events.ErrInvalid
				}
				var exists bool
				if err := repo.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM incident_major_cases WHERE tenant_id=? AND incident_id=?)`, event.TenantID, id).Scan(&exists); err != nil {
					return err
				}
				if !exists {
					return events.ErrInvalid
				}
				return project(ctx, event)
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
