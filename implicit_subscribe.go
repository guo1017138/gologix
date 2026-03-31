package gologix

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"time"
)

type cipConnectionPointSize byte

const (
	cipConnectionPoint_8bit  cipConnectionPointSize = 0x2C
	cipConnectionPoint_16bit cipConnectionPointSize = 0x2D
	cipConnectionPoint_32bit cipConnectionPointSize = 0x2E
)

// CIPConnectionPoint represents a logical connection point segment in a CIP path.
// It is typically used in Class 1 assembly connection paths.
type CIPConnectionPoint uint32

func (p CIPConnectionPoint) Bytes() []byte {
	if p < 256 {
		return []byte{byte(cipConnectionPoint_8bit), byte(p)}
	}
	if p <= 0xFFFF {
		b := make([]byte, 4)
		b[0] = byte(cipConnectionPoint_16bit)
		b[2] = byte(p)
		b[3] = byte(p >> 8)
		return b
	}
	b := make([]byte, 6)
	b[0] = byte(cipConnectionPoint_32bit)
	b[2] = byte(p)
	b[3] = byte(p >> 8)
	b[4] = byte(p >> 16)
	b[5] = byte(p >> 24)
	return b
}

func (p CIPConnectionPoint) Len() int {
	if p < 256 {
		return 2
	}
	if p <= 0xFFFF {
		return 4
	}
	return 6
}

// ImplicitAssemblyPathConfig describes a common Class 1 assembly connection path.
//
// Path layout:
//
//	Class(Assembly) -> Instance(config, optional) -> ConnectionPoint(O->T) -> ConnectionPoint(T->O)
//
// Example (small IDs): 20 04 24 96 2C 96 2C 64
type ImplicitAssemblyPathConfig struct {
	// Optional override, defaults to CipObject_Assembly.
	AssemblyClass CIPClass

	// Optional config assembly instance (0 means omitted).
	ConfigInstance CIPInstance

	// OutputPoint is the O->T connection point.
	OutputPoint CIPConnectionPoint

	// InputPoint is the T->O connection point.
	InputPoint CIPConnectionPoint
}

// ImplicitProducedConsumedPathConfig describes a Class 1 Produced/Consumed tag
// style connection path.
//
// Path layout used here:
//
//	[optional keying words] -> Class(IOClass=0x69) -> Instance -> SymbolicTag(0x91)
//	-> ConnectionPoint -> [optional trailer bytes]
//
// Notes:
// - Some devices add trailing simple-data bytes after connection point.
// - TrailerBytes lets callers supply those bytes verbatim when needed.
type ImplicitProducedConsumedPathConfig struct {
	ConsumedTag string

	// Optional override, defaults to CipObject_IOClass (0x69).
	IOClass CIPClass

	// Optional override, defaults to 1.
	IOInstance CIPInstance

	// Optional override, defaults to 1.
	ConnectionPoint CIPConnectionPoint

	// Include keying words prefix. Defaults to true.
	IncludeKeying bool

	// Number of 16-bit keying words. Defaults to 4 when IncludeKeying=true.
	KeyingWords int

	// Optional raw bytes appended after connection point.
	TrailerBytes []byte
}

// Common Rockwell Generic Ethernet Module defaults used by many projects.
const (
	ABGenericEthernetModuleOutputInstanceDefault uint32 = 150
	ABGenericEthernetModuleInputInstanceDefault  uint32 = 100
	ABGenericEthernetModuleConfigInstanceDefault uint32 = 151
)

// NewABGenericEthernetModuleAssemblyPath returns a commonly used Class 1
// assembly path config for Rockwell Generic Ethernet Module style mappings.
//
// Parameter semantics follow PLC-side module configuration naming:
//   - outputAssemblyInstance: PLC O->T (controller output to adapter)
//   - inputAssemblyInstance:  PLC T->O (adapter input to controller)
//   - configAssemblyInstance: optional config assembly (0 = omitted)
func NewABGenericEthernetModuleAssemblyPath(outputAssemblyInstance, inputAssemblyInstance, configAssemblyInstance uint32) ImplicitAssemblyPathConfig {
	cfg := ImplicitAssemblyPathConfig{
		AssemblyClass: CipObject_Assembly,
		OutputPoint:   CIPConnectionPoint(outputAssemblyInstance),
		InputPoint:    CIPConnectionPoint(inputAssemblyInstance),
	}
	if configAssemblyInstance != 0 {
		cfg.ConfigInstance = CIPInstance(configAssemblyInstance)
	}
	return cfg
}

