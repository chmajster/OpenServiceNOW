package assets

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalid   = errors.New("nieprawidłowe dane ewidencji")
	ErrNotFound  = errors.New("nie znaleziono dostępnego rekordu ewidencji")
	ErrConflict  = errors.New("wersja rekordu zmieniła się; porównaj aktualny stan z propozycją")
	ErrDuplicate = errors.New("identyfikator lub numer seryjny jest już używany w organizacji")
)

type Fields struct {
	AssetTag      string `json:"asset_tag"`
	Name          string `json:"name"`
	OwnerID       uint64 `json:"owner_id,string"`
	SerialNumber  string `json:"serial_number"`
	Vendor        string `json:"vendor"`
	Model         string `json:"model"`
	Location      string `json:"location"`
	State         string `json:"state"`
	PurchaseDate  string `json:"purchase_date"`
	PurchaseCost  string `json:"purchase_cost"`
	Currency      string `json:"currency"`
	WarrantyUntil string `json:"warranty_until"`
}
type Record struct {
	Fields
	ID        uint64    `json:"id,string"`
	Version   uint64    `json:"version,string"`
	OwnerName string    `json:"owner_name"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Page struct {
	Items []Record `json:"items"`
	Next  uint64   `json:"next_before_id,string,omitempty"`
}
type Filter struct {
	Query, State, Location string
	OwnerID, Before        uint64
	Archived               string
	Limit                  int
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,99}$`)
var serial = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._:/-]{0,127}$`)
var money = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$`)

func Identifier(s string) bool { return identifier.MatchString(s) }
func Text(s string, min, max int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) < min || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func Date(s string) bool {
	if s == "" {
		return true
	}
	t, e := time.Parse("2006-01-02", s)
	return e == nil && t.Format("2006-01-02") == s
}
func Normalize(f Fields) (Fields, error) {
	f.AssetTag = strings.ToLower(strings.TrimSpace(f.AssetTag))
	f.Name = strings.TrimSpace(f.Name)
	f.SerialNumber = strings.ToUpper(strings.TrimSpace(f.SerialNumber))
	f.Vendor = strings.TrimSpace(f.Vendor)
	f.Model = strings.TrimSpace(f.Model)
	f.Location = strings.TrimSpace(f.Location)
	if !Identifier(f.AssetTag) || !Text(f.Name, 1, 160) || f.OwnerID == 0 || !Text(f.Vendor, 0, 160) || !Text(f.Model, 0, 160) || !Text(f.Location, 0, 160) || f.SerialNumber != "" && !serial.MatchString(f.SerialNumber) {
		return f, ErrInvalid
	}
	if !State(f.State) || !Date(f.PurchaseDate) || !Date(f.WarrantyUntil) || f.PurchaseDate != "" && f.WarrantyUntil != "" && f.WarrantyUntil < f.PurchaseDate {
		return f, ErrInvalid
	}
	if f.Currency != "PLN" && f.Currency != "EUR" && f.Currency != "USD" && f.Currency != "GBP" && f.Currency != "CZK" {
		return f, ErrInvalid
	}
	if f.PurchaseCost != "" {
		if !money.MatchString(f.PurchaseCost) {
			return f, ErrInvalid
		}
		p := strings.Split(f.PurchaseCost, ".")
		if len(p) == 1 {
			f.PurchaseCost += ".00"
		} else if len(p[1]) == 1 {
			f.PurchaseCost += "0"
		}
	}
	return f, nil
}
func State(s string) bool {
	switch s {
	case "ORDERED", "IN_STOCK", "IN_USE", "IN_REPAIR", "RETIRED", "DISPOSED":
		return true
	}
	return false
}
func Transition(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string][]string{"ORDERED": {"IN_STOCK", "RETIRED"}, "IN_STOCK": {"IN_USE", "IN_REPAIR", "RETIRED"}, "IN_USE": {"IN_STOCK", "IN_REPAIR", "RETIRED"}, "IN_REPAIR": {"IN_STOCK", "IN_USE", "RETIRED"}, "RETIRED": {"DISPOSED"}}
	for _, s := range allowed[from] {
		if s == to {
			return true
		}
	}
	return false
}
