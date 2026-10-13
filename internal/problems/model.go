package problems

import (
	"context"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
	"time"
)

type Actor = workrecords.Principal

var (
	ErrInvalid    = errors.New("nieprawidłowe dane problemu")
	ErrNotFound   = errors.New("nie znaleziono dostępnego problemu")
	ErrForbidden  = workrecords.ErrForbidden
	ErrConflict   = errors.New("rekord zmienił się; zachowaj propozycję i odczytaj aktualną wersję")
	ErrTransition = errors.New("niedozwolone przejście procesu Problem")
)

type Fields struct {
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Impact       string  `json:"impact"`
	Symptoms     string  `json:"symptoms"`
	Analysis     string  `json:"analysis"`
	RootCause    string  `json:"root_cause"`
	Workaround   string  `json:"workaround"`
	SolutionPlan string  `json:"solution_plan"`
	Verification string  `json:"verification"`
	KnownError   bool    `json:"known_error"`
	AssignedTo   *uint64 `json:"assigned_to,string"`
	GroupID      *uint64 `json:"assignment_group_id,string"`
}
type Record struct {
	Fields
	ID            uint64    `json:"id,string"`
	Number        string    `json:"number"`
	ReporterID    uint64    `json:"reporter_id,string"`
	ReporterName  string    `json:"reporter_name"`
	AssignedName  string    `json:"assigned_name"`
	GroupName     string    `json:"group_name"`
	State         string    `json:"state"`
	Reason        string    `json:"reason"`
	Version       uint64    `json:"version,string"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	CanEdit       bool      `json:"can_edit"`
	CanAssign     bool      `json:"can_assign"`
	CanKnownError bool      `json:"can_known_error"`
	CanTasks      bool      `json:"can_tasks"`
	Actions       []string  `json:"actions"`
}
type Input struct {
	Fields
	Version          uint64 `json:"version,string"`
	SourceIncidentID uint64 `json:"source_incident_id,string,omitempty"`
	SourceVersion    uint64 `json:"source_version,string,omitempty"`
}
type Change struct {
	Version uint64 `json:"version,string"`
	State   string `json:"state"`
	Reason  string `json:"reason"`
}
type IncidentSnapshot struct {
	ID          uint64 `json:"id,string"`
	Number      string `json:"number"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Impact      string `json:"impact"`
	State       string `json:"state"`
	Version     uint64 `json:"version,string"`
	CanLink     bool   `json:"can_link"`
}

// The incident domain authorizes both its preview and the versioned link change.
// Only explicit public form fields are part of the transfer contract.
type IncidentDomain interface {
	ProblemReference(context.Context, database.Executor, Actor, uint64, bool, uint64) (IncidentSnapshot, error)
	ProblemReferences(context.Context, Actor, string, uint64, uint64, int) ([]IncidentSnapshot, uint64, error)
}

func Normalize(f Fields) (Fields, error) {
	fields := []*string{&f.Title, &f.Description, &f.Symptoms, &f.Analysis, &f.RootCause, &f.Workaround, &f.SolutionPlan, &f.Verification}
	for _, v := range fields {
		*v = strings.TrimSpace(*v)
		if !catalog.Text(*v, 0, 20000) {
			return f, ErrInvalid
		}
	}
	if !catalog.Text(f.Title, 1, 160) || !catalog.Text(f.Description, 1, 20000) || !contains([]string{"HIGH", "MEDIUM", "LOW"}, f.Impact) {
		return f, ErrInvalid
	}
	if f.AssignedTo != nil && *f.AssignedTo == 0 || f.GroupID != nil && *f.GroupID == 0 {
		return f, ErrInvalid
	}
	if f.KnownError && (f.RootCause == "" || f.Workaround == "") {
		return f, errors.Join(ErrInvalid, errors.New("Known Error wymaga przyczyny źródłowej i obejścia"))
	}
	return f, nil
}
func contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
func active(state string) bool   { return !contains([]string{"RESOLVED", "CLOSED", "CANCELLED"}, state) }
func editable(state string) bool { return !contains([]string{"CLOSED", "CANCELLED"}, state) }
func actions(a Actor, r Record) []string {
	out := []string{}
	if !a.Has("problem.transition") {
		return out
	}
	switch r.State {
	case "NEW":
		out = append(out, "ASSESS")
	case "ASSESS":
		out = append(out, "ROOT_CAUSE")
	case "ROOT_CAUSE":
		out = append(out, "ASSESS", "FIX_IN_PROGRESS")
	case "FIX_IN_PROGRESS":
		out = append(out, "ROOT_CAUSE", "RESOLVED")
	case "RESOLVED":
		out = append(out, "ROOT_CAUSE", "CLOSED")
	case "CLOSED", "CANCELLED":
		out = append(out, "ASSESS")
	}
	if active(r.State) || r.State == "RESOLVED" {
		out = append(out, "CANCELLED")
	}
	return out
}
func validateTransition(r Record, in Change) error {
	if !catalog.Text(strings.TrimSpace(in.Reason), 0, 10000) {
		return ErrInvalid
	}
	required := []string{}
	switch in.State {
	case "ASSESS":
		if r.State == "CLOSED" || r.State == "CANCELLED" || r.State == "ROOT_CAUSE" {
			required = append(required, in.Reason)
		}
	case "ROOT_CAUSE":
		required = append(required, r.Symptoms)
		if r.State == "RESOLVED" || r.State == "FIX_IN_PROGRESS" {
			required = append(required, in.Reason)
		}
	case "FIX_IN_PROGRESS":
		required = append(required, r.Analysis, r.RootCause, r.SolutionPlan)
	case "RESOLVED", "CLOSED":
		required = append(required, r.Analysis, r.RootCause, r.SolutionPlan, r.Verification)
	case "CANCELLED":
		required = append(required, in.Reason)
	default:
		return ErrTransition
	}
	if in.State == "ASSESS" || in.State == "ROOT_CAUSE" || in.State == "FIX_IN_PROGRESS" {
		if r.GroupID == nil {
			return errors.Join(ErrTransition, errors.New("wskaż grupę odpowiedzialną za analizę"))
		}
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return errors.Join(ErrTransition, errors.New("uzupełnij wymagane pola analizy, rozwiązania, weryfikacji lub uzasadnienia"))
		}
	}
	return nil
}