// NewABGenericEthernetModuleAssemblyPathPtr is a pointer-returning variant for
// direct use in ImplicitSubscriptionConfig.AssemblyPath.
func NewABGenericEthernetModuleAssemblyPathPtr(outputAssemblyInstance, inputAssemblyInstance, configAssemblyInstance uint32) *ImplicitAssemblyPathConfig {
	cfg := NewABGenericEthernetModuleAssemblyPath(outputAssemblyInstance, inputAssemblyInstance, configAssemblyInstance)
	return &cfg
}

// NewABGenericEthernetModuleAssemblyPathDefault returns the common
// 150/100/151 output/input/config instance mapping.
func NewABGenericEthernetModuleAssemblyPathDefault() *ImplicitAssemblyPathConfig {
	return NewABGenericEthernetModuleAssemblyPathPtr(
		ABGenericEthernetModuleOutputInstanceDefault,
		ABGenericEthernetModuleInputInstanceDefault,
		ABGenericEthernetModuleConfigInstanceDefault,
	)
}

// NewProducedConsumedPathDefault returns a baseline Produced/Consumed path
// configuration with common defaults for IO class/instance/connection point.
func NewProducedConsumedPathDefault(consumedTag string) *ImplicitProducedConsumedPathConfig {
	return &ImplicitProducedConsumedPathConfig{
		ConsumedTag:     consumedTag,
		IOClass:         CipObject_IOClass,
		IOInstance:      1,
		ConnectionPoint: 1,
		IncludeKeying:   true,
		KeyingWords:     4,
	}
}

// BuildImplicitProducedConsumedPath serializes a Produced/Consumed Tag style
// Class 1 connection path.
func BuildImplicitProducedConsumedPath(cfg ImplicitProducedConsumedPathConfig) ([]byte, error) {
	if strings.TrimSpace(cfg.ConsumedTag) == "" {
		return nil, fmt.Errorf("consumed tag must not be empty")
	}

	ioClass := cfg.IOClass
	if ioClass == 0 {
		ioClass = CipObject_IOClass
	}
	ioInstance := cfg.IOInstance
	if ioInstance == 0 {
		ioInstance = 1
	}
	connectionPoint := cfg.ConnectionPoint
	if connectionPoint == 0 {
		connectionPoint = 1
	}

	includeKeying := cfg.IncludeKeying
	keyingWords := cfg.KeyingWords
	if keyingWords == 0 {
		keyingWords = 4
	}

	b := bytes.Buffer{}
	if includeKeying {
		if keyingWords < 0 {
			return nil, fmt.Errorf("keying words must be >= 0")
		}
		b.Write(make([]byte, keyingWords*2))
	}

	b.Write(ioClass.Bytes())
	b.Write(ioInstance.Bytes())

	tagSeg, err := marshalIOIPart(cfg.ConsumedTag)
	if err != nil {
		return nil, fmt.Errorf("failed building symbolic consumed-tag segment: %w", err)
	}
	b.Write(tagSeg)
	b.Write(connectionPoint.Bytes())
	if len(cfg.TrailerBytes) > 0 {
		b.Write(cfg.TrailerBytes)
	}

	out := b.Bytes()
	if len(out)%2 == 1 {
		out = append(out, 0x00)
	}
	return out, nil
}

// BuildImplicitAssemblyPath serializes an assembly-based Class 1 connection path.
func BuildImplicitAssemblyPath(cfg ImplicitAssemblyPathConfig) ([]byte, error) {
	assemblyClass := cfg.AssemblyClass
	if assemblyClass == 0 {
		assemblyClass = CipObject_Assembly
	}
	if cfg.OutputPoint == 0 || cfg.InputPoint == 0 {
		return nil, fmt.Errorf("output and input connection points must be non-zero")
	}

	parts := make([]any, 0, 4)
	parts = append(parts, assemblyClass)
	if cfg.ConfigInstance != 0 {
		parts = append(parts, cfg.ConfigInstance)
	}
	parts = append(parts, cfg.OutputPoint, cfg.InputPoint)

	b, err := Serialize(parts...)
	if err != nil {
		return nil, fmt.Errorf("failed building implicit assembly path: %w", err)
	}

	path := b.Bytes()
	if len(path)%2 == 1 {
		path = append(path, 0x00)
	}
	return path, nil
}

