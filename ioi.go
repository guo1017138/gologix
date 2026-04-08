package gologix

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type tagPartDescriptor struct {
	FullPath    string
	BasePath    string
	Array_Order []int
	BitNumber   int
	BitAccess   bool
}

var bit_access_regex, _ = regexp.Compile(`\.\d+$`)
var array_access_regex, _ = regexp.Compile(`\[([\d]|[,]|[\s])*\]$`)

func (tag *tagPartDescriptor) Parse(tagpath string) error {
	var err error
	tag.FullPath = tagpath
	tag.BasePath = tagpath

	// Check if tag is accessing a bit in the data
	bitpos := bit_access_regex.FindStringIndex(tagpath)
	if bitpos == nil {
		tag.BitAccess = false
	} else {
		tag.BitAccess = true
		bit_access_text := tagpath[bitpos[0]+1 : bitpos[1]]
		tag.BasePath = strings.ReplaceAll(tag.BasePath, bit_access_text, "")
		tag.BitNumber, err = strconv.Atoi(bit_access_text)
		if err != nil {
			return fmt.Errorf("could't parse %v to a bit portion of tag. %w", bit_access_text, err)
		}
	}

	// check if tag is accessing an array
	arrpos := array_access_regex.FindStringIndex(tagpath)
	if arrpos == nil {
		tag.Array_Order = nil
	} else {
		arr_access_text := tagpath[arrpos[0]+1 : arrpos[1]-1]
		tag.BasePath = strings.ReplaceAll(tag.BasePath, arr_access_text, "")
		if strings.Contains(arr_access_text, ",") {
			parts := strings.Split(arr_access_text, ",")
			tag.Array_Order = make([]int, len(parts))
			for i, part := range parts {
				tag.Array_Order[i], err = strconv.Atoi(part)
				if err != nil {
					return fmt.Errorf("could't parse %v to an array position. %w", arr_access_text, err)
				}
			}

		} else {
			tag.Array_Order = make([]int, 1)
			tag.Array_Order[0], err = strconv.Atoi(arr_access_text)
			if err != nil {
				return fmt.Errorf("could't parse %v to an array position. %w", arr_access_text, err)
			}
		}
	}

	return nil

}

// parse the tag name into its base tag (remove array index or bit) and get the array index if it exists
func parse_tag_name(tagpath string) (tag tagPartDescriptor, err error) {
	err = tag.Parse(tagpath)
	return

}

// Internal Object Identifier. Used to specify a tag name in the controller
// the Buffer has the CIP route for a tag path.
type tagIOI struct {
	Path        string
	Type        CIPType
	BitAccess   bool
	BitPosition int
	Buffer      []byte
}

func (ioi *tagIOI) Write(p []byte) (n int, err error) {
	ioi.Buffer = append(ioi.Buffer, p...)
	return len(p), nil
}

func (ioi *tagIOI) Bytes() []byte {
	return ioi.Buffer
}
func (ioi *tagIOI) Len() int {
	return len(ioi.Buffer)
}

// this is the default buffer size for tag IOI generation.
const defaultIOIBufferSize = 256

// The IOI is the tag name structure that CIP requires. It's parsed into symbolic or
// instance-based path segments, whichever is shorter for the requested tag.
func (client *Client) newIOI(tagpath string, datatype CIPType) (ioi *tagIOI, err error) {
	client.ioi_cache_lock.Lock()
	defer client.ioi_cache_lock.Unlock()

	if client.ioi_cache == nil {
		client.ioi_cache = make(map[string]*tagIOI)
	}

	// CIP doesn't care about case. But we'll make it lowercase to match the encodings
	// shown in 1756-PM020H-EN-P.
	tagpath = strings.ToLower(tagpath)

	cached, exists := client.ioi_cache[tagpath]
	if client.firmware() > 20 {
		knownIOI, ok, err := client.newKnownTagIOI(tagpath, datatype)
		if err != nil {
			return nil, err
		}
		if ok {
			if !exists {
				cached, err = client.newSymbolicIOI(tagpath, datatype)
				if err != nil {
					return nil, err
				}
			}
			if len(knownIOI.Buffer) < len(cached.Buffer) {
				client.ioi_cache[tagpath] = knownIOI
				return knownIOI, nil
			}
		}
	}

	if exists {
		return cached, nil
	}

	ioi, err = client.newSymbolicIOI(tagpath, datatype)
	if err != nil {
		return nil, err
	}
	client.ioi_cache[tagpath] = ioi
	return ioi, nil
}

