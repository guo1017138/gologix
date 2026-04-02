package gologix

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// instanceIOIPath builds the CIP path bytes for addressing a tag by its Symbol instance ID.
//
// This produces: class 0x6B (CipObject_Symbol) | instance N
// which is the same path that KepServer and other SCADA systems use to address PLC tags.
func instanceIOIPath(instance CIPInstance) []byte {
	b := bytes.Buffer{}
	b.Write(CipObject_Symbol.Bytes())
	b.Write(instance.Bytes())
	return b.Bytes()
}

func instanceReadService(tag tagDesc) CIPService {
	return readServiceForTag(tag)
}

func instanceReadFooterSize(tag tagDesc) int {
	if instanceReadService(tag) == CIPService_FragRead {
		return binary.Size(msgCIPFragIOIFooter{})
	}
	return binary.Size(msgCIPIOIFooter{})
}

func writeInstanceReadFooter(b *bytes.Buffer, tag tagDesc, offset uint32) error {
	if instanceReadService(tag) == CIPService_FragRead {
		return binary.Write(b, binary.LittleEndian, msgCIPFragIOIFooter{
			Elements: uint16(tag.Elements),
			Offset:   offset,
		})
	}
	return binary.Write(b, binary.LittleEndian, msgCIPIOIFooter{Elements: uint16(tag.Elements)})
}

// ReadMapByInstance reads multiple tags using CIP Symbol Instance IDs as map keys.
//
// This is equivalent to ReadMap but uses instance IDs instead of tag name strings.
// Instance-based reads skip ASCII name encoding in each CIP request, which is more
// efficient for large-scale repeated reads (e.g. 10000+ tags).
//
// Instance IDs can be obtained by calling ListAllTags first:
//
//	err := client.ListAllTags(0)
//	instanceID := client.KnownTags["mytag"].Instance
//
// The map values must be initialized with the correct Go types, the same as ReadMap.
// After a successful call, map values are updated with data read from the PLC.
//
// Example:
//
//	// Build the instance map from KnownTags after ListAllTags
//	m := map[gologix.CIPInstance]any{
//	    client.KnownTags["uint32"].Instance: uint32(0),
//	    client.KnownTags["testdint"].Instance: int32(0),
//	    client.KnownTags["testreal"].Instance: float32(0),
//	}
//	err := client.ReadMapByInstance(m)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Access results
//	val := m[client.KnownTags["uint32"].Instance].(uint32)
func (client *Client) ReadMapByInstance(m map[CIPInstance]any) error {
	err := client.checkConnection()
	if err != nil {
		return fmt.Errorf("could not start instance-based multi read: %w", err)
	}

	total := len(m)
	tags := make([]tagDesc, total)
	iois := make([]*tagIOI, total)
	instances := make([]CIPInstance, total)

	i := 0
	for inst, v := range m {
		ct, elem := GoVarToCIPType(v)
		path := instanceIOIPath(inst)
		tags[i] = tagDesc{
			TagName:  fmt.Sprintf("@%d", inst),
			TagType:  ct,
			Elements: elem,
			Struct:   v,
		}
		iois[i] = &tagIOI{
			Path:   fmt.Sprintf("@%d", inst),
			Type:   ct,
			Buffer: path,
		}
		instances[i] = inst
		i++
	}

	resultValues := make([]any, total)
	pending := make([]int, total)
	for i := range pending {
		pending[i] = i
	}

	for len(pending) > 0 {
		selectedOrig, chosenTags, chosenIOIs, nextPending, err := selectPackedTagBatch(client, pending, tags, iois)
		if err != nil {
			return err
		}
		subResults, err := client.readListFragWithIOIsAll(chosenTags, chosenIOIs)
		if err != nil {
			return err
		}
		for i, val := range subResults {
			resultValues[selectedOrig[i]] = val
		}
		pending = nextPending
	}

	for i := range resultValues {
		m[instances[i]] = resultValues[i]
	}
	return nil
}

