package sla

import (
	"fmt"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/incidents"
)

type checkpoint struct {
	Policy          *Policy   `json:"policy"`
	Calendar        Calendar  `json:"calendar"`
	Response        *Instance `json:"response"`
	Resolution      *Instance `json:"resolution"`
	CursorVersion   uint64    `json:"cursor_version"`
	CursorTime      time.Time `json:"cursor_time"`
	CursorID        uint64    `json:"cursor_id"`
	IncidentVersion uint64    `json:"incident_version"`
	LastState       string    `json:"last_state"`
}

func start(p Policy, kind string, cycle, id, version uint64, at time.Time, c Calendar) (*Instance, error) {
	target := p.ResponseSeconds
	if kind == "resolution" {
		target = p.ResolutionSeconds
	}
	due, err := c.Add(at, target)
	if err != nil {
		return nil, err
	}
	return &Instance{IncidentID: id, PolicyID: p.ID, PolicyVersion: p.Version, CalendarID: p.CalendarID, CalendarVersion: p.CalendarVersion, PolicyName: p.Name, Kind: kind, Cycle: cycle, Status: "ACTIVE", StartedAt: at.UTC(), TargetSeconds: target, RemainingSeconds: target, DueAt: &due, CalculatedAt: at.UTC(), IncidentVersion: version}, nil
}
func advance(i *Instance, c Calendar, percent int, at time.Time) error {
	if at.Before(i.CalculatedAt) {
		return fmt.Errorf("%w: niezgodna kolejność czasu biznesowego", ErrInvalid)
	}
	if i.Status != "ACTIVE" {
		i.CalculatedAt = at.UTC()
		return nil
	}
	n, err := c.elapsedMicros(i.CalculatedAt, at)
	if err != nil {
		return err
	}
	warning := i.TargetSeconds * 1000000 * int64(percent) / 100
	if i.WarnedAt == nil && i.AccruedMicros+n >= warning {
		t, e := c.addMicros(i.CalculatedAt, max(int64(0), warning-i.AccruedMicros))
		if e != nil {
			return e
		}
		i.WarnedAt = &t
	}
	if i.BreachedAt == nil && i.AccruedMicros+n >= i.TargetSeconds*1000000 {
		t, e := c.addMicros(i.CalculatedAt, max(int64(0), i.TargetSeconds*1000000-i.AccruedMicros))
		if e != nil {
			return e
		}
		i.BreachedAt = &t
	}
	i.AccruedMicros += n
	i.AccruedSeconds = i.AccruedMicros / 1000000
	i.RemainingSeconds = max(int64(0), (i.TargetSeconds*1000000-i.AccruedMicros+999999)/1000000)
	i.CalculatedAt = at.UTC()
	// Preserve the original deadline once breached; further computation must not
	// move it forward to the time when a delayed worker happened to run.
	if i.BreachedAt != nil {
		i.DueAt = i.BreachedAt
	}
	return nil
}
func (s checkpoint) shouldPause(r incidents.Snapshot) bool {
	if r.State != "ON_HOLD" || s.Policy == nil {
		return false
	}
	for _, reason := range s.Policy.PauseReasons {
		if reason == r.HoldReason {
			return true
		}
	}
	return false
}
func apply(i *Instance, c Calendar, p Policy, kind string, r incidents.Snapshot, at time.Time, pause bool) (string, error) {
	if err := advance(i, c, p.WarningPercent, at); err != nil {
		return "", err
	}
	i.IncidentVersion = r.Version
	if i.Status != "ACTIVE" && i.Status != "PAUSED" {
		return "", nil
	}
	if kind == "incident.first_response" && i.Kind == "response" || r.State == "RESOLVED" || r.State == "CLOSED" || r.State == "CANCELLED" {
		i.Status = "COMPLETED"
		if r.State == "CANCELLED" {
			i.Status = "CANCELLED"
		} else if i.Kind == "response" && kind != "incident.first_response" {
			i.Status = "UNANSWERED"
		}
		t := at.UTC()
		i.FinishedAt = &t
		i.PausedAt = nil
		i.PauseReason = ""
		return "finished", nil
	}
	if pause && i.Status == "ACTIVE" {
		i.Status = "PAUSED"
		t := at.UTC()
		i.PausedAt = &t
		i.PauseReason = r.HoldReason
		i.DueAt = nil
		return "paused", nil
	}
	if !pause && i.Status == "PAUSED" {
		i.Status = "ACTIVE"
		i.PausedAt = nil
		i.PauseReason = ""
		due, err := c.addMicros(at, max(int64(0), i.TargetSeconds*1000000-i.AccruedMicros))
		if err != nil {
			return "", err
		}
		if i.BreachedAt != nil {
			i.DueAt = i.BreachedAt
		} else {
			i.DueAt = &due
		}
		return "resumed", nil
	}
	if pause && i.PauseReason != r.HoldReason {
		i.PauseReason = r.HoldReason
		return "pause_reason_changed", nil
	}
	return "", nil
}