func (client *Client) newSymbolicIOI(tagpath string, datatype CIPType) (*tagIOI, error) {
	ioi := &tagIOI{
		Path:   tagpath,
		Type:   datatype,
		Buffer: make([]byte, 0, defaultIOIBufferSize),
	}

	for _, tagPart := range strings.Split(tagpath, ".") {
		if err := appendIOIPart(ioi, tagPart); err != nil {
			return nil, err
		}
	}
	return ioi, nil
}

func appendIOIPart(ioi *tagIOI, tagPart string) error {
	if tagPart == "" {
		return nil
	}

	if strings.HasSuffix(tagPart, "]") {
		startIndex := strings.Index(tagPart, "[")
		if startIndex < 0 {
			startIndex = len(tagPart)
		}
		if startIndex > 0 {
			ioiPart, err := marshalIOIPart(tagPart[:startIndex])
			if err != nil {
				return err
			}
			if _, err := ioi.Write(ioiPart); err != nil {
				return fmt.Errorf("problem writing ioi part %w", err)
			}
		}

		t, err := parse_tag_name(tagPart)
		if err != nil {
			return fmt.Errorf("problem parsing path %q: %w", tagPart, err)
		}
		for _, orderSize := range t.Array_Order {
			if orderSize < 256 {
				indexPart := []byte{byte(cipElement_8bit), byte(orderSize)}
				if err := binary.Write(ioi, binary.LittleEndian, indexPart); err != nil {
					return fmt.Errorf("problem reading index part. %w", err)
				}
			} else if orderSize < 65536 {
				indexPart := []uint16{uint16(cipElement_16bit), uint16(orderSize)}
				if err := binary.Write(ioi, binary.LittleEndian, indexPart); err != nil {
					return fmt.Errorf("problem reading index part. %w", err)
				}
			} else {
				if err := binary.Write(ioi, binary.LittleEndian, []uint16{uint16(cipElement_32bit)}); err != nil {
					return err
				}
				if err := binary.Write(ioi, binary.LittleEndian, []uint32{uint32(orderSize)}); err != nil {
					return err
				}
			}
		}
		return nil
	}

	bitAccess, err := strconv.Atoi(tagPart)
	if err == nil && bitAccess <= 31 {
		ioi.BitAccess = true
		ioi.BitPosition = bitAccess
		return nil
	}

	ioiPart, err := marshalIOIPart(tagPart)
	if err != nil {
		return err
	}
	_, err = ioi.Write(ioiPart)
	return err
}

func (client *Client) newKnownTagIOI(tagpath string, datatype CIPType) (*tagIOI, bool, error) {
	if client.KnownTags == nil {
		return nil, false, nil
	}

	knownTag, remainder, ok, err := client.findKnownTagPrefix(tagpath)
	if err != nil || !ok {
		return nil, false, err
	}

	if knownTag.Info.Type == CIPTypeStruct && len(knownTag.Array_Order) > 0 {
		return nil, false, nil
	}

	prefix := knownTag.Bytes()
	ioi := &tagIOI{
		Path:   tagpath,
		Type:   datatype,
		Buffer: make([]byte, 0, len(prefix)+defaultIOIBufferSize),
	}
	ioi.Buffer = append(ioi.Buffer, prefix...)

	remainder = strings.TrimPrefix(remainder, ".")
	if remainder == "" {
		return ioi, true, nil
	}
	for _, tagPart := range strings.Split(remainder, ".") {
		if err := appendIOIPart(ioi, tagPart); err != nil {
			return nil, false, err
		}
	}
	return ioi, true, nil
}

