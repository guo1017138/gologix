package gologix

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// this is specifically the response for a GetAttrList service on a
// template object with requested attributes of 4,5,2,1
// where
//
//	4 = Size of the template in 32 bit words
//	5 = Size of the data in the template (when sent in a read response)
//	2 = Number of fields/members in the template
//	1 = The handle of the template. not sure what this is for yet
type msgGetTemplateAttrListResponse struct {
	SequenceCount   uint16
	Service         CIPService
	Reserved        byte
	Status          CIPStatus
	Status_extended byte
	Count           uint16

	// this is the size of the TEMPLATE data of the structure when read
	SizeWords_ID     uint16
	SizeWords_Status uint16
	SizeWords        uint32

	// this is the size of the DATA in the structure when read.
	SizeBytes_ID     uint16
	SizeBytes_Status uint16
	SizeBytes        uint32

	MemberCount_ID     uint16
	MemberCount_Status uint16
	MemberCount        uint16

	Handle_ID     uint16
	Handle_Status uint16
	Handle        uint16
}

func (client *Client) GetTemplateInstanceAttr(str_instance uint32) (msgGetTemplateAttrListResponse, error) {
	client.Logger.Debug("list members", "instance", str_instance)

	// have to start at 1.
	if str_instance == 0 {
		str_instance = 1
	}

	reqitems := make([]CIPItem, 2)
	//reqitems[0] = cipItem{Header: cipItemHeader{ID: cipItem_Null}}
	reqitems[0] = newItem(cipItem_ConnectionAddress, &client.OTNetworkConnectionID)

	p, err := Serialize(
		CipObject_Template, CIPInstance(str_instance),
		//cipObject_Symbol, cipInstance(start_instance),
	)
	if err != nil {
		return msgGetTemplateAttrListResponse{}, fmt.Errorf("couldn't build path. %w", err)
	}

	readmsg := msgCIPConnectedServiceReq{
		SequenceCount: uint16(sequencer()),
		Service:       CIPService_GetAttributeList,
		PathLength:    byte(p.Len() / 2),
	}

	reqitems[1] = newItem(cipItem_ConnectedData, readmsg)
	err = reqitems[1].Serialize(p.Bytes())
	if err != nil {
		return msgGetTemplateAttrListResponse{}, fmt.Errorf("problem serializing path: %w", err)
	}
	number_of_attr_to_receive := 4
	attr_Size_32bitWords := 4
	attr_Size_Bytes := 5
	attr_MemberCount := 2
	attr_symbol_type := 1
	err = reqitems[1].Serialize([5]uint16{
		uint16(number_of_attr_to_receive),
		uint16(attr_Size_32bitWords),
		uint16(attr_Size_Bytes),
		uint16(attr_MemberCount),
		uint16(attr_symbol_type),
	})
	// the attributes for the template object are
	// 1 uint16 = Template Handle (Is this the CRC?)
	// 2 uint16 = Number of members
	// 3 uint16 = Size of the template in 32 bit words
	// 4 uint32 = Size of the template in bytes
	// 5 uint32 = Size of the data in the template (when sent in a read response)
	// 6 uint16 = Family type  (0 for UDT, 1 for string?)
	// 7 uint32 = Multiply Code?
	// 8 uint8  = Recon Data

	itemdata, err := serializeItems(reqitems)
	if err != nil {
		return msgGetTemplateAttrListResponse{}, fmt.Errorf("problem serializing item data: %w", err)
	}
	hdr, data, err := client.send_recv_data(cipCommandSendUnitData, itemdata)
	if err != nil {
		return msgGetTemplateAttrListResponse{}, err
	}
	_ = hdr
	_ = data
	//data_hdr := ListInstanceHeader{}
	//binary.Read(data, binary.LittleEndian, &data_hdr)

	// first six bytes are zero.
	padding := make([]byte, 6)
	_, err = data.Read(padding)
	if err != nil {
		return msgGetTemplateAttrListResponse{}, fmt.Errorf("problem reading padding. %w", err)
	}

	resp_items, err := readItems(data)
	if err != nil {
		return msgGetTemplateAttrListResponse{}, fmt.Errorf("couldn't parse items. %w", err)
	}

	if len(resp_items) < 2 {
		return msgGetTemplateAttrListResponse{}, fmt.Errorf("wrong number of items for template attr read. expected at least 2 but got %d", len(resp_items))
	}

	result, err := parseTemplateAttrResponse(resp_items[1].Data)
	if err != nil {
		return result, err
	}

	return result, nil
}

