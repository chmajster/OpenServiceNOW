package changes

import (
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/events"
)

func RegisterActivityHandlers(d *events.Dispatcher, p *activity.Projector) error {
	for kind, label := range map[string]string{"change.created": "Zarejestrowano zmianę", "change.changed": "Zmieniono zmianę", "change.task.changed": "Zmieniono zadanie wykonawcze", "change.comment.added": "Odpowiedź publiczna", "change.note.added": "Notatka wewnętrzna", "change.attachment.added": "Dodano załącznik zmiany", "change.attachment.removed": "Usunięto załącznik zmiany"} {
		if err := d.Register(kind, p.Handler(kind, label)); err != nil {
			return err
		}
	}
	return nil
}
