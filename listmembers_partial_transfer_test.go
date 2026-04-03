package gologix

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func encodeTemplateAttrResponse(t *testing.T, status CIPStatus) []byte {
	t.Helper()

	result := msgGetTemplateAttrListResponse{
		SequenceCount:      7,
		Service:            CIPService(byte(CIPService_GetAttributeList) | 0x80),
		Status:             status,
		Count:              4,
		SizeWords_ID:       4,
		SizeWords_Status:   uint16(CIPStatus_OK),
		SizeWords:          64,
		SizeBytes_ID:       5,
		SizeBytes_Status:   uint16(CIPStatus_OK),
		SizeBytes:          128,
		MemberCount_ID:     2,
		MemberCount_Status: uint16(CIPStatus_OK),
		MemberCount:        3,
		Handle_ID:          1,
		Handle_Status:      uint16(CIPStatus_OK),
		Handle:             99,
	}

	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, result); err != nil {
		t.Fatalf("failed to encode template attr response: %v", err)
	}
	return buf.Bytes()
}

func TestParseTemplateAttrResponseOK(t *testing.T) {
	result, err := parseTemplateAttrResponse(encodeTemplateAttrResponse(t, CIPStatus_OK))
	if err != nil {
		t.Fatalf("parseTemplateAttrResponse returned error: %v", err)
	}
	if result.SizeWords != 64 || result.MemberCount != 3 || result.Handle != 99 {
		t.Fatalf("unexpected parsed result: %+v", result)
	}
}

func TestParseTemplateAttrResponseRejectsPartialTransfer(t *testing.T) {
	_, err := parseTemplateAttrResponse(encodeTemplateAttrResponse(t, CIPStatus_PartialTransfer))
	if err == nil {
		t.Fatal("expected error for partial transfer, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCollectMemberReadPayloadAggregatesPartialTransfer(t *testing.T) {
	chunk1 := []byte{1, 2, 3, 4}
	chunk2 := []byte{5, 6}

	type call struct {
		offset     uint32
		readLength uint16
	}
	var calls []call

	payload, err := collectMemberReadPayload(6, func(startOffset uint32, readLength uint16) (msgMemberInfoHdr, []byte, error) {
		calls = append(calls, call{offset: startOffset, readLength: readLength})
		switch len(calls) {
		case 1:
			return msgMemberInfoHdr{Status: uint16(CIPStatus_PartialTransfer)}, chunk1, nil
		case 2:
			return msgMemberInfoHdr{Status: uint16(CIPStatus_OK)}, chunk2, nil
		default:
			t.Fatalf("unexpected fetch call %d", len(calls))
			return msgMemberInfoHdr{}, nil, nil
		}
	})
	if err != nil {
		t.Fatalf("collectMemberReadPayload returned error: %v", err)
	}

	if !bytes.Equal(payload, []byte{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("payload mismatch: got %v want %v", payload, []byte{1, 2, 3, 4, 5, 6})
	}

	wantCalls := []call{{offset: 0, readLength: 6}, {offset: 4, readLength: 2}}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("request sequence mismatch: got %+v want %+v", calls, wantCalls)
	}
}

func TestCollectMemberReadPayloadRejectsEmptyPartialTransfer(t *testing.T) {
	_, err := collectMemberReadPayload(8, func(startOffset uint32, readLength uint16) (msgMemberInfoHdr, []byte, error) {
		return msgMemberInfoHdr{Status: uint16(CIPStatus_PartialTransfer)}, nil, nil
	})
	if err == nil {
		t.Fatal("expected error for empty partial transfer, got nil")
	}
	if !strings.Contains(err.Error(), "empty payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}
