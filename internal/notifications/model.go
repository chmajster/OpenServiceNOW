package notifications

import (
	"errors"
	"time"
)

var (
	ErrInvalid    = errors.New("Nieprawidłowe dane powiadomienia")
	ErrConflict   = errors.New("Dane zmieniły się; odśwież aktualny stan przed ponowieniem")
	ErrNotFound   = errors.New("Powiadomienie niedostępne")
	ErrEncryption = errors.New("Klucz szyfrowania SMTP jest niedostępny lub niezgodny")
)

const ConsumerID = "notifications.v1"
const SMTPJobType = "notification.smtp"

type Preference struct {
	InApp        bool   `json:"in_app_enabled"`
	SMTP         bool   `json:"smtp_enabled"`
	Assignments  bool   `json:"assignments"`
	Replies      bool   `json:"replies"`
	StateChanges bool   `json:"state_changes"`
	Resolutions  bool   `json:"resolutions"`
	Version      uint64 `json:"version,string"`
}

func defaultPreference() Preference { return Preference{true, false, true, true, true, true, 0} }

type Notification struct {
	ID            uint64     `json:"id,string"`
	SourceEventID uint64     `json:"source_event_id,string"`
	IncidentID    uint64     `json:"incident_id,string"`
	ExecutionID   uint64     `json:"execution_id,string,omitempty"`
	Kind          string     `json:"kind"`
	Summary       string     `json:"summary"`
	OccurredAt    time.Time  `json:"occurred_at"`
	ReadAt        *time.Time `json:"read_at,omitempty"`
}
type Page struct {
	Notifications []Notification `json:"notifications"`
	NextBeforeID  uint64         `json:"next_before_id,string,omitempty"`
	Unread        uint64         `json:"unread,string"`
}
type SMTPConfig struct {
	ProviderID      string `json:"provider_id"`
	Enabled         bool   `json:"enabled"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	TLSMode         string `json:"tls_mode"`
	Username        string `json:"username"`
	From            string `json:"from_address"`
	HasPassword     bool   `json:"has_password"`
	EncryptionReady bool   `json:"encryption_ready"`
	InsecureAllowed bool   `json:"insecure_allowed"`
	Version         uint64 `json:"version,string"`
	cipher          []byte
}
type ConfigInput struct {
	ProviderID string  `json:"provider_id"`
	Enabled    bool    `json:"enabled"`
	Host       string  `json:"host"`
	Port       int     `json:"port"`
	TLSMode    string  `json:"tls_mode"`
	Username   string  `json:"username"`
	From       string  `json:"from_address"`
	Password   *string `json:"password,omitempty"`
	Version    uint64  `json:"version,string"`
}
type MailDelivery struct {
	ID             uint64     `json:"id,string"`
	UserID         uint64     `json:"user_id,string"`
	NotificationID *uint64    `json:"notification_id,string,omitempty"`
	ProviderID     string     `json:"provider_id"`
	Status         string     `json:"status"`
	Attempts       uint32     `json:"attempt_count"`
	Version        uint64     `json:"version,string"`
	ErrorCode      string     `json:"error_code"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}
type MailPage struct {
	Deliveries   []MailDelivery `json:"deliveries"`
	NextBeforeID uint64         `json:"next_before_id,string,omitempty"`
}
type Attempt struct {
	Number     uint32     `json:"attempt_number"`
	Status     string     `json:"status"`
	ErrorCode  string     `json:"error_code"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}
