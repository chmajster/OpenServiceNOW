package knowledge

import (
	"errors"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

var (
	ErrInvalid   = errors.New("nieprawidłowe dane dokumentacji")
	ErrNotFound  = errors.New("nie znaleziono dokumentacji")
	ErrForbidden = errors.New("brak uprawnień do dokumentacji")
	ErrConflict  = errors.New("dokumentacja zmieniła się; odczytaj aktualną wersję")
	ErrTooLarge  = errors.New("archiwum dokumentacji przekracza dozwolony rozmiar")
)

const (
	PermissionRead   = "knowledge.read"
	PermissionWrite  = "knowledge.write"
	PermissionManage = "knowledge.manage"

	SpaceActive   = "ACTIVE"
	SpaceArchived = "ARCHIVED"
	PageDraft     = "DRAFT"
	PagePublished = "PUBLISHED"

	MaxImportBytes   int64 = 8 << 20
	MaxArchiveBytes        = 20 << 20
	MaxImportPages         = 500
	MaxPageBodyBytes       = 256 << 10
)

type Actor = workrecords.Principal

type Space struct {
	ID                 uint64    `json:"id,string"`
	Key                string    `json:"key"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	Status             string    `json:"status"`
	ReviewIntervalDays uint16    `json:"review_interval_days"`
	Version            uint64    `json:"version,string"`
	PageCount          uint64    `json:"page_count,string"`
	Watched            bool      `json:"watched"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type SpaceInput struct {
	Key                string `json:"key"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Status             string `json:"status"`
	ReviewIntervalDays uint16 `json:"review_interval_days"`
	Version            uint64 `json:"version,string,omitempty"`
}

type SpacePage struct {
	Spaces      []Space `json:"spaces"`
	NextAfterID uint64  `json:"next_after_id,string,omitempty"`
}

type Page struct {
	ID            uint64     `json:"id,string"`
	SpaceID       uint64     `json:"space_id,string"`
	ParentID      *uint64    `json:"parent_id,string,omitempty"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	Status        string     `json:"status"`
	ReviewDueAt   *time.Time `json:"review_due_at,omitempty"`
	Version       uint64     `json:"version,string"`
	CreatedBy     uint64     `json:"created_by,string"`
	UpdatedBy     uint64     `json:"updated_by,string"`
	UpdatedByName string     `json:"updated_by_name"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Attention     string     `json:"attention_reason,omitempty"`
}

type PageInput struct {
	ParentID    *uint64    `json:"parent_id,string,omitempty"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Status      string     `json:"status"`
	ReviewDueAt *time.Time `json:"review_due_at,omitempty"`
	Version     uint64     `json:"version,string,omitempty"`
}

type PageSummary struct {
	ID            uint64     `json:"id,string"`
	SpaceID       uint64     `json:"space_id,string"`
	ParentID      *uint64    `json:"parent_id,string,omitempty"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	ReviewDueAt   *time.Time `json:"review_due_at,omitempty"`
	Version       uint64     `json:"version,string"`
	UpdatedBy     uint64     `json:"updated_by,string"`
	UpdatedByName string     `json:"updated_by_name"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Excerpt       string     `json:"excerpt,omitempty"`
	Attention     string     `json:"attention_reason,omitempty"`
}

type PagePage struct {
	Pages       []PageSummary `json:"pages"`
	NextAfterID uint64        `json:"next_after_id,string,omitempty"`
}

type PageVersion struct {
	PageID        uint64     `json:"page_id,string"`
	Version       uint64     `json:"version,string"`
	ParentID      *uint64    `json:"parent_id,string,omitempty"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	ReviewDueAt   *time.Time `json:"review_due_at,omitempty"`
	ChangedBy     uint64     `json:"changed_by,string"`
	ChangedByName string     `json:"changed_by_name"`
	ChangedAt     time.Time  `json:"changed_at"`
}

type VersionPage struct {
	Versions          []PageVersion `json:"versions"`
	NextBeforeVersion uint64        `json:"next_before_version,string,omitempty"`
}

type ImportResult struct {
	SpaceID  uint64 `json:"space_id,string"`
	Created  int    `json:"created"`
	Replayed bool   `json:"replayed"`
}