// ImplicitSubscriptionConfig configures a Class 1 implicit I/O subscription.
//
// This implementation is intentionally minimal and targets cyclic UDP I/O:
//  1. Open a dedicated Class 1 forward open connection
//  2. Listen on UDP (default :2222)
//  3. Filter by the subscription's TO connection ID
//  4. Decode payload into T and push to the Values channel
type ImplicitSubscriptionConfig struct {
	// Optional connection path override used in the Forward Open connection path.
	// If nil, client.Controller.Path.Bytes() is used.
	ConnectionPath []byte

	// Optional high-level assembly path config for real PLC/module Class1 connections.
	// Used only when ConnectionPath is empty.
	AssemblyPath *ImplicitAssemblyPathConfig

	// Optional Produced/Consumed path config.
	// Used only when ConnectionPath is empty and takes precedence over AssemblyPath.
	ProducedConsumedPath *ImplicitProducedConsumedPathConfig

	// RPI for this implicit connection. If 0, the client's RPI is used.
	RPI time.Duration

	// TransportTrigger byte for Forward Open.
	//
	// Defaults to 0x01 for compatibility with this repository's local Class1 server example.
	// Some real devices may require a different value (for example 0xA3).
	TransportTrigger byte

	// Local UDP listen address. Defaults to ":2222".
	ListenAddress string

	// Socket timeout for UDP receive loop. Defaults to 3 seconds.
	ReceiveTimeout time.Duration

	// Optional output channel depth for decoded values. Defaults to 32.
	ValueBuffer int

	// Optional error channel depth. Defaults to 8.
	ErrorBuffer int
}

// ImplicitSubscription represents one active Class 1 subscription.
//
// Values is closed when Stop() is called or when the receive loop exits.
// Errors is best-effort and also closed on stop.
type ImplicitSubscription[T any] struct {
	Values <-chan T
	Errors <-chan error

	values chan T
	errs   chan error

	client          *Client
	udpConn         net.PacketConn
	stopCh          chan struct{}
	doneCh          chan struct{}
	forwardCloseErr chan error

	connectionSerial uint16
	vendorID         uint16
	originatorSerial uint32
	toConnectionID   uint32
	closed           bool
}

// SubscribeImplicit starts a typed Class 1 implicit subscription.
//
// The decode behavior is:
//   - if T is []byte, raw payload bytes are emitted directly
//   - otherwise payload is decoded with Unpack into T
func SubscribeImplicit[T any](client *Client, cfg ImplicitSubscriptionConfig) (*ImplicitSubscription[T], error) {
	if err := client.checkConnection(); err != nil {
		return nil, fmt.Errorf("could not start implicit subscription: %w", err)
	}

	if cfg.RPI <= 0 {
		cfg.RPI = client.RPI
		if cfg.RPI <= 0 {
			cfg.RPI = rpiDefault
		}
	}
	if cfg.TransportTrigger == 0 {
		cfg.TransportTrigger = 0x01
	}
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = ":2222"
	}
	if cfg.ReceiveTimeout <= 0 {
		cfg.ReceiveTimeout = 3 * time.Second
	}
	if cfg.ValueBuffer <= 0 {
		cfg.ValueBuffer = 32
	}
	if cfg.ErrorBuffer <= 0 {
		cfg.ErrorBuffer = 8
	}

	connPath := cfg.ConnectionPath
	if len(connPath) == 0 {
		if cfg.ProducedConsumedPath != nil {
			pcPath, err := BuildImplicitProducedConsumedPath(*cfg.ProducedConsumedPath)
			if err != nil {
				return nil, err
			}
			connPath = pcPath
		} else if cfg.AssemblyPath != nil {
			assemblyPath, err := BuildImplicitAssemblyPath(*cfg.AssemblyPath)
			if err != nil {
				return nil, err
			}
			connPath = assemblyPath
		} else {
			if client.Controller.Path == nil {
				return nil, fmt.Errorf("connection path is empty and controller path is not configured")
			}
			connPath = client.Controller.Path.Bytes()
		}
	}

	serial := uint16(client.sequenceNumber.Add(1))
	otID := client.sequenceNumber.Add(1)
	toID := client.sequenceNumber.Add(1)

	item, err := client.newImplicitForwardOpenItem(connPath, serial, otID, toID, cfg.RPI, cfg.TransportTrigger)
	if err != nil {
		return nil, err
	}

	openReply, err := client.forwardOpenCustom(item)
	if err != nil {
		return nil, err
	}

	client.Logger.Debug("implicit forward-open response",
		"OtNetworkConnectionId", openReply.OtNetworkConnectionId,
		"TOConnectionId", openReply.TOConnectionId,
		"OriginatorVendorId", openReply.OriginatorVendorId,
		"OriginatorSerialNumber", openReply.OriginatorSerialNumber,
		"OTApiNs", openReply.OTApiNs,
		"TOApiNs", openReply.TOApiNs,
		"ApplicationReply", openReply.ApplicationReply,
	)

	udpConn, err := net.ListenPacket("udp", cfg.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("unable to listen for implicit UDP on %s: %w", cfg.ListenAddress, err)
	}

	values := make(chan T, cfg.ValueBuffer)
	errs := make(chan error, cfg.ErrorBuffer)

	sub := &ImplicitSubscription[T]{
		Values: values,
		Errors: errs,

		values: values,
		errs:   errs,

		client:          client,
		udpConn:         udpConn,
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
		forwardCloseErr: make(chan error, 1),

		connectionSerial: serial,
		vendorID:         client.VendorId,
		originatorSerial: client.SerialNumber,
		toConnectionID:   openReply.TOConnectionId,
	}

	go sub.run(cfg.ReceiveTimeout)
	return sub, nil
}

