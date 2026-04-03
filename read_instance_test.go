package gologix

import (
	"encoding/binary"
	"testing"
)

func TestInstanceReadService(t *testing.T) {
	if got := instanceReadService(tagDesc{TagType: CIPTypeStruct}); got != CIPService_FragRead {
		t.Fatalf("struct tags should use fragmented read service 0x52, got %v", got)
	}

	if got := instanceReadService(tagDesc{TagType: CIPTypeDINT, Elements: 16}); got != CIPService_FragRead {
		t.Fatalf("array tags should use fragmented read service 0x52, got %v", got)
	}

	if got := instanceReadService(tagDesc{TagType: CIPTypeSTRING, Elements: 1}, 96); got != CIPService_FragRead {
		t.Fatalf("oversized scalar responses should use fragmented read service 0x52, got %v", got)
	}

	if got := instanceReadService(tagDesc{TagType: CIPTypeDINT, Elements: 1}); got != CIPService_Read {
		t.Fatalf("scalar atomic tags should keep using read service 0x4c, got %v", got)
	}
}

func TestInstanceReadFooterSize(t *testing.T) {
	if got := instanceReadFooterSize(tagDesc{TagType: CIPTypeStruct}); got != binary.Size(msgCIPFragIOIFooter{}) {
		t.Fatalf("struct tags should use fragmented footer size %d, got %d", binary.Size(msgCIPFragIOIFooter{}), got)
	}

	if got := instanceReadFooterSize(tagDesc{TagType: CIPTypeDINT}); got != binary.Size(msgCIPIOIFooter{}) {
		t.Fatalf("atomic tags should use normal footer size %d, got %d", binary.Size(msgCIPIOIFooter{}), got)
	}
}
