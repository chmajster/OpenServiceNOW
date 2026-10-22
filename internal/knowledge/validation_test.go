package knowledge

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeSpace(t *testing.T) {
	input, err := normalizeSpace(SpaceInput{Key: " ops ", Name: " Operacje ", ReviewIntervalDays: 90}, true)
	if err != nil {
		t.Fatal(err)
	}
	if input.Key != "OPS" || input.Name != "Operacje" || input.Status != SpaceActive {
		t.Fatalf("unexpected normalized space: %#v", input)
	}
	for _, invalid := range []SpaceInput{
		{Key: "A", Name: "x"},
		{Key: "../OPS", Name: "x"},
		{Key: "OPS", Name: "", Status: SpaceActive},
		{Key: "OPS", Name: "x", Status: "DELETED"},
		{Key: "OPS", Name: "x", Status: SpaceActive, ReviewIntervalDays: 3651},
	} {
		if _, err := normalizeSpace(invalid, true); err == nil {
			t.Fatalf("accepted invalid space: %#v", invalid)
		}
	}
}

func TestNormalizePageAndReviewDefaults(t *testing.T) {
	input, err := normalizePage(PageInput{Title: "  Instrukcja  ", Body: "# Treść", Status: PagePublished}, true)
	if err != nil || input.Title != "Instrukcja" {
		t.Fatalf("normalization failed: %#v, %v", input, err)
	}
	applyReviewDefault(&input, 30)
	if input.ReviewDueAt == nil || input.ReviewDueAt.Before(time.Now().AddDate(0, 0, 29)) {
		t.Fatalf("review due date was not derived: %#v", input.ReviewDueAt)
	}
	input.Status = PageDraft
	applyReviewDefault(&input, 30)
	if input.ReviewDueAt != nil {
		t.Fatal("draft retained a review due date")
	}
	input.Status = PageRetired
	input.Version = 1
	if _, err := normalizePage(input, false); err != nil {
		t.Fatalf("retired article rejected: %v", err)
	}
	if _, err := normalizePage(PageInput{Title: "x", Body: strings.Repeat("x", MaxPageBodyBytes+1)}, true); err == nil {
		t.Fatal("oversized page body accepted")
	}
	if _, err := normalizePage(PageInput{Title: "x", Body: strings.Repeat("ą", MaxPageBodyBytes/2+1)}, true); err == nil {
		t.Fatal("UTF-8 body exceeding byte limit accepted")
	}
}

func TestNormalizePageRejectsReviewDatesOutsideMySQLRange(t *testing.T) {
	valid := []time.Time{
		time.Date(1000, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, time.December, 31, 23, 59, 59, 999999000, time.UTC),
		time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC),
	}
	for _, value := range valid {
		input, err := normalizePage(PageInput{Title: "x", Body: "x", Status: PagePublished, ReviewDueAt: &value}, true)
		if err != nil {
			t.Errorf("valid MySQL DATETIME rejected (%s): %v", value.Format(time.RFC3339Nano), err)
		}
		if input.ReviewDueAt == nil || input.ReviewDueAt.Nanosecond()%1000 != 0 {
			t.Errorf("review date was not normalized to MySQL microsecond precision: %#v", input.ReviewDueAt)
		}
	}
	invalid := []time.Time{
		time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(999, time.December, 31, 23, 59, 59, 0, time.UTC),
		time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1000, time.January, 1, 0, 0, 0, 0, time.FixedZone("east", 14*60*60)),
		time.Date(9999, time.December, 31, 23, 59, 59, 0, time.FixedZone("west", -14*60*60)),
	}
	for _, value := range invalid {
		if _, err := normalizePage(PageInput{Title: "x", Body: "x", Status: PagePublished, ReviewDueAt: &value}, true); err == nil {
			t.Errorf("review date outside the UTC MySQL range was accepted: %s", value.Format(time.RFC3339Nano))
		}
	}
}

func TestPermissionHierarchy(t *testing.T) {
	read := Actor{Permissions: map[string]bool{PermissionRead: true}}
	write := Actor{Permissions: map[string]bool{PermissionWrite: true}}
	manage := Actor{Permissions: map[string]bool{PermissionManage: true}}
	if !canRead(read) || canWrite(read) || !canRead(write) || !canWrite(write) || !canRead(manage) || !canWrite(manage) {
		t.Fatal("permission hierarchy is inconsistent")
	}
}
