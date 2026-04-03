package qmi

import (
	"encoding/binary"
	"errors"
	"testing"
)

func successResultTLV() TLV {
	return TLV{Type: 0x02, Value: []byte{0x00, 0x00, 0x00, 0x00}}
}

func qmiErrorResultTLV(code uint16) TLV {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint16(buf[0:2], 0x0001)
	binary.LittleEndian.PutUint16(buf[2:4], code)
	return TLV{Type: 0x02, Value: buf}
}

func TestBuildOpenLogicalChannelTLVs(t *testing.T) {
	tlvs := buildOpenLogicalChannelTLVs(1, []byte{0xA0, 0x00, 0x01})
	if len(tlvs) != 2 {
		t.Fatalf("expected 2 TLVs, got %d", len(tlvs))
	}
	if tlvs[0].Type != 0x10 || len(tlvs[0].Value) != 4 {
		t.Fatalf("unexpected AID TLV: %+v", tlvs[0])
	}
	if tlvs[0].Value[0] != 0x03 {
		t.Fatalf("expected AID length prefix 0x03, got 0x%02x", tlvs[0].Value[0])
	}
	if tlvs[1].Type != 0x01 || len(tlvs[1].Value) != 1 || tlvs[1].Value[0] != 0x01 {
		t.Fatalf("unexpected slot TLV: %+v", tlvs[1])
	}
}

func TestBuildCloseLogicalChannelTLVs(t *testing.T) {
	tlvs := buildCloseLogicalChannelTLVs(1, 7)
	if len(tlvs) != 3 {
		t.Fatalf("expected 3 TLVs, got %d", len(tlvs))
	}
	if tlvs[0].Type != 0x01 || tlvs[0].Value[0] != 0x01 {
		t.Fatalf("unexpected slot TLV: %+v", tlvs[0])
	}
	if tlvs[1].Type != 0x11 || tlvs[1].Value[0] != 0x07 {
		t.Fatalf("unexpected channel TLV: %+v", tlvs[1])
	}
	if tlvs[2].Type != 0x13 || tlvs[2].Value[0] != 0x01 {
		t.Fatalf("unexpected terminate TLV: %+v", tlvs[2])
	}
}

func TestBuildSendAPDUTLVs(t *testing.T) {
	command := []byte{0x80, 0xE2, 0x91, 0x00}
	tlvs := buildSendAPDUTLVs(1, 4, command)
	if len(tlvs) != 3 {
		t.Fatalf("expected 3 TLVs, got %d", len(tlvs))
	}
	if tlvs[0].Type != 0x10 || tlvs[0].Value[0] != 0x04 {
		t.Fatalf("unexpected channel TLV: %+v", tlvs[0])
	}
	if tlvs[1].Type != 0x02 {
		t.Fatalf("unexpected APDU TLV type: %+v", tlvs[1])
	}
	if got := binary.LittleEndian.Uint16(tlvs[1].Value[0:2]); got != uint16(len(command)) {
		t.Fatalf("expected APDU length %d, got %d", len(command), got)
	}
	if tlvs[2].Type != 0x01 || tlvs[2].Value[0] != 0x01 {
		t.Fatalf("unexpected slot TLV: %+v", tlvs[2])
	}
}

func TestParseOpenLogicalChannelResponse(t *testing.T) {
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x10, Value: []byte{0x05}},
		},
	}
	channel, err := parseOpenLogicalChannelResponse(resp)
	if err != nil {
		t.Fatalf("parseOpenLogicalChannelResponse returned error: %v", err)
	}
	if channel != 0x05 {
		t.Fatalf("expected channel 0x05, got 0x%02x", channel)
	}
}

func TestParseCloseLogicalChannelResponseNotSupported(t *testing.T) {
	resp := &Packet{
		TLVs: []TLV{
			qmiErrorResultTLV(QMIErrNotSupported),
		},
	}
	err := parseCloseLogicalChannelResponse(resp)
	var notSupported *NotSupportedError
	if !errors.As(err, &notSupported) {
		t.Fatalf("expected NotSupportedError, got %v", err)
	}
}

func TestParseSendAPDUResponseMissingTLV(t *testing.T) {
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
		},
	}
	if _, err := parseSendAPDUResponse(resp); err == nil {
		t.Fatal("expected missing TLV error, got nil")
	}
}
