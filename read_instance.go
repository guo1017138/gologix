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

	resultValues := make([]any, 0, total)
	n := 0
	for n < total {
		nNew, err := countInstanceIOIsThatFit(client, tags[n:], iois[n:])
		if err != nil {
			return err
		}
		subResults, err := client.readListWithIOIs(tags[n:n+nNew], iois[n:n+nNew])
		n += nNew
		if err != nil {
			return err
		}
		resultValues = append(resultValues, subResults...)
	}

	for i := range resultValues {
		m[instances[i]] = resultValues[i]
	}
	return nil
}

// countInstanceIOIsThatFit calculates how many instance-based tag requests fit within
// the connection size limit (request bytes + expected response bytes).
func countInstanceIOIsThatFit(client *Client, tags []tagDesc, iois []*tagIOI) (int, error) {
	qty := len(tags)

	ioiHeader := msgCIPConnectedMultiServiceReq{
		Sequence:     uint16(sequencer()),
		Service:      CIPService_MultipleService,
		PathSize:     2,
		Path:         [4]byte{0x20, 0x02, 0x24, 0x01},
		ServiceCount: uint16(qty),
	}

	mainHdrSize := binary.Size(ioiHeader)
	ioiHdrSize := binary.Size(msgCIPMultiIOIHeader{})
	ioiFtrSize := binary.Size(msgCIPIOIFooter{})

	b := bytes.Buffer{}
	n := 1
	responseSize := 0

	for i, tag := range tags {
		ioi := iois[i]

		newSize := mainHdrSize
		newSize += 2 * n
		newSize += b.Len()
		newSize += ioiHdrSize + len(ioi.Buffer) + ioiFtrSize

		responseSize += tags[i].TagType.Size() * tags[i].Elements
		if newSize >= int(client.ConnectionSize) || responseSize >= int(client.ConnectionSize) {
			break
		}

		h := msgCIPMultiIOIHeader{
			Service: CIPService_Read,
			Size:    byte(len(ioi.Buffer) / 2),
		}
		f := msgCIPIOIFooter{Elements: uint16(tag.Elements)}

		if err := binary.Write(&b, binary.LittleEndian, h); err != nil {
			return 0, fmt.Errorf("problem writing cip IO header to buffer: %w", err)
		}
		b.Write(ioi.Buffer)
		if err := binary.Write(&b, binary.LittleEndian, f); err != nil {
			return 0, fmt.Errorf("problem writing ioi footer to buffer: %w", err)
		}

		n = i + 1
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
			Service: CIPService_Read,
			Size:    byte(len(iois[i].Buffer) / 2),
		}
		f := msgCIPIOIFooter{Elements: uint16(tags[i].Elements)}

		if err := binary.Write(&b, binary.LittleEndian, h); err != nil {
			return nil, fmt.Errorf("problem writing cip IO header: %w", err)
		}
		b.Write(iois[i].Buffer)
		if err := binary.Write(&b, binary.LittleEndian, f); err != nil {
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

	readResultHeader := msgCIPResultHeader{}
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
