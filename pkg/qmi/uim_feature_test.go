package qmi

import (
	"context"
	"encoding/binary"
	"testing"
)

func TestParseGetSlotStatusResponse(t *testing.T) {
	statusValue := []byte{
		0x02,
		0x02, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x01,
		0x13,
		'8', '9', '8', '6', '0', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '0', '1', '2', '3',
		0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00,
		0x00,
	}
	extInfoValue := []byte{
		0x02,
		0x02, 0x00, 0x00, 0x00,
		0x02,
		0x03,
		0x3B, 0x9F, 0x95,
		0x01,
		0x00, 0x00, 0x00, 0x00,
		0x00,
		0x00,
		0x00,
	}
	eidValue := []byte{
		0x02,
		0x04, 0x89, 0x10, 0x00, 0x01,
		0x00,
	}

	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x10, Value: statusValue},
			{Type: 0x11, Value: extInfoValue},
			{Type: 0x12, Value: eidValue},
		},
	}

	info, err := parseGetSlotStatusResponse(resp)
	if err != nil {
		t.Fatalf("parseGetSlotStatusResponse returned error: %v", err)
	}
	if len(info.Slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(info.Slots))
	}
	if info.Slots[0].PhysicalCardStatus != UIMPhysicalCardStatePresent || info.Slots[0].PhysicalSlotStatus != UIMSlotStateActive {
		t.Fatalf("unexpected first slot state: %+v", info.Slots[0])
	}
	if info.Slots[0].ICCID != "8986001234567890123" {
		t.Fatalf("unexpected first slot ICCID: %+v", info.Slots[0])
	}
	if !info.Slots[0].HasExtendedInfo || info.Slots[0].CardProtocol != UIMCardProtocolUICC || !info.Slots[0].IsEUICC {
		t.Fatalf("unexpected first slot extended info: %+v", info.Slots[0])
	}
	if !info.Slots[0].HasEID || len(info.Slots[0].EID) != 4 {
		t.Fatalf("unexpected first slot EID: %+v", info.Slots[0])
	}
	if info.Slots[1].PhysicalCardStatus != UIMPhysicalCardStateAbsent || info.Slots[1].PhysicalSlotStatus != UIMSlotStateInactive {
		t.Fatalf("unexpected second slot state: %+v", info.Slots[1])
	}
}

func TestParseReadRecordResponse(t *testing.T) {
	readValue := []byte{0x03, 0x00, 0xDE, 0xAD, 0xBE}
	additionalValue := []byte{0x02, 0x00, 0xFA, 0xCE}
	token := make([]byte, 4)
	binary.LittleEndian.PutUint32(token, 0x10203040)
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x10, Value: []byte{0x90, 0x00}},
			{Type: 0x11, Value: readValue},
			{Type: 0x12, Value: additionalValue},
			{Type: 0x13, Value: token},
		},
	}

	info, err := parseReadRecordResponse(resp)
	if err != nil {
		t.Fatalf("parseReadRecordResponse returned error: %v", err)
	}
	if !info.HasCardResult || info.CardResult.SW1 != 0x90 || info.CardResult.SW2 != 0x00 {
		t.Fatalf("unexpected card result: %+v", info)
	}
	if len(info.Data) != 3 || info.Data[2] != 0xBE {
		t.Fatalf("unexpected read data: %+v", info.Data)
	}
	if len(info.AdditionalData) != 2 || info.AdditionalData[1] != 0xCE {
		t.Fatalf("unexpected additional data: %+v", info.AdditionalData)
	}
	if !info.HasResponseInIndicationToken || info.ResponseInIndicationToken != 0x10203040 {
		t.Fatalf("unexpected token: %+v", info)
	}
}

func TestParseGetFileAttributesResponse(t *testing.T) {
	attrValue := make([]byte, 29)
	binary.LittleEndian.PutUint16(attrValue[0:2], 64)
	binary.LittleEndian.PutUint16(attrValue[2:4], 0x6F3A)
	attrValue[4] = UIMFileTypeLinearFixed
	binary.LittleEndian.PutUint16(attrValue[5:7], 16)
	binary.LittleEndian.PutUint16(attrValue[7:9], 4)
	attrValue[9] = 1
	binary.LittleEndian.PutUint16(attrValue[10:12], 0x1001)
	attrValue[12] = 2
	binary.LittleEndian.PutUint16(attrValue[13:15], 0x1002)
	attrValue[15] = 3
	binary.LittleEndian.PutUint16(attrValue[16:18], 0x1003)
	attrValue[18] = 4
	binary.LittleEndian.PutUint16(attrValue[19:21], 0x1004)
	attrValue[21] = 5
	binary.LittleEndian.PutUint16(attrValue[22:24], 0x1005)
	binary.LittleEndian.PutUint16(attrValue[24:26], 3)
	copy(attrValue[26:29], []byte{0x62, 0x10, 0x82})

	token := make([]byte, 4)
	binary.LittleEndian.PutUint32(token, 0x0A0B0C0D)
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x10, Value: []byte{0x90, 0x00}},
			{Type: 0x11, Value: attrValue},
			{Type: 0x12, Value: token},
		},
	}

	info, err := parseGetFileAttributesResponse(resp)
	if err != nil {
		t.Fatalf("parseGetFileAttributesResponse returned error: %v", err)
	}
	if info.FileID != 0x6F3A || info.FileType != UIMFileTypeLinearFixed || info.RecordCount != 4 {
		t.Fatalf("unexpected file attributes: %+v", info)
	}
	if info.ReadSecurity.Attributes != 0x1001 || info.ActivateSecurity.Attributes != 0x1005 {
		t.Fatalf("unexpected security attributes: %+v", info)
	}
	if len(info.RawData) != 3 || info.RawData[2] != 0x82 {
		t.Fatalf("unexpected raw data: %+v", info.RawData)
	}
	if !info.HasResponseInIndicationToken || info.ResponseInIndicationToken != 0x0A0B0C0D {
		t.Fatalf("unexpected token: %+v", info)
	}
}

