package workrecords

import (
	"errors"
	"testing"
)

func TestValidateSequenceInput(t *testing.T) {
	tests := []struct {
		name string
		tenantID uint64
		recordType string
		prefix string
		width int
		wantErr bool
	}{
		{name: "valid", tenantID: 1, recordType: "incident", prefix: "INC", width: 7},
		{name: "zero tenant", recordType: "incident", prefix: "INC", width: 7, wantErr: true},
		{name: "invalid record type", tenantID: 1, recordType: "Incident!", prefix: "INC", width: 7, wantErr: true},
		{name: "invalid prefix", tenantID: 1, recordType: "incident", prefix: "inc", width: 7, wantErr: true},
		{name: "invalid width", tenantID: 1, recordType: "incident", prefix: "INC", width: 21, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSequenceInput(test.tenantID, test.recordType, test.prefix, test.width)
			if test.wantErr && !errors.Is(err, ErrInvalidSequenceConfiguration) {
				t.Fatalf("error = %v, want ErrInvalidSequenceConfiguration", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	if got := formatNumber("INC", 7, 42); got != "INC0000042" {
		t.Fatalf("formatNumber() = %q", got)
	}
	if got := formatNumber("TASK", 2, 123); got != "TASK123" {
		t.Fatalf("formatNumber() preserves overflow digits = %q", got)
	}
}
