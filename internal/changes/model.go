package changes

import (
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
	"time"
	_ "time/tzdata"
)

type Actor = workrecords.Principal

var (
	ErrInvalid    = errors.New("nieprawidłowe dane zmiany")
	ErrNotFound   = errors.New("nie znaleziono dostępnej zmiany")
	ErrForbidden  = workrecords.ErrForbidden
	ErrConflict   = errors.New("rekord zmienił się; porównaj aktualną wersję ze swoją propozycją")
	ErrTransition = errors.New("niedozwolone przejście procesu Change")
	ErrCollision  = errors.New("termin koliduje ze zmianą, okresem zakazu lub oknem serwisowym")
)

type Fields struct {
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	Type               string     `json:"change_type"`
	Impact             string     `json:"impact"`
	Risk               string     `json:"risk"`
	Justification      string     `json:"justification"`
	Scope              string     `json:"scope"`
	ImplementationPlan string     `json:"implementation_plan"`
	TestPlan           string     `json:"test_plan"`
	RollbackPlan       string     `json:"rollback_plan"`
	EmergencyReason    string     `json:"emergency_reason"`
	ReviewResult       string     `json:"review_result"`
	ReviewOutcome      string     `json:"review_outcome"`
	PlannedStart       *time.Time `json:"planned_start"`
	PlannedEnd         *time.Time `json:"planned_end"`
	TimeZone           string     `json:"time_zone"`
	WorkflowID         *uint64    `json:"workflow_id,string"`
	WorkflowVersion    *uint64    `json:"workflow_version,string"`
	TemplateID         *uint64    `json:"template_id,string"`
	TemplateVersion    *uint64    `json:"template_version,string"`
	AssignedTo         *uint64    `json:"assigned_to,string"`
	GroupID            *uint64    `json:"assignment_group_id,string"`
}
type Record struct {
	Fields
	ID                 uint64     `json:"id,string"`
	Number             string     `json:"number"`
	ReporterID         uint64     `json:"reporter_id,string"`
	ReporterName       string     `json:"reporter_name"`
	AssignedName       string     `json:"assigned_name"`
	GroupName          string     `json:"group_name"`
	State              string     `json:"state"`
	Reason             string     `json:"reason"`
	Version            uint64     `json:"version,string"`
	AssessmentRevision uint64     `json:"assessment_revision,string"`
	ApprovedRevision   *uint64    `json:"approved_revision,string"`
	EvaluatedVersion   *uint64    `json:"evaluated_version,string"`
	ExecutionID        *uint64    `json:"execution_id,string"`
	ApprovalState      string     `json:"approval_state"`
	ActualStart        *time.Time `json:"actual_start"`
	ActualEnd          *time.Time `json:"actual_end"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	CanEdit            bool       `json:"can_edit"`
	CanAssign          bool       `json:"can_assign"`
	CanTasks           bool       `json:"can_tasks"`
	Actions            []string   `json:"actions"`
}
type Input struct {
	Fields
	Version         uint64 `json:"version,string"`
	SourceProblemID uint64 `json:"source_problem_id,string,omitempty"`
	SourceVersion   uint64 `json:"source_version,string,omitempty"`
}
type TransitionInput struct {
	Version  uint64 `json:"version,string"`
	State    string `json:"state"`
	Reason   string `json:"reason"`
	Override bool   `json:"override"`
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func active(state string) bool   { return state != "CLOSED" && state != "CANCELLED" }
func editable(state string) bool { return active(state) }
func Normalize(f Fields) (Fields, error) {
	for _, p := range []*string{&f.Title, &f.Description, &f.Justification, &f.Scope, &f.ImplementationPlan, &f.TestPlan, &f.RollbackPlan, &f.EmergencyReason, &f.ReviewResult} {
		*p = strings.TrimSpace(*p)
		if !catalog.Text(*p, 0, 20000) {
			return f, ErrInvalid
		}
	}
	if !catalog.Text(f.Title, 1, 160) || !catalog.Text(f.Description, 1, 20000) || !contains([]string{"NORMAL", "STANDARD", "EMERGENCY"}, f.Type) || !contains([]string{"HIGH", "MEDIUM", "LOW"}, f.Impact) || !contains([]string{"HIGH", "MEDIUM", "LOW"}, f.Risk) || !contains([]string{"", "SUCCESS", "PARTIAL", "FAILED"}, f.ReviewOutcome) {
		return f, ErrInvalid
	}
	if _, err := time.LoadLocation(f.TimeZone); err != nil || f.TimeZone == "Local" || len(f.TimeZone) > 64 {
		return f, errors.Join(ErrInvalid, errors.New("wybierz jednoznaczną strefę IANA"))
	}
	for _, id := range []*uint64{f.WorkflowID, f.WorkflowVersion, f.TemplateID, f.TemplateVersion, f.AssignedTo, f.GroupID} {
		if id != nil && *id == 0 {
			return f, ErrInvalid
		}
	}
	if (f.WorkflowID == nil) != (f.WorkflowVersion == nil) || (f.TemplateID == nil) != (f.TemplateVersion == nil) {
		return f, ErrInvalid
	}
	if (f.PlannedStart == nil) != (f.PlannedEnd == nil) {
		return f, ErrInvalid
	}
	if f.PlannedStart != nil {
		start, end := f.PlannedStart.UTC(), f.PlannedEnd.UTC()
		f.PlannedStart = &start
		f.PlannedEnd = &end
		if !end.After(start) || end.Sub(start) > 30*24*time.Hour || start.Year() < 2000 || end.Year() > 2100 {
			return f, errors.Join(ErrInvalid, errors.New("przedział musi trwać od 1 mikrosekundy do 30 dni w latach 2000–2100"))
		}
	}
	if f.Type == "STANDARD" && f.TemplateID == nil || f.Type != "STANDARD" && f.TemplateID != nil {
		return f, errors.Join(ErrInvalid, errors.New("Standard wymaga wersji zatwierdzonego szablonu"))
	}
	return f, nil
}
func actions(a Actor, r Record) []string {
	out := []string{}
	if !a.Has("change.transition") {
		return out
	}
	switch r.State {
	case "NEW":
		if r.Type == "NORMAL" {
			out = append(out, "ASSESS")
		} else {
			out = append(out, "AUTHORIZE")
		}
	case "ASSESS":
		out = append(out, "AUTHORIZE")
	case "AUTHORIZE":
		if r.ApprovalState == "APPROVED" {
			out = append(out, "SCHEDULED")
		}
		if r.Type == "NORMAL" {
			out = append(out, "ASSESS")
		} else {
			out = append(out, "NEW")
		}
	case "SCHEDULED":
		out = append(out, "IMPLEMENT")
		if r.Type == "NORMAL" {
			out = append(out, "ASSESS")
		} else {
			out = append(out, "NEW")
		}
	case "IMPLEMENT":
		out = append(out, "REVIEW")
	case "REVIEW":
		out = append(out, "CLOSED")
	}
	if active(r.State) {
		out = append(out, "CANCELLED")
	}
	return out
}