// SubscribeImplicitBytes starts an implicit subscription that yields raw payload bytes.
func (client *Client) SubscribeImplicitBytes(cfg ImplicitSubscriptionConfig) (*ImplicitSubscription[[]byte], error) {
	return SubscribeImplicit[[]byte](client, cfg)
}

func (client *Client) newImplicitForwardOpenItem(connPath []byte, serial uint16, otID, toID uint32, rpi time.Duration, transportTrigger byte) (CIPItem, error) {
	item := CIPItem{Header: cipItemHeader{ID: cipItem_UnconnectedData}}

	if len(connPath)%2 == 1 {
		connPath = append(connPath, 0x00)
	}

	const (
		redundantOwner     uint16 = 0
		connectionType     uint16 = 2
		priority           uint16 = 0
		connectionSizeType uint16 = 1
	)

	// Use a conservative payload size (511) for broad compatibility.
	const implicitConnSize uint16 = connSizeStandardMax
	connParams := uint16(
		redundantOwner<<15 |
			connectionType<<13 |
			priority<<10 |
			connectionSizeType<<9 |
			implicitConnSize,
	)

	var msg cipForwardOpen[uint16]
	msg.Service = CIPService_ForwardOpen
	msg.PathSize = 0x02
	msg.ClassType = cipClass_8bit
	msg.Class = byte(CipObject_ConnectionManager)
	msg.InstanceType = cipInstance_8bit
	msg.Instance = 0x01
	msg.Priority = 0x07
	msg.TimeoutTicks = 0xE9
	msg.OTConnectionID = otID
	msg.TOConnectionID = toID
	msg.ConnectionSerialNumber = serial
	msg.VendorID = client.VendorId
	msg.OriginatorSerialNumber = client.SerialNumber
	msg.Multiplier = 0x00
	msg.OtRpi = uint32(rpi / time.Microsecond)
	msg.OTNetworkConnParams = connParams
	msg.ToRpi = uint32(rpi / time.Microsecond)
	msg.TONetworkConnParams = connParams

	msg.TransportTrigger = transportTrigger
	msg.ConnPathSize = byte(len(connPath) / 2)

	if err := item.Serialize(msg); err != nil {
		return item, fmt.Errorf("error serializing implicit forward open message: %w", err)
	}
	if err := item.Serialize(connPath); err != nil {
		return item, fmt.Errorf("error serializing implicit forward open connection path: %w", err)
	}
	return item, nil
}

func (client *Client) forwardOpenCustom(forwardOpenMsg CIPItem) (msgCipForwardOpenReply, error) {
	var openReply msgCipForwardOpenReply

	reqItems := make([]CIPItem, 2)
	reqItems[0] = CIPItem{Header: cipItemHeader{ID: cipItem_Null}}
	reqItems[1] = forwardOpenMsg
	itemData, err := serializeItems(reqItems)
	if err != nil {
		return openReply, fmt.Errorf("error serializing forward-open items: %w", err)
	}

	header, data, err := client.send_recv_data(cipCommandSendRRData, itemData)
	if err != nil {
		return openReply, fmt.Errorf("error sending implicit forward-open request: %w", err)
	}

	items, err := client.parseResponse(&header, data)
	if err != nil {
		return openReply, fmt.Errorf("error parsing implicit forward-open response: %w", err)
	}

	if err := items[1].DeSerialize(&openReply); err != nil {
		return openReply, fmt.Errorf("error deserializing implicit forward-open response: %w", err)
	}

	return openReply, nil
}