// countInstanceIOIsThatFit calculates how many instance-based tag requests fit within
// the connection size limit (request bytes + expected response bytes).
func countInstanceIOIsThatFit(client *Client, tags []tagDesc, iois []*tagIOI) (int, error) {
	ioiHdrSize := binary.Size(msgCIPMultiIOIHeader{})

	b := bytes.Buffer{}
	n := 1
	responseSize := 0

	for i, tag := range tags {
		ioi := iois[i]

		candidateCount := i + 1
		newSize := estimateMultiReadRequestOverhead(candidateCount)
		newSize += b.Len()
		newSize += ioiHdrSize + len(ioi.Buffer) + instanceReadFooterSize(tag)

		candidateRespSize := estimateMultiReadReplyOverhead(candidateCount) + responseSize + estimateTagResponseSize(tag)
		if newSize >= int(client.ConnectionSize) || candidateRespSize >= int(client.ConnectionSize) {
			break
		}
		responseSize += estimateTagResponseSize(tag)

		h := msgCIPMultiIOIHeader{
			Service: instanceReadService(tag),
			Size:    byte(len(ioi.Buffer) / 2),
		}

		if err := binary.Write(&b, binary.LittleEndian, h); err != nil {
			return 0, fmt.Errorf("problem writing cip IO header to buffer: %w", err)
		}
		b.Write(ioi.Buffer)
		if err := writeInstanceReadFooter(&b, tag, 0); err != nil {
			return 0, fmt.Errorf("problem writing ioi footer to buffer: %w", err)
		}

		n = candidateCount
	}

	if n < 1 {
		n = 1
	}
	return n, nil
}

// readListWithIOIs sends a Multiple Service Request (0x4C) with pre-built IOI paths,
// avoiding the tag-name-to-IOI conversion step. Used by ReadMapByInstance.
func (client *Client) readListWithIOIs(tags []tagDesc, iois []*tagIOI) ([]any, error) {
	qty := len(tags)

	reqItems := make([]CIPItem, 2)
	reqItems[0] = newItem(cipItem_ConnectionAddress, &client.OTNetworkConnectionID)

	ioiHeader := msgCIPConnectedMultiServiceReq{
		Sequence:     uint16(sequencer()),
		Service:      CIPService_MultipleService,
		PathSize:     2,
		Path:         [4]byte{0x20, 0x02, 0x24, 0x01},
		ServiceCount: uint16(qty),
	}

	b := bytes.Buffer{}
	jumpTable := make([]uint16, qty)
	jumpStart := 2 + qty*2
	for i := 0; i < qty; i++ {
		jumpTable[i] = uint16(jumpStart + b.Len())
		h := msgCIPMultiIOIHeader{
			Service: instanceReadService(tags[i]),
			Size:    byte(len(iois[i].Buffer) / 2),
		}

		if err := binary.Write(&b, binary.LittleEndian, h); err != nil {
			return nil, fmt.Errorf("problem writing cip IO header: %w", err)
		}
		b.Write(iois[i].Buffer)
		if err := writeInstanceReadFooter(&b, tags[i], 0); err != nil {
			return nil, fmt.Errorf("problem writing ioi footer: %w", err)
		}
	}
	reqItems[1] = CIPItem{Header: cipItemHeader{ID: cipItem_ConnectedData}}
	if err := reqItems[1].Serialize(ioiHeader, jumpTable, &b); err != nil {
		return nil, fmt.Errorf("problem serializing item header: %w", err)
	}

	itemData, err := serializeItems(reqItems)
	if err != nil {
		return nil, err
	}

	hdr, data, err := client.send_recv_data(cipCommandSendUnitData, itemData)
	if err != nil {
		return nil, err
	}
	if hdr.Status != 0 {
		return nil, fmt.Errorf("problem in instance-based read. Status %v", CIPStatus(hdr.Status))
	}

	readResultHeader := msgCSDHeader{}
	if err := binary.Read(data, binary.LittleEndian, &readResultHeader); err != nil {
		client.Logger.Warn("Problem reading result header", "error", err)
	}

	items, err := readItems(data)
	if err != nil {
		return nil, fmt.Errorf("problem reading items: %w", err)
	}
	if len(items) != 2 {
		return nil, fmt.Errorf("wrong Number of Items. Expected 2 but got %v", len(items))
	}

	rItem := items[1]

	// parse multi-service response header
	seqCount, err := rItem.Uint16()
	if err != nil {
		return nil, fmt.Errorf("problem reading reply sequence count: %w", err)
	}
	_ = seqCount

	svcByte, err := rItem.Byte()
	if err != nil {
		return nil, fmt.Errorf("problem reading reply service code: %w", err)
	}
	_ = svcByte

	_, err = rItem.Byte() // reserved/padding
	if err != nil {
		return nil, fmt.Errorf("problem reading reply padding byte: %w", err)
	}

	status, err := rItem.Uint16()
	if err != nil {
		return nil, fmt.Errorf("problem reading reply status: %w", err)
	}
	if status != uint16(CIPStatus_OK) && status != uint16(CIPStatus_EmbeddedServiceError) {
		return nil, fmt.Errorf("instance read service returned status %v", CIPStatus(status))
	}

	replyCount, err := rItem.Uint16()
	if err != nil {
		return nil, fmt.Errorf("problem reading reply item count: %w", err)
	}

	offsetTable := make([]uint16, replyCount)
	if err := binary.Read(&rItem, binary.LittleEndian, &offsetTable); err != nil {
		return nil, fmt.Errorf("problem reading offset table: %w", err)
	}

	rb, err := rItem.Bytes()
	if err != nil {
		return nil, err
	}

	resultValues := make([]any, replyCount)
	for i := 0; i < int(replyCount); i++ {
		offset := int(offsetTable[i]) + 10 // offset doesn't start at 0 in the item
		myBytes := bytes.NewBuffer(rb[offset:])

		rHdr := msgMultiReadResult{}
		if err := binary.Read(myBytes, binary.LittleEndian, &rHdr); err != nil {
			return nil, fmt.Errorf("problem reading multi result header for item %d: %w", i, err)
		}

		if !rHdr.Service.IsResponse() {
			return nil, fmt.Errorf("item %d was not a response service: got %v", i, rHdr.Service)
		}
		rHdr.Service = rHdr.Service.UnResponse()
		if rHdr.Service != CIPService_Read && rHdr.Service != CIPService_FragRead {
			return nil, fmt.Errorf("item %d had unexpected service %v", i, rHdr.Service)
		}

		cipStatus := CIPStatus(rHdr.Status & 0xFF)
		if cipStatus != CIPStatus_OK {
			resultValues[i] = fmt.Errorf("problem reading %s. Status %v", tags[i].TagName, cipStatus)
			continue
		}

		payload := myBytes.Bytes()
		val, err := decodeReadTagValue(tags[i], iois[i], rHdr.Type, payload)
		if err != nil {
			return nil, fmt.Errorf("problem decoding value for instance %s: %w", tags[i].TagName, err)
		}
		resultValues[i] = val
	}

	return resultValues, nil
}