type msgMemberInfoHdr struct {
	SequenceCount uint16
	Service       CIPService
	Reserved      byte
	Status        uint16
}
type msgMemberInfo struct {
	Info   uint16
	Type   uint16
	Offset uint32
}

func (m msgMemberInfo) CIPType() CIPType {
	return CIPType(m.Type & 0x00FF)
}

type memberReadChunkFetcher func(startOffset uint32, readLength uint16) (msgMemberInfoHdr, []byte, error)

func collectMemberReadPayload(totalBytes uint32, fetch memberReadChunkFetcher) ([]byte, error) {
	if totalBytes == 0 {
		return nil, nil
	}

	var payload bytes.Buffer
	var offset uint32

	for rounds := 0; rounds < 1024; rounds++ {
		remaining := totalBytes - offset
		if remaining == 0 {
			return payload.Bytes(), nil
		}

		requestLen := uint16(remaining)
		if remaining > 0xFFFF {
			requestLen = 0xFFFF
		}

		hdr, chunk, err := fetch(offset, requestLen)
		if err != nil {
			return nil, err
		}

		status := CIPStatus(hdr.Status)
		if status != CIPStatus_OK && status != CIPStatus_PartialTransfer {
			return nil, fmt.Errorf("template member read returned status %v", status)
		}

		if len(chunk) > 0 {
			if _, err := payload.Write(chunk); err != nil {
				return nil, fmt.Errorf("problem accumulating template member payload: %w", err)
			}
			offset += uint32(len(chunk))
		}

		if status != CIPStatus_PartialTransfer {
			return payload.Bytes(), nil
		}
		if len(chunk) == 0 {
			return nil, fmt.Errorf("template member read returned partial transfer with empty payload at offset %d", offset)
		}
	}

	return nil, fmt.Errorf("template member read exceeded max continuation rounds")
}

func parseTemplateAttrResponse(data []byte) (msgGetTemplateAttrListResponse, error) {
	data2 := bytes.NewBuffer(data)
	result := msgGetTemplateAttrListResponse{}
	if err := binary.Read(data2, binary.LittleEndian, &result); err != nil {
		return result, fmt.Errorf("problem reading result. %w", err)
	}

	switch result.Status {
	case CIPStatus_OK:
		return result, nil
	case CIPStatus_PartialTransfer:
		return result, fmt.Errorf("template attribute read returned unexpected status %v; continuation is not supported for this fixed-size response", result.Status)
	default:
		return result, fmt.Errorf("template attribute read returned status %v", result.Status)
	}
}

func (client *Client) requestMemberReadChunk(str_instance uint32, start_offset uint32, read_length uint16) (msgMemberInfoHdr, []byte, error) {
	reqitems := make([]CIPItem, 2)
	reqitems[0] = newItem(cipItem_ConnectionAddress, &client.OTNetworkConnectionID)

	p, err := Serialize(
		CipObject_Template, CIPInstance(str_instance),
	)
	if err != nil {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("couldn't build path. %w", err)
	}

	readmsg := msgCIPConnectedServiceReq{
		SequenceCount: uint16(sequencer()),
		Service:       CIPService_Read,
		PathLength:    byte(p.Len() / 2),
	}

	reqitems[1] = newItem(cipItem_ConnectedData, readmsg)
	err = reqitems[1].Serialize(p.Bytes())
	if err != nil {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("problem serializing path: %w", err)
	}
	err = reqitems[1].Serialize(start_offset)
	if err != nil {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("problem serializing item start offset: %w", err)
	}
	err = reqitems[1].Serialize(read_length)
	if err != nil {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("problem serializing item read length: %w", err)
	}

	itemdata, err := serializeItems(reqitems)
	if err != nil {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("problem serializing item data: %w", err)
	}
	hdr, data, err := client.send_recv_data(cipCommandSendUnitData, itemdata)
	if err != nil {
		return msgMemberInfoHdr{}, nil, err
	}
	_ = hdr
	_ = data

	padding := make([]byte, 6)
	_, err = data.Read(padding)
	if err != nil {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("couldn't read padding. %w", err)
	}

	resp_items, err := readItems(data)
	if err != nil {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("couldn't parse items. %w", err)
	}
	if len(resp_items) < 2 {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("wrong number of items for member read. expected at least 2 but got %d", len(resp_items))
	}

	data2 := bytes.NewBuffer(resp_items[1].Data)
	mihdr := msgMemberInfoHdr{}
	err = binary.Read(data2, binary.LittleEndian, &mihdr)
	if err != nil {
		return msgMemberInfoHdr{}, nil, fmt.Errorf("couldn't read member info header. %w", err)
	}

	return mihdr, append([]byte(nil), data2.Bytes()...), nil
}