func TestDecodeUIMDigits(t *testing.T) {
	if got := decodeUIMDigits([]byte("898600")); got != "898600" {
		t.Fatalf("unexpected ASCII digit decode: %s", got)
	}
	if got := decodeUIMDigits([]byte{0x98, 0x10, 0x32}); got != "890123" {
		t.Fatalf("unexpected BCD digit decode: %s", got)
	}
}

func TestParseSupportedMessagesTLV(t *testing.T) {
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x10, Value: []byte{0x03, 0x00, 0x2F, 0x30, 0x47}},
		},
	}

	msgs, err := parseSupportedMessagesTLV(resp)
	if err != nil {
		t.Fatalf("parseSupportedMessagesTLV returned error: %v", err)
	}
	if len(msgs) != 3 || msgs[0] != 0x2F || msgs[2] != 0x47 {
		t.Fatalf("unexpected supported messages: %v", msgs)
	}
}

func TestBuildChangeProvisioningSessionTLVs(t *testing.T) {
	slot := uint8(2)
	tlvs := buildChangeProvisioningSessionTLVs(UIMChangeProvisioningSessionRequest{
		SessionType:           UIMSessionTypePrimaryGWProvisioning,
		Activate:              true,
		Slot:                  &slot,
		ApplicationIdentifier: []byte{0xA0, 0x00},
	})

	if len(tlvs) != 2 {
		t.Fatalf("expected 2 TLVs, got %d", len(tlvs))
	}
	if tlvs[0].Type != 0x01 || len(tlvs[0].Value) != 2 || tlvs[0].Value[0] != UIMSessionTypePrimaryGWProvisioning || tlvs[0].Value[1] != 1 {
		t.Fatalf("unexpected session change TLV: %+v", tlvs[0])
	}
	if tlvs[1].Type != 0x10 || len(tlvs[1].Value) != 4 || tlvs[1].Value[0] != 2 || tlvs[1].Value[1] != 2 {
		t.Fatalf("unexpected app info TLV header: %+v", tlvs[1])
	}
	if tlvs[1].Value[2] != 0xA0 || tlvs[1].Value[3] != 0x00 {
		t.Fatalf("unexpected app info TLV payload: %+v", tlvs[1])
	}
}

func TestBuildRefreshRegisterInfoTLV(t *testing.T) {
	tlv, err := buildRefreshRegisterInfoTLV(UIMRefreshRegisterRequest{
		RegisterFlag: true,
		VoteForInit:  true,
		Files: []UIMRefreshFile{
			{FileID: 0x6F07, Path: []uint8{0x00, 0x3F}},
		},
	})
	if err != nil {
		t.Fatalf("buildRefreshRegisterInfoTLV returned error: %v", err)
	}
	if tlv.Type != 0x02 || len(tlv.Value) != 9 {
		t.Fatalf("unexpected refresh register info TLV: %+v", tlv)
	}
	if tlv.Value[0] != 1 || tlv.Value[1] != 1 || binary.LittleEndian.Uint16(tlv.Value[2:4]) != 1 {
		t.Fatalf("unexpected refresh register flags/count: %v", tlv.Value[:4])
	}
	if binary.LittleEndian.Uint16(tlv.Value[4:6]) != 0x6F07 || tlv.Value[6] != 2 || tlv.Value[7] != 0x00 || tlv.Value[8] != 0x3F {
		t.Fatalf("unexpected refresh register file payload: %v", tlv.Value[4:])
	}
}

