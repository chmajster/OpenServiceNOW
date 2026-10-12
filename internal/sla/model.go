package sla

import (
	"errors"
	"time"
)

var (
	ErrInvalid  = errors.New("Nieprawidłowa konfiguracja SLA")
	ErrConflict = errors.New("Konfiguracja SLA została zmieniona; porównaj bieżącą wersję ze swoją propozycją")
	ErrNotFound = errors.New("Nie znaleziono SLA")
)

const ConsumerID = "sla.v1"
const JobType = "sla.reconcile"
const Rules = "Start przy utworzeniu; pierwsza odpowiedź to publiczna wiadomość operatora innego niż zgłaszający. Priorytet i kategoria są zamrożone przy wyborze. Ponowne otwarcie rozpoczyna nowy czas rozwiązania z tą samą wersją polityki; odpowiedź nie jest resetowana. Zmiany kalendarza dotyczą wyłącznie nowych wersji polityk. Pauzują tylko dokładnie wskazane powody wstrzymania. Rozwiązanie kończy oba mierniki, anulowanie je anuluje."

type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
type Day struct {
	Weekday int      `json:"weekday"`
	Windows []Window `json:"windows"`
}
type Exception struct {
	Date    string   `json:"date"`
	Windows []Window `json:"windows"`
}
type Calendar struct {
	ID         uint64      `json:"id,string"`
	Version    uint64      `json:"version,string"`
	Name       string      `json:"name"`
	Timezone   string      `json:"timezone"`
	Week       []Day       `json:"week"`
	Holidays   []string    `json:"holidays"`
	Exceptions []Exception `json:"exceptions"`
}
type Policy struct {
	ID                uint64    `json:"id,string"`
	Version           uint64    `json:"version,string"`
	Name              string    `json:"name"`
	Enabled           bool      `json:"enabled"`
	Order             int       `json:"order"`
	Priority          int       `json:"priority"`
	Category          string    `json:"category"`
	CalendarID        uint64    `json:"calendar_id,string"`
	CalendarVersion   uint64    `json:"calendar_version,string"`
	ResponseSeconds   int64     `json:"response_seconds"`
	ResolutionSeconds int64     `json:"resolution_seconds"`
	WarningPercent    int       `json:"warning_percent"`
	PauseReasons      []string  `json:"pause_reasons"`
	EffectiveAt       time.Time `json:"effective_at"`
}
type Instance struct {
	ID               uint64     `json:"id,string"`
	IncidentID       uint64     `json:"incident_id,string"`
	PolicyID         uint64     `json:"policy_id,string"`
	PolicyVersion    uint64     `json:"policy_version,string"`
	CalendarID       uint64     `json:"calendar_id,string"`
	CalendarVersion  uint64     `json:"calendar_version,string"`
	PolicyName       string     `json:"policy_name"`
	Kind             string     `json:"kind"`
	Cycle            uint64     `json:"cycle,string"`
	Status           string     `json:"status"`
	StartedAt        time.Time  `json:"started_at"`
	AccruedSeconds   int64      `json:"accrued_seconds"`
	AccruedMicros    int64      `json:"accrued_microseconds,string"`
	TargetSeconds    int64      `json:"target_seconds"`
	RemainingSeconds int64      `json:"remaining_seconds"`
	DueAt            *time.Time `json:"due_at"`
	FinishedAt       *time.Time `json:"finished_at"`
	PausedAt         *time.Time `json:"paused_at"`
	PauseReason      string     `json:"pause_reason"`
	WarnedAt         *time.Time `json:"warned_at"`
	BreachedAt       *time.Time `json:"breached_at"`
	CalculatedAt     time.Time  `json:"calculated_at"`
	IncidentVersion  uint64     `json:"incident_version,string"`
	Version          uint64     `json:"version,string"`
}
type History struct {
	ID              uint64     `json:"id,string"`
	InstanceID      uint64     `json:"instance_id,string"`
	IncidentVersion uint64     `json:"incident_version,string"`
	SourceEventID   uint64     `json:"source_event_id,string"`
	Type            string     `json:"type"`
	OccurredAt      time.Time  `json:"occurred_at"`
	AccruedSeconds  int64      `json:"accrued_seconds"`
	Reason          string     `json:"reason"`
	DueAt           *time.Time `json:"due_at"`
}