func (client *Client) readListFragWithIOIsAll(tags []tagDesc, iois []*tagIOI) ([]any, error) {
	qty := len(tags)
	if qty != len(iois) {
		return nil, fmt.Errorf("mismatched tag and IOI counts for instance-based fragmented read")
	}

	offsets := make([]uint32, qty)
	chunks := make([]bytes.Buffer, qty)
	respTypes := make([]CIPType, qty)
	pending := make([]int, qty)
	for i := range pending {
		pending[i] = i
	}

	for rounds := 0; len(pending) > 0; rounds++ {
		if rounds > 1024 {
			return nil, fmt.Errorf("instance-based fragmented read exceeded max continuation rounds")
		}

		roundTags := make([]tagDesc, len(pending))
		roundIOIs := make([]*tagIOI, len(pending))
		roundOffsets := make([]uint32, len(pending))
		for i, idx := range pending {
			roundTags[i] = tags[idx]
			roundIOIs[i] = iois[idx]
			roundOffsets[i] = offsets[idx]
		}

		roundResults, err := client.readListFragRound(roundTags, roundIOIs, roundOffsets)
		if err != nil {
			return nil, err
		}

		nextPending := make([]int, 0, len(pending))
		for i, rr := range roundResults {
			idx := pending[i]
			if rr.Status != CIPStatus_OK && rr.Status != CIPStatus_PartialTransfer {
				return nil, fmt.Errorf("problem reading tag %s. Status %v", tags[idx].TagName, rr.Status)
			}

			if respTypes[idx] == 0 {
				respTypes[idx] = rr.Type
			}

			if len(rr.Data) > 0 {
				if _, err := chunks[idx].Write(rr.Data); err != nil {
					return nil, fmt.Errorf("problem accumulating fragmented payload for %s: %w", tags[idx].TagName, err)
				}
				offsets[idx] += uint32(len(rr.Data))
			}

			if rr.Status == CIPStatus_PartialTransfer {
				nextPending = append(nextPending, idx)
			}
		}
		pending = nextPending
	}

	resultValues := make([]any, qty)
	for i := range tags {
		val, err := decodeReadTagValue(tags[i], iois[i], respTypes[i], chunks[i].Bytes())
		if err != nil {
			return nil, fmt.Errorf("problem decoding fragmented instance read value for %s: %w", tags[i].TagName, err)
		}
		resultValues[i] = val
	}

	return resultValues, nil
}
