package password

import (
	"strings"
	"testing"
)

func TestUnicodePasswordPolicy(t *testing.T) {
	for _, test := range []struct {
		value string
		valid bool
	}{
		{strings.Repeat("😀", 11), false}, {strings.Repeat("😀", 12), true},
		{strings.Repeat("😀", 256), true}, {strings.Repeat("😀", 257), false},
		{string([]byte{255}) + strings.Repeat("a", 12), false},
	} {
		if got := Validate(test.value) == nil; got != test.valid {
			t.Fatalf("validation for %d bytes=%t, want %t", len(test.value), got, test.valid)
		}
	}
}
