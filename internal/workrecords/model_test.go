package workrecords

import "testing"

func TestValidObjectRef(t *testing.T) {
	if !validObjectRef(1, "incident", "INC000001") {
		t.Fatal("expected valid object reference")
	}
	for _, test := range []struct {
		tenant uint64
		objectType string
		objectID string
	}{
		{0, "incident", "1"},
		{1, "", "1"},
		{1, "incident", ""},
	} {
		if validObjectRef(test.tenant, test.objectType, test.objectID) {
			t.Fatalf("unexpected valid reference: %+v", test)
		}
	}
}

func TestMessageTypesAreDistinct(t *testing.T) {
	if MessageComment == MessageWorkNote {
		t.Fatal("comment and work note types must remain distinct")
	}
}
