package workrecords

import (
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/events"
)

// Shared production registration is used unchanged by integration tests.
func RegisterActivityHandlers(dispatcher *events.Dispatcher, projector *activity.Projector) error {
	handlers := map[string]struct{ kind, summary string }{
		"work.assignment.changed": {"assignment", "Zmieniono przypisanie"},
		"work.comment.added":      {"comment", "Odpowiedź dla zgłaszającego"},
		"work.work_note.added":    {"work_note", "Notatka wewnętrzna"},
		"work.watcher.added":      {"watcher", "Dodano obserwatora"},
		"work.watcher.removed":    {"watcher", "Usunięto obserwatora"},
		"work.relation.added":     {"relation", "Dodano powiązany incydent"},
		"work.relation.removed":   {"relation", "Usunięto powiązanie"},
		"work.attachment.added":   {"attachment", "Dodano załącznik"},
		"work.attachment.removed": {"attachment", "Usunięto załącznik"},
	}
	for kind, m := range handlers {
		if err := dispatcher.Register(kind, projector.Handler(m.kind, m.summary)); err != nil {
			return err
		}
	}
	return nil
}