// Per my testing, this only works after a certain firmware version.  I don't know which one
// exactly, but V32 it works and V20 it does not.  I suspect v24 or v28 since they were pretty substantial
// changes, but v21 could also be the version since that is the swap from rslogix to studio
func (client *Client) ListMembers(str_instance uint32) (UDTDescriptor, error) {
	client.Logger.Debug("list members", "instance", str_instance)

	d, ok := client.KnownTypesByID[str_instance]
	if ok {
		return d, nil
	}

	template_info, err := client.GetTemplateInstanceAttr(str_instance)

	if err != nil {
		return UDTDescriptor{}, fmt.Errorf("couldn't get template info. %w", err)
	}

	totalBytes := template_info.SizeWords * 4
	if totalBytes < 20 { // 20 is TCP header + CIP header size
		return UDTDescriptor{}, fmt.Errorf("template size too small for member read. instance %d sizeWords=%d", str_instance, template_info.SizeWords)
	}

	payload, err := collectMemberReadPayload(totalBytes-20, func(startOffset uint32, readLength uint16) (msgMemberInfoHdr, []byte, error) {
		return client.requestMemberReadChunk(str_instance, startOffset, readLength)
	})
	if err != nil {
		return UDTDescriptor{}, fmt.Errorf("couldn't read fragmented member payload. %w", err)
	}

	data2 := bytes.NewBuffer(payload)

	memberInfos := make([]msgMemberInfo, template_info.MemberCount)
	err = binary.Read(data2, binary.LittleEndian, &memberInfos)
	if err != nil {
		return UDTDescriptor{}, fmt.Errorf("couldn't read memberinfos. %w", err)
	}

	descriptor := UDTDescriptor{}
	descriptor.Info = template_info
	descriptor.Instance_ID = str_instance
	descriptor.Members = make([]UDTMemberDescriptor, template_info.MemberCount)

	struct_name, err := data2.ReadString(0x00)
	if err != nil {
		return UDTDescriptor{}, fmt.Errorf("couldn't read struct name. %w", err)
	}

	if strings.Contains(struct_name, ";") {
		struct_name = strings.Split(struct_name, ";")[0]
	}
	struct_name = strings.TrimRight(struct_name, "\000")
	descriptor.Name = struct_name

	for i := 0; i < int(template_info.MemberCount); i++ {

		fieldname, err := data2.ReadString(0x00)
		if err != nil && fieldname == "" {
			if errors.Is(err, io.EOF) {
				continue
			}
			return UDTDescriptor{}, fmt.Errorf("couldn't read field name. %w", err)
		}
		fieldname = strings.TrimRight(fieldname, "\000")

		descriptor.Members[i].Name = fieldname
		descriptor.Members[i].Info = memberInfos[i]
		id := descriptor.Members[i].Template_ID()
		if id != 0 {
			// this is a UDT
			d2, err := client.ListMembers(uint32(descriptor.Members[i].Template_ID()))
			if err == nil {
				descriptor.Members[i].UDT = &d2
			} else {
				client.Logger.Debug("couldn't get udt", "name", fieldname, "type", descriptor.Members[i].Info.Type)
			}
		}
	}

	client.KnownTypesByID[str_instance] = descriptor
	return descriptor, nil
}

// full descriptor of a struct in the controller.
// could be a UDT or a builtin struct like a TON
type UDTDescriptor struct {
	Instance_ID uint32
	Name        string
	Info        msgGetTemplateAttrListResponse
	Members     []UDTMemberDescriptor
}

// This function is experimental and not accurate.  I suspect it is accurate only if the last field in the
// udt is a simple atomic type (int, real, dint, etc...).  Use at your own risk.
func (u UDTDescriptor) Size() int {
	maxsize := uint32(0)
	for i := range u.Members {
		m := u.Members[i]
		end := m.Info.Offset + uint32(m.Info.CIPType().Size())
		if end > maxsize {
			maxsize = end
		}
	}
	return int(maxsize)
}

type UDTMemberDescriptor struct {
	Name string
	Info msgMemberInfo
	UDT  *UDTDescriptor
}

func (u *UDTMemberDescriptor) Template_ID() uint16 {
	val := u.Info.Type
	template_mask := uint16(0b0000_1111_1111_1111) // spec says first 11 bits, but built-in types use 12th.
	bit12 := uint16(1 << 12)
	bit15 := uint16(1 << 15)
	b12_set := val&bit12 != 0
	b15_set := val&bit15 != 0
	if !b15_set || b12_set {
		// not a template
		return 0
	}

	return val & template_mask
}
