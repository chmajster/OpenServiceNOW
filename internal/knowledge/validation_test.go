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
}

func TestPermissionHierarchy(t *testing.T) {
	read := Actor{Permissions: map[string]bool{PermissionRead: true}}
	write := Actor{Permissions: map[string]bool{PermissionWrite: true}}
	manage := Actor{Permissions: map[string]bool{PermissionManage: true}}
	if !canRead(read) || canWrite(read) || !canRead(write) || !canWrite(write) || !canRead(manage) || !canWrite(manage) {
		t.Fatal("permission hierarchy is inconsistent")
	}
}
