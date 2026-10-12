package requests

import (
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"net/http"
)

func (h *Handler) Templates(w http.ResponseWriter, r *http.Request) {
	templates := []catalog.Definition{
		{Title: "Dostęp do systemu", Description: "Nadanie uprawnień do wybranego systemu.", Method: "MANUAL_TASKS", Fields: []catalog.Field{{Key: "system", Label: "System", Type: "text", Required: true}, {Key: "justification", Label: "Uzasadnienie", Type: "longtext", Required: true}}, Steps: []catalog.Step{{Title: "Nadaj dostęp do systemu", Instructions: "Zweryfikuj zakres dostępu i zrealizuj nadanie uprawnień. Zapisz wynik realizacji.", Required: true}}},
		{Title: "Zamówienie sprzętu", Description: "Przygotowanie i przekazanie sprzętu.", Method: "MANUAL_TASKS", Fields: []catalog.Field{{Key: "equipment", Label: "Sprzęt", Type: "select", Options: []string{"Laptop", "Monitor", "Klawiatura"}, Required: true}, {Key: "quantity", Label: "Liczba sztuk", Type: "number", Required: true}, {Key: "needed_on", Label: "Oczekiwana data", Type: "date"}}, Steps: []catalog.Step{{Title: "Przygotuj sprzęt", Required: true}, {Title: "Przekaż sprzęt użytkownikowi", Required: true}}},
		{Title: "Instalacja oprogramowania", Description: "Instalacja wskazanego oprogramowania.", Method: "MANUAL_TASKS", Fields: []catalog.Field{{Key: "software", Label: "Oprogramowanie", Type: "text", Required: true}, {Key: "device", Label: "Urządzenie", Type: "text", Required: true}, {Key: "license", Label: "Potwierdzam posiadanie licencji", Type: "checkbox", Required: true}}, Steps: []catalog.Step{{Title: "Zweryfikuj licencję i zainstaluj oprogramowanie", Required: true}}},
	}
	respond(w, r, 200, map[string]any{"templates": templates}, nil)
}
