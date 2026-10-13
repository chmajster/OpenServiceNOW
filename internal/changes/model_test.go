package changes

import (
	"testing"
	"time"
)

func TestChangeInstantsAndMaterialAssessment(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2027-10-31T02:15:00+02:00")
	end, _ := time.Parse(time.RFC3339, "2027-10-31T02:15:00+01:00")
	f := Fields{Title: "Zmiana", Description: "Plan", Type: "NORMAL", Risk: "LOW", Impact: "LOW", TimeZone: "Europe/Warsaw", PlannedStart: &start, PlannedEnd: &end}
	valid, err := Normalize(f)
	if err != nil {
		t.Fatal(err)
	}
	if valid.PlannedEnd.Sub(*valid.PlannedStart) != time.Hour || valid.PlannedStart.Location() != time.UTC {
		t.Fatal("DST instants lost their offsets")
	}
	f.PlannedEnd = &start
	if _, err = Normalize(f); err == nil {
		t.Fatal("empty interval accepted")
	}
	f.PlannedEnd = &end
	f.TimeZone = "Local"
	if _, err = Normalize(f); err == nil {
		t.Fatal("host-local timezone accepted")
	}
	f.TimeZone = "Europe/Warsaw"
	f.Type = "STANDARD"
	if _, err = Normalize(f); err == nil {
		t.Fatal("Standard without approved template accepted")
	}
	f = valid
	g := f
	g.ReviewResult = "Zweryfikowano"
	g.ReviewOutcome = "SUCCESS"
	if material(f).ReviewResult != material(g).ReviewResult {
		t.Fatal("review invalidates evaluated implementation plan")
	}
}