func (client *Client) findKnownTagPrefix(tagpath string) (KnownTag, string, bool, error) {
	rawParts := strings.Split(tagpath, ".")
	for end := len(rawParts); end > 0; end-- {
		candidateParts := make([]string, 0, end)
		for _, part := range rawParts[:end] {
			bitAccess, err := strconv.Atoi(part)
			if err == nil && bitAccess <= 31 {
				continue
			}
			parsed, err := parse_tag_name(part)
			if err != nil {
				return KnownTag{}, "", false, err
			}
			if parsed.BasePath != "" {
				candidateParts = append(candidateParts, parsed.BasePath)
			}
		}
		if len(candidateParts) == 0 {
			continue
		}
		candidate := strings.Join(candidateParts, ".")
		if knownTag, ok := client.KnownTags[candidate]; ok {
			remainder := strings.TrimPrefix(tagpath, candidate)
			return knownTag, remainder, true, nil
		}
	}
	return KnownTag{}, "", false, nil
}

func marshalIOIPart(tagpath string) ([]byte, error) {
	t, err := parse_tag_name(tagpath)
	if err != nil {
		return nil, fmt.Errorf("could not parse tag path: %w", err)
	}
	tag_size := len(t.BasePath)
	need_extend := false
	if tag_size%2 == 1 {
		need_extend = true
		//tag_size += 1
	}

	tag_name_header := [2]byte{byte(segmentTypeExtendedSymbolic), byte(tag_size)}
	tag_name_msg := append(tag_name_header[:], []byte(t.BasePath)...)
	// has to be an even number of bytes.
	if need_extend {
		tag_name_msg = append(tag_name_msg, []byte{0x00}...)
	}
	return tag_name_msg, nil
}

// these next functions are for reversing the bytes back to a tag string
func getAsciiTagPart(item *CIPItem) (string, error) {
	var tag_len byte
	err := item.DeSerialize(&tag_len)
	if err != nil {
		return "", fmt.Errorf("problem getting tag len. %w", err)
	}
	b := make([]byte, tag_len)
	err = item.DeSerialize(&b)
	if err != nil {
		return "", fmt.Errorf("problem reading tag path. %w", err)
	}
	if tag_len%2 == 1 {
		var pad byte
		err = item.DeSerialize(&pad)
		if err != nil {
			return "", fmt.Errorf("problem reading pad byte. %w", err)
		}
	}

	tag_str := string(b)
	return tag_str, nil
}
func getTagFromPath(item *CIPItem) (string, error) {

	tag_str := ""

morepath:
	for {
		// we haven't read all the tag path info.
		var tag_path_type byte
		err := item.DeSerialize(&tag_path_type)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return tag_str, nil

			}
			return "", fmt.Errorf("couldn't get path part type: %w", err)
		}
		switch tag_path_type {
		case 0x28:
			// one byte index
			var array_index byte
			err = item.DeSerialize(&array_index)
			if err != nil {
				return "", fmt.Errorf("couldn't get array index: %w", err)
			}
			if tag_str[len(tag_str)-1] == ']' {
				tag_str = fmt.Sprintf("%s,%d]", tag_str[:len(tag_str)-1], array_index)
			} else {
				tag_str = fmt.Sprintf("%s[%d]", tag_str, array_index)
			}
		case 0x29:
			// two byte index
			var pad byte
			err = item.DeSerialize(&pad)
			if err != nil {
				return "", fmt.Errorf("couldn't get padding: %w", err)
			}
			var array_index uint16
			err = item.DeSerialize(&array_index)
			if err != nil {
				return "", fmt.Errorf("couldn't get array index: %w", err)
			}
			if tag_str[len(tag_str)-1] == ']' {
				tag_str = fmt.Sprintf("%s,%d]", tag_str[:len(tag_str)-1], array_index)
			} else {
				tag_str = fmt.Sprintf("%s[%d]", tag_str, array_index)
			}
		case 0x91:
			// ascii portion of tag path
			s, err := getAsciiTagPart(item)
			if err != nil {
				return "", fmt.Errorf("problem in ascii tag part: %w", err)
			}
			if tag_str == "" {
				tag_str = s
			} else {
				tag_str = fmt.Sprintf("%s.%s", tag_str, s)
			}
		default:
			// this byte does not indicate the tag path is continuing.  go back by one in the item's data buffer to "unread" it.
			item.Pos--
			break morepath
		}

	}

	return tag_str, nil

}
