package knowledge

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var spaceKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,15}$`)

func normalizeSpace(input SpaceInput, creating bool) (SpaceInput, error) {
	input.Key = strings.ToUpper(strings.TrimSpace(input.Key))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Status == "" {
		input.Status = SpaceActive
	}
	if !spaceKeyPattern.MatchString(input.Key) || !text(input.Name, 1, 160) ||
		!text(input.Description, 0, 10000) || input.ReviewIntervalDays > 3650 ||
		(input.Status != SpaceActive && input.Status != SpaceArchived) ||
		(!creating && input.Version == 0) {
		return input, ErrInvalid
	}
	return input, nil
}

func normalizePage(input PageInput, creating bool) (PageInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Status == "" {
		input.Status = PageDraft
	}
	if !text(input.Title, 1, 200) || !utf8.ValidString(input.Body) ||
		len(input.Body) > MaxPageBodyBytes ||
		(input.Status != PageDraft && input.Status != PagePublished && input.Status != PageRetired) ||
		(!creating && input.Version == 0) {
		return input, ErrInvalid
	}
	if input.ReviewDueAt != nil {
		value := input.ReviewDueAt.UTC()
		input.ReviewDueAt = &value
	}
	return input, nil
}

func text(value string, minRunes, maxRunes int) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	return length >= minRunes && length <= maxRunes
}

func canRead(actor Actor) bool {
	return actor.Has(PermissionRead) || actor.Has(PermissionWrite) || actor.Has(PermissionManage)
}

func canWrite(actor Actor) bool {
	return actor.Has(PermissionWrite) || actor.Has(PermissionManage)
}
