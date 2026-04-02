package gologix

import (
	"encoding/binary"
	"reflect"
	"testing"
)

func TestEstimateTagResponseSizeIncludesResultHeader(t *testing.T) {
	tag := tagDesc{TagName: "TagA", TagType: CIPTypeDINT, Elements: 1}
	want := binary.Size(msgMultiReadResult{}) + int(CIPTypeDINT.Size())
	if got := estimateTagResponseSize(tag); got != want {
		t.Fatalf("unexpected estimated DINT response size: got %d want %d", got, want)
	}
}

func TestEstimateTagResponseSizeIncludesStructHeader(t *testing.T) {
	tag := tagDesc{
		TagName:  "UDT1",
		TagType:  CIPTypeStruct,
		Elements: 1,
		Struct: struct {
			Count int32
			Value float32
		}{},
	}
	minWant := binary.Size(msgMultiReadResult{}) + binary.Size(cipStructHeader{})
	if got := estimateTagResponseSize(tag); got <= minWant {
		t.Fatalf("expected struct response estimate to include payload beyond headers, got %d", got)
	}
}

func TestSelectPackedTagIndexesSkipsOversizedGap(t *testing.T) {
	client := &Client{ConnectionSize: 90}
	largeUDT := struct {
		Count int32
		Data  [100]byte
	}{}

	tags := []tagDesc{
		{TagName: "TagA", TagType: CIPTypeDINT, Elements: 8},
		{TagName: "TagB", TagType: CIPTypeStruct, Elements: 1, Struct: largeUDT},
		{TagName: "TagC", TagType: CIPTypeINT, Elements: 4},
	}
	iois := []*tagIOI{
		{Path: "TagA", Type: CIPTypeDINT, Buffer: []byte{0x91, 0x04, 'T', 'a', 'g', 'A'}},
		{Path: "TagB", Type: CIPTypeStruct, Buffer: []byte{0x91, 0x04, 'T', 'a', 'g', 'B'}},
		{Path: "TagC", Type: CIPTypeINT, Buffer: []byte{0x91, 0x04, 'T', 'a', 'g', 'C'}},
	}

	got, err := selectPackedTagIndexes(client, tags, iois)
	if err != nil {
		t.Fatalf("selectPackedTagIndexes returned error: %v", err)
	}

	want := []int{0, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected packed indexes: got %v want %v", got, want)
	}
}

func TestSelectPackedTagIndexesIncludesResponseHeaderOverhead(t *testing.T) {
	client := &Client{ConnectionSize: 31}
	tags := []tagDesc{
		{TagName: "TagA", TagType: CIPTypeDINT, Elements: 1},
		{TagName: "TagB", TagType: CIPTypeDINT, Elements: 1},
	}
	iois := []*tagIOI{
		{Path: "TagA", Type: CIPTypeDINT, Buffer: []byte{0x20, 0x6b, 0x24, 0x01}},
		{Path: "TagB", Type: CIPTypeDINT, Buffer: []byte{0x20, 0x6b, 0x24, 0x02}},
	}

	got, err := selectPackedTagIndexes(client, tags, iois)
	if err != nil {
		t.Fatalf("selectPackedTagIndexes returned error: %v", err)
	}

	want := []int{0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected fixed response header to limit batch to one tag, got %v want %v", got, want)
	}
}