func TestParseUIMRefreshIndication(t *testing.T) {
	packet := &Packet{
		TLVs: []TLV{
			{
				Type: 0x10,
				Value: []byte{
					0x01, 0x02, UIMSessionTypePrimaryGWProvisioning,
					0x02, 0xA0, 0x00,
					0x01, 0x00,
					0x07, 0x6F, 0x02, 0x00, 0x3F,
				},
			},
		},
	}

	info, err := ParseUIMRefreshIndication(packet)
	if err != nil {
		t.Fatalf("ParseUIMRefreshIndication returned error: %v", err)
	}
	if info.Stage != 0x01 || info.Mode != 0x02 || info.SessionType != UIMSessionTypePrimaryGWProvisioning {
		t.Fatalf("unexpected refresh indication header: %+v", info)
	}
	if len(info.ApplicationIdentifier) != 2 || info.ApplicationIdentifier[0] != 0xA0 {
		t.Fatalf("unexpected refresh indication aid: %+v", info.ApplicationIdentifier)
	}
	if len(info.Files) != 1 || info.Files[0].FileID != 0x6F07 || len(info.Files[0].Path) != 2 || info.Files[0].Path[1] != 0x3F {
		t.Fatalf("unexpected refresh indication files: %+v", info.Files)
	}
}

func TestParseUIMSlotStatusIndication(t *testing.T) {
	packet := &Packet{
		TLVs: []TLV{
			{
				Type:  0x10,
				Value: []byte{0x00},
			},
		},
	}

	info, err := ParseUIMSlotStatusIndication(packet)
	if err != nil {
		t.Fatalf("ParseUIMSlotStatusIndication returned error: %v", err)
	}
	if info == nil || len(info.Slots) != 0 {
		t.Fatalf("unexpected slot status indication parse result: %+v", info)
	}
}

func TestUIMRegisterEventsReturnsAcceptedMask(t *testing.T) {
	c := &Client{
		eventCh:        make(chan Event, 1),
		indicationInCh: make(chan Event, 1),
		writeCh:        make(chan writeRequest, 1),
		closeCh:        make(chan struct{}),
		transactions:   make(map[uint32]chan *Packet),
		opts:           DefaultClientOptions(),
	}
	u := &UIMService{client: c, clientID: 7}

	go func() {
		wr := <-c.writeCh
		wr.result <- nil
		key := uint32(ServiceUIM)<<16 | 1
		c.mu.Lock()
		respCh := c.transactions[key]
		c.mu.Unlock()
		if respCh == nil {
			t.Errorf("response channel not found for key=%d", key)
			return
		}
		respCh <- &Packet{TLVs: []TLV{
			successResultTLV(),
			{Type: 0x10, Value: []byte{0x05, 0x00, 0x00, 0x00}},
		}}
	}()

	mask, err := u.RegisterEvents(context.Background(), UIMEventRegistrationCardStatus|UIMEventRegistrationPhysicalSlotStatus)
	if err != nil {
		t.Fatalf("RegisterEvents returned error: %v", err)
	}
	if mask != 0x00000005 {
		t.Fatalf("unexpected accepted mask: got=0x%08x", mask)
	}
}

func TestUIMRegisterEventsFallsBackToRequestedMask(t *testing.T) {
	c := &Client{
		eventCh:        make(chan Event, 1),
		indicationInCh: make(chan Event, 1),
		writeCh:        make(chan writeRequest, 1),
		closeCh:        make(chan struct{}),
		transactions:   make(map[uint32]chan *Packet),
		opts:           DefaultClientOptions(),
	}
	u := &UIMService{client: c, clientID: 8}
	requested := UIMEventRegistrationCardStatus | UIMEventRegistrationExtendedCardStatus

	go func() {
		wr := <-c.writeCh
		wr.result <- nil
		key := uint32(ServiceUIM)<<16 | 1
		c.mu.Lock()
		respCh := c.transactions[key]
		c.mu.Unlock()
		if respCh == nil {
			t.Errorf("response channel not found for key=%d", key)
			return
		}
		respCh <- &Packet{TLVs: []TLV{successResultTLV()}}
	}()

	mask, err := u.RegisterEvents(context.Background(), requested)
	if err != nil {
		t.Fatalf("RegisterEvents returned error: %v", err)
	}
	if mask != requested {
		t.Fatalf("expected requested mask fallback=0x%08x, got=0x%08x", requested, mask)
	}
}

func TestUIMRegisterEventsMapsNotSupported(t *testing.T) {
	c := &Client{
		eventCh:        make(chan Event, 1),
		indicationInCh: make(chan Event, 1),
		writeCh:        make(chan writeRequest, 1),
		closeCh:        make(chan struct{}),
		transactions:   make(map[uint32]chan *Packet),
		opts:           DefaultClientOptions(),
	}
	u := &UIMService{client: c, clientID: 9}

	go func() {
		wr := <-c.writeCh
		wr.result <- nil
		key := uint32(ServiceUIM)<<16 | 1
		c.mu.Lock()
		respCh := c.transactions[key]
		c.mu.Unlock()
		if respCh == nil {
			t.Errorf("response channel not found for key=%d", key)
			return
		}
		respCh <- &Packet{TLVs: []TLV{{Type: 0x02, Value: []byte{0x01, 0x00, 0x5E, 0x00}}}}
	}()

	_, err := u.RegisterEvents(context.Background(), UIMEventRegistrationCardStatus)
	if err == nil {
		t.Fatal("expected not supported error")
	}
	if _, ok := err.(*NotSupportedError); !ok {
		t.Fatalf("expected NotSupportedError, got %T: %v", err, err)
	}
}
