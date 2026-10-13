package workrecords

import (
	"strconv"
	"strings"
)

func ChoiceID(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != raw {
		return 0, ErrInvalid
	}
	return id, nil
}

// RestrictChoice retains the original tenant/role predicates and binds one ID.
// SQL expressions are constants supplied by domain code, never HTTP values.
// Call before appending any LIMIT parameter.
func RestrictChoice(query string, args []any, column string, id uint64) (string, []any) {
	if id == 0 {
		return query, args
	}
	index := strings.Index(query, " ORDER BY")
	if index < 0 {
		index = len(query)
	}
	return query[:index] + " AND " + column + "=?" + query[index:], append(args, id)
}