func (sub *ImplicitSubscription[T]) run(receiveTimeout time.Duration) {
	defer close(sub.doneCh)
	defer close(sub.values)
	defer close(sub.errs)

	buf := make([]byte, 8192)
	for {
		select {
		case <-sub.stopCh:
			return
		default:
		}

		_ = sub.udpConn.SetReadDeadline(time.Now().Add(receiveTimeout))
		n, _, err := sub.udpConn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			select {
			case <-sub.stopCh:
				return
			default:
				sub.emitErr(fmt.Errorf("implicit UDP receive failed: %w", err))
				continue
			}
		}
		if n == 0 {
			continue
		}

		items, err := readItems(bytes.NewBuffer(buf[:n]))
		if err != nil {
			sub.emitErr(fmt.Errorf("implicit packet decode failed: %w", err))
			continue
		}
		if len(items) != 2 {
			continue
		}

		if items[0].Header.ID != cipItem_SequenceAddress || items[1].Header.ID != cipItem_ConnectedData {
			continue
		}

		var seqInfo cipIOSeqAccessData
		if err := items[0].DeSerialize(&seqInfo); err != nil {
			sub.emitErr(fmt.Errorf("implicit sequence address decode failed: %w", err))
			continue
		}
		if seqInfo.ConnectionID != sub.toConnectionID {
			continue
		}

		// Strip sequence from connected data item payload.
		_, err = items[1].Uint16()
		if err != nil {
			sub.emitErr(fmt.Errorf("implicit connected-data sequence decode failed: %w", err))
			continue
		}
		payload := items[1].Rest()

		value, err := decodeImplicitPayload[T](payload)
		if err != nil {
			sub.emitErr(fmt.Errorf("implicit payload unpack failed: %w", err))
			continue
		}

		select {
		case sub.values <- value:
		default:
			sub.emitErr(fmt.Errorf("implicit values channel full; dropping packet"))
		}
	}
}

func decodeImplicitPayload[T any](payload []byte) (T, error) {
	var out T

	if dst, ok := any(&out).(*[]byte); ok {
		*dst = append((*dst)[:0], payload...)
		return out, nil
	}

	b := bytes.NewBuffer(payload)
	_, err := Unpack(b, &out)
	if err != nil {
		return out, err
	}
	return out, nil
}

func (sub *ImplicitSubscription[T]) emitErr(err error) {
	select {
	case sub.errs <- err:
	default:
	}
}

// Stop terminates the receive loop, closes UDP listener resources,
// and attempts a forward-close for the Class 1 connection.
func (sub *ImplicitSubscription[T]) Stop() error {
	if sub.closed {
		return nil
	}
	sub.closed = true

	close(sub.stopCh)
	if sub.udpConn != nil {
		_ = sub.udpConn.Close()
	}

	if sub.client != nil && sub.client.Connected() {
		err := sub.client.forwardCloseBySerial(sub.connectionSerial, sub.vendorID, sub.originatorSerial)
		if err != nil {
			sub.forwardCloseErr <- err
		}
	}

	<-sub.doneCh
	close(sub.forwardCloseErr)

	for err := range sub.forwardCloseErr {
		if err != nil {
			return err
		}
	}
	return nil
}

func (client *Client) forwardCloseBySerial(serial, vendorID uint16, originatorSerial uint32) error {
	items := make([]CIPItem, 2)
	items[0] = CIPItem{}
	items[1] = CIPItem{Header: cipItemHeader{ID: cipItem_UnconnectedData}}

	path, err := Serialize(
		client.Controller.Path,
		CipObject_MessageRouter,
		CIPInstance(1),
	)
	if err != nil {
		return fmt.Errorf("error serializing forward-close path: %w", err)
	}

	msg := msgCipUnRegister{
		Service:                CIPService_ForwardClose,
		CipPathSize:            0x02,
		ClassType:              cipClass_8bit,
		Class:                  0x06,
		InstanceType:           cipInstance_8bit,
		Instance:               0x01,
		Priority:               0x0A,
		TimeoutTicks:           0x0E,
		ConnectionSerialNumber: serial,
		VendorID:               vendorID,
		OriginatorSerialNumber: originatorSerial,
		PathSize:               byte(path.Len() / 2),
		Reserved:               0x00,
	}

	if err := items[1].Serialize(msg); err != nil {
		return fmt.Errorf("error serializing forward-close request: %w", err)
	}
	if err := items[1].Serialize(path); err != nil {
		return fmt.Errorf("error serializing forward-close route path: %w", err)
	}

	itemData, err := serializeItems(items)
	if err != nil {
		return fmt.Errorf("error serializing forward-close items: %w", err)
	}

	header, data, err := client.send_recv_data(cipCommandSendRRData, itemData)
	if err != nil {
		return fmt.Errorf("error sending forward-close request: %w", err)
	}

	_, err = client.parseResponse(&header, data)
	if err != nil {
		return fmt.Errorf("error parsing forward-close response: %w", err)
	}

	return nil
}
