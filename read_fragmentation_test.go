package gologix

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestEstimateTagResponseSizeForStruct(t *testing.T) {
	tag := tagDesc{
		TagName:  "LargeUDT",
		TagType:  CIPTypeStruct,
		Elements: 1,
		Struct: struct {
			Count int32
			Name  string
			Data  [64]byte
		}{},
	}

	got := estimateTagResponseSize(tag)
	if got <= 0 {
		t.Fatalf("expected a positive packed size, got %d", got)
	}
}

func TestCountInstanceIOIsThatFitLimitsLargeStructBatch(t *testing.T) {
	client := &Client{ConnectionSize: 120}
	largeUDT := struct {
		Count int32
		Name  string
		Data  [64]byte
	}{}

	tags := []tagDesc{
		{TagName: "@1", TagType: CIPTypeStruct, Elements: 1, Struct: largeUDT},
		{TagName: "@2", TagType: CIPTypeStruct, Elements: 1, Struct: largeUDT},
	}
	iois := []*tagIOI{
		{Path: "@1", Type: CIPTypeStruct, Buffer: []byte{0x20, 0x6b, 0x24, 0x01}},
		{Path: "@2", Type: CIPTypeStruct, Buffer: []byte{0x20, 0x6b, 0x24, 0x02}},
	}

	got, err := countInstanceIOIsThatFit(client, tags, iois)
	if err != nil {
		t.Fatalf("countInstanceIOIsThatFit returned error: %v", err)
	}
	if got != 1 {
		t.Fatalf("expected one large fragmented tag per batch, got %d", got)
	}
}

func TestReadServiceForTag(t *testing.T) {
	if got := readServiceForTag(tagDesc{TagType: CIPTypeStruct}); got != CIPService_FragRead {
		t.Fatalf("struct tags should use fragmented read service 0x52, got %v", got)
	}

	if got := readServiceForTag(tagDesc{TagType: CIPTypeDINT, Elements: 8}); got != CIPService_FragRead {
		t.Fatalf("array tags should use fragmented read service 0x52, got %v", got)
	}

	if got := readServiceForTag(tagDesc{TagType: CIPTypeSTRING, Elements: 1}, 96); got != CIPService_FragRead {
		t.Fatalf("oversized scalar responses should use fragmented read service 0x52, got %v", got)
	}

	if got := readServiceForTag(tagDesc{TagType: CIPTypeDINT, Elements: 1}); got != CIPService_Read {
		t.Fatalf("scalar atomic tags should use normal read service 0x4C, got %v", got)
	}
}

func TestGoVarToCIPTypeStructSliceUsesLength(t *testing.T) {
	type sampleUDT struct {
		Count int32
	}

	gotType, gotElems := GoVarToCIPType(make([]sampleUDT, 3))
	if gotType != CIPTypeStruct {
		t.Fatalf("expected CIPTypeStruct for struct slice, got %v", gotType)
	}
	if gotElems != 3 {
		t.Fatalf("expected struct slice length 3, got %d", gotElems)
	}
}

func TestDecodeReadTagValueStructSlice(t *testing.T) {
	type sampleUDT struct {
		Count int32
		Value float32
	}

	want := []sampleUDT{{Count: 1, Value: 1.5}, {Count: 2, Value: 2.5}}
	payload := bytes.Buffer{}
	if err := binary.Write(&payload, binary.LittleEndian, cipStructHeader{}); err != nil {
		t.Fatalf("failed to write struct header: %v", err)
	}
	for _, item := range want {
		if _, err := Pack(&payload, item); err != nil {
			t.Fatalf("failed to pack sample UDT: %v", err)
		}
	}

	gotAny, err := decodeReadTagValue(tagDesc{
		TagName:  "Program:SampleUDTs[0]",
		TagType:  CIPTypeStruct,
		Elements: len(want),
		Struct:   make([]sampleUDT, len(want)),
	}, &tagIOI{}, CIPTypeStruct, payload.Bytes())
	if err != nil {
		t.Fatalf("decodeReadTagValue returned error: %v", err)
	}

	got, ok := gotAny.([]sampleUDT)
	if !ok {
		t.Fatalf("expected []sampleUDT result, got %T", gotAny)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded struct slice mismatch: got %+v want %+v", got, want)
	}
}

func TestSingleReadServiceUsesFragReadWhenReplyTooLarge(t *testing.T) {
	client := &Client{ConnectionSize: 96}
	if got := client.singleReadService("LargeString", CIPTypeSTRING, 1); got != CIPService_FragRead {
		t.Fatalf("expected oversized single-tag read to use 0x52, got %v", got)
	}
	if got := client.singleReadService("AtomicDint", CIPTypeDINT, 1); got != CIPService_Read {
		t.Fatalf("expected small single-tag read to keep using 0x4c, got %v", got)
	}
}
