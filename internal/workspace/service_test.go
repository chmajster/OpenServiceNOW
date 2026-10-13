package workspace

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQueryValidation(t *testing.T) {
	for _, q := range []string{"ab", "INC0000001", "żółć", "' OR 1=1 --", "%_"} {
		if err := ValidateQuery(q); err != nil {
			t.Fatalf("valid query %q: %v", q, err)
		}
	}
	for _, q := range []string{"", "a", "a\nb", strings.Repeat("ą", 201), string([]byte{0xff, 0xfe})} {
		if ValidateQuery(q) == nil {
			t.Fatalf("accepted %q", q)
		}
	}
}
func TestCountPreservesBigInt(t *testing.T) {
	b, err := json.Marshal(Metric{Count: 9007199254740993})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"count":"9007199254740993"`) {
		t.Fatal(string(b))
	}
}
