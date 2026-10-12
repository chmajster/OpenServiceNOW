package sla

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/incidents"
)

func utc(s string) time.Time {
	t, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return t
}
func office() Calendar {
	c := Calendar{Name: "Biuro", Timezone: "Europe/Warsaw", Holidays: []string{}, Exceptions: []Exception{}}
	for _, d := range []int{1, 2, 3, 4, 5} {
		c.Week = append(c.Week, Day{d, []Window{{"09:00", "17:00"}}})
	}
	return c
}
func TestBusinessCalendar(t *testing.T) {
	c := office()
	c.Holidays = []string{"2026-10-19"}
	c.Exceptions = []Exception{{"2026-10-20", []Window{{"12:00", "14:00"}}}}
	cases := []struct {
		name, start, end string
		n                int64
	}{
		{"weekend holiday and exception", "2026-10-16T14:30:00Z", "2026-10-20T11:30:00Z", 7200},
		{"night", "2026-10-14T18:00:00Z", "2026-10-15T07:01:00Z", 60},
		{"partial seconds", "2026-10-15T06:59:55Z", "2026-10-15T07:00:11Z", 11},
	}
	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			n, e := c.Elapsed(utc(v.start), utc(v.end))
			if e != nil || n != v.n {
				t.Fatalf("elapsed %d: %v", n, e)
			}
			end, e := c.Add(utc(v.start), v.n)
			if e != nil || !end.Equal(utc(v.end)) {
				t.Fatalf("due %v: %v", end, e)
			}
		})
	}
}
func TestWarsawDaylightSaving(t *testing.T) {
	c := Calendar{Name: "Noc", Timezone: "Europe/Warsaw", Week: []Day{{0, []Window{{"01:00", "04:00"}}}}}
	for _, v := range []struct {
		start, end string
		n          int64
	}{{"2026-03-29T00:00:00Z", "2026-03-29T02:00:00Z", 7200}, {"2026-10-24T23:00:00Z", "2026-10-25T03:00:00Z", 14400}} {
		n, e := c.Elapsed(utc(v.start), utc(v.end))
		if e != nil || n != v.n {
			t.Fatalf("DST elapsed %d: %v", n, e)
		}
		due, e := c.Add(utc(v.start), v.n)
		if e != nil || !due.Equal(utc(v.end)) {
			t.Fatalf("DST due %v: %v", due, e)
		}
	}
}
func TestMidnightDaylightSaving(t *testing.T) {
	c := Calendar{Name: "Północ", Timezone: "Africa/Cairo", Week: []Day{{5, []Window{{"00:00", "04:00"}}}}}
	start := utc("2026-04-23T22:00:00Z")
	n, err := c.Elapsed(start, utc("2026-04-24T02:00:00Z"))
	if err != nil || n != 10800 {
		t.Fatal("skipped midnight shifted the end of the local window", n, err)
	}
	due, err := c.Add(start, 14400)
	if err != nil || !due.Equal(utc("2026-04-30T22:00:00Z")) {
		t.Fatal("normalized midnight shifted subsequent civil dates", due, err)
	}
	c.Week = []Day{{4, []Window{{"23:00", "24:00"}}}}
	start, end := utc("2026-10-29T20:00:00Z"), utc("2026-10-29T22:00:00Z")
	n, err = c.Elapsed(start, end)
	due, addErr := c.Add(start, 7200)
	if err != nil || addErr != nil || n != 7200 || !due.Equal(end) {
		t.Fatal("repeated final hour of civil date", n, due, err, addErr)
	}
}
func TestPauseResumeAndDelayedClock(t *testing.T) {
	c := office()
	p := Policy{ID: 1, Version: 1, Name: "Standard", CalendarID: 2, CalendarVersion: 1, ResponseSeconds: 3600, ResolutionSeconds: 7200, WarningPercent: 50, PauseReasons: []string{"Oczekiwanie na klienta"}}
	i, e := start(p, "resolution", 1, 9007199254740993, 1, utc("2026-10-15T07:00:00Z"), c)
	if e != nil {
		t.Fatal(e)
	}
	state := checkpoint{Policy: &p}
	snapshot := incidents.Snapshot{State: "ON_HOLD", HoldReason: "Dostawca", Version: 2}
	if state.shouldPause(snapshot) {
		t.Fatal("status alone paused SLA")
	}
	snapshot.HoldReason = "Oczekiwanie na klienta"
	for _, v := range []struct{ at, state, action string }{{"2026-10-15T07:30:00Z", "ON_HOLD", "paused"}, {"2026-10-15T08:00:00Z", "IN_PROGRESS", "resumed"}, {"2026-10-15T08:30:00Z", "ON_HOLD", "paused"}, {"2026-10-15T09:00:00Z", "IN_PROGRESS", "resumed"}} {
		snapshot.State = v.state
		snapshot.Version++
		action, e := apply(i, c, p, "incident.state.changed", snapshot, utc(v.at), state.shouldPause(snapshot))
		if e != nil || action != v.action {
			t.Fatalf("%s: %s %v", v.action, action, e)
		}
	}
	if i.AccruedSeconds != 3600 || !i.DueAt.Equal(utc("2026-10-15T10:00:00Z")) {
		t.Fatalf("pause accounting %+v", i)
	}
	if e = advance(i, c, 50, utc("2026-10-15T12:00:00Z")); e != nil {
		t.Fatal(e)
	}
	if i.BreachedAt == nil || !i.BreachedAt.Equal(utc("2026-10-15T10:00:00Z")) || !i.WarnedAt.Equal(utc("2026-10-15T08:30:00Z")) {
		t.Fatalf("delayed thresholds %+v", i)
	}
	raw, _ := json.Marshal(i)
	restored := Instance{}
	_ = json.Unmarshal(raw, &restored)
	if e = advance(&restored, c, 50, utc("2026-10-15T12:00:00Z")); e != nil || restored.AccruedSeconds != i.AccruedSeconds {
		t.Fatal("restart/retry duplicated time", e)
	}
	if !strings.Contains(string(raw), `"incident_id":"9007199254740993"`) {
		t.Fatal("unsafe bigint DTO")
	}
}
func TestPolicySelectionAndValidation(t *testing.T) {
	p := []Policy{{ID: 9, Enabled: true, Order: 2}, {ID: 8, Enabled: true, Order: 1}, {ID: 4, Enabled: true, Order: 1, Priority: 1}, {ID: 3, Enabled: true, Order: 1, Priority: 1}, {ID: 2, Enabled: false, Order: 0}}
	if got := choose(p, 1, "network"); got == nil || got.ID != 3 {
		t.Fatal("ambiguous selection", got)
	}
	if ValidateCalendar(office()) != nil {
		t.Fatal("valid office rejected")
	}
	for _, c := range []Calendar{{Name: "x", Timezone: "Local"}, {Name: "x", Timezone: "Europe/Warsaw", Week: []Day{{1, []Window{{"17:00", "09:00"}}}}}, {Name: "x", Timezone: "Europe/Warsaw", Week: []Day{{1, []Window{{"09:00", "13:00"}, {"12:00", "17:00"}}}}}} {
		if ValidateCalendar(c) == nil {
			t.Fatal("invalid calendar accepted")
		}
	}
}
func TestResolutionBeforeDelayedWorkerAndFirstResponse(t *testing.T) {
	c := office()
	p := Policy{ResolutionSeconds: 7200, ResponseSeconds: 3600, WarningPercent: 90}
	i, _ := start(p, "resolution", 1, 1, 1, utc("2026-10-15T07:00:00Z"), c)
	_, e := apply(i, c, p, "incident.resolved", incidents.Snapshot{State: "RESOLVED", Version: 2}, utc("2026-10-15T08:00:00Z"), false)
	if e != nil {
		t.Fatal(e)
	}
	_ = advance(i, c, 90, utc("2026-10-19T12:00:00Z"))
	if i.BreachedAt != nil || i.AccruedSeconds != 3600 || i.Status != "COMPLETED" {
		t.Fatalf("false late breach %+v", i)
	}
	response, _ := start(p, "response", 1, 1, 1, utc("2026-10-15T07:00:00Z"), c)
	_, e = apply(response, c, p, "incident.first_response", incidents.Snapshot{State: "NEW", Version: 1}, utc("2026-10-15T07:20:00Z"), false)
	if e != nil || response.Status != "COMPLETED" || response.AccruedSeconds != 1200 {
		t.Fatal(response, e)
	}
}

func TestFractionalSecondsSurviveMultipleTransitions(t *testing.T) {
	c := office()
	p := Policy{ResponseSeconds: 60, WarningPercent: 50}
	i, _ := start(p, "response", 1, 1, 1, utc("2026-10-15T07:00:00Z"), c)
	for n := 1; n <= 10; n++ {
		if err := advance(i, c, 50, i.StartedAt.Add(time.Duration(n)*150*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if i.AccruedMicros != 1500000 || i.AccruedSeconds != 1 || i.RemainingSeconds != 59 {
		t.Fatal("lost fractional elapsed time", i)
	}
	raw, _ := json.Marshal(i)
	var restored Instance
	_ = json.Unmarshal(raw, &restored)
	if err := advance(&restored, c, 50, i.StartedAt.Add(2*time.Second)); err != nil || restored.AccruedMicros != 2000000 {
		t.Fatal("fractional restart", restored, err)
	}
}
