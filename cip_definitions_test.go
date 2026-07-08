package gologix

import (
	"strings"
	"testing"
)

func TestCIPExtendedStatusString(t *testing.T) {
	tests := []struct {
		name string
		code CIPExtendedStatus
		want string
	}{
		{
			name: "tag offset",
			code: CIPExtendedStatus_BeginningOffsetBeyondTemplateEnd,
			want: "The beginning offset was beyond the end of the template.",
		},
		{
			name: "duplicate forward open",
			code: CIPExtendedStatus_ConnectionInUseOrDuplicateForwardOpen,
			want: "Connection in Use or Duplicate Forward Open.",
		},
		{
			name: "unknown",
			code: 0xFFFF,
			want: "Unknown CIPExtendedStatus: 0xFFFF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.code.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCIPExtendedStatusIsWordSized(t *testing.T) {
	if strings.Contains(CIPExtendedStatus_AbbreviatedTypeMismatch.String(), "0x") {
		t.Fatalf("0x2107 should be a known 16-bit extended status")
	}
}
