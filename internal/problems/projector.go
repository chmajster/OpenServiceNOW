package problems

import (
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/events"
)

func RegisterActivityHandlers(d *events.Dispatcher, p *activity.Projector) error {
	for kind, label := range map[string]string{"problem.created": "Zarejestrowano problem", "problem.changed": "Zmieniono problem", "problem.task.changed": "Zmieniono zadanie analityczne", "problem.comment.added": "Odpowiedź publiczna", "problem.note.added": "Notatka wewnętrzna", "problem.attachment.added": "Dodano załącznik problemu", "problem.attachment.removed": "Usunięto załącznik problemu"} {
		if err := d.Register(kind, p.Handler(kind, label)); err != nil {
			return err
		}
	}
	return nil
}
