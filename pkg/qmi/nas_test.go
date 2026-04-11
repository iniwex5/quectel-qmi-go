package qmi

import (
	"encoding/binary"
	"testing"
)

func nasTLVUint16(tlvType uint8, v uint16) TLV {
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, v)
	return TLV{Type: tlvType, Value: buf}
}

func nasTLVUint32(tlvType uint8, v uint32) TLV {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, v)
	return TLV{Type: tlvType, Value: buf}
}

func nasTLVUint64(tlvType uint8, v uint64) TLV {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, v)
	return TLV{Type: tlvType, Value: buf}
}

func TestBuildTechnologyPreferenceTLVs(t *testing.T) {
	tlvs := buildTechnologyPreferenceTLVs(TechnologyPreference{
		ActivePreference: NASTechPreference3GPP | NASTechPreferenceLTE,
		ActiveDuration:   NASPreferenceDurationPowerCycle,
	})

	if len(tlvs) != 1 {
		t.Fatalf("expected 1 TLV, got %d", len(tlvs))
	}
	if tlvs[0].Type != 0x01 {
		t.Fatalf("unexpected TLV type: 0x%02X", tlvs[0].Type)
	}
	if got := binary.LittleEndian.Uint16(tlvs[0].Value[0:2]); got != (NASTechPreference3GPP | NASTechPreferenceLTE) {
		t.Fatalf("unexpected active preference: 0x%X", got)
	}
	if tlvs[0].Value[2] != NASPreferenceDurationPowerCycle {
		t.Fatalf("unexpected active duration: 0x%02X", tlvs[0].Value[2])
	}
}

func TestParseTechnologyPreferenceResponse(t *testing.T) {
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x01, Value: []byte{0x22, 0x00, NASPreferenceDurationPermanent}},
			{Type: 0x10, Value: []byte{0x20, 0x00}},
		},
	}

	info, err := parseTechnologyPreferenceResponse(resp)
	if err != nil {
		t.Fatalf("parseTechnologyPreferenceResponse returned error: %v", err)
	}
	if info.ActivePreference != 0x22 || info.ActiveDuration != NASPreferenceDurationPermanent {
		t.Fatalf("unexpected active preference: %+v", info)
	}
	if !info.HasPersistentPreference || info.PersistentPreference != 0x20 {
		t.Fatalf("unexpected persistent preference: %+v", info)
	}
}

func TestParseRFBandInfoResponse(t *testing.T) {
	extended := []byte{
		0x02,
		0x04, 0x2C, 0x00, 0x64, 0x00, 0x00, 0x00,
		0x08, 0x4D, 0x00, 0x44, 0x01, 0x00, 0x00,
	}
	bandwidths := []byte{
		0x02,
		0x04, 0x14, 0x00, 0x00, 0x00,
		0x08, 0x64, 0x00, 0x00, 0x00,
	}
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x11, Value: extended},
			{Type: 0x12, Value: bandwidths},
		},
	}

	info, err := parseRFBandInfoResponse(resp)
	if err != nil {
		t.Fatalf("parseRFBandInfoResponse returned error: %v", err)
	}
	if len(info.Bands) != 2 {
		t.Fatalf("expected 2 band entries, got %d", len(info.Bands))
	}
	if info.Bands[0].RadioInterface != 0x04 || info.Bands[0].ActiveBandClass != 0x002C || info.Bands[0].ActiveChannel != 100 {
		t.Fatalf("unexpected first band entry: %+v", info.Bands[0])
	}
	if len(info.Bandwidths) != 2 || info.Bandwidths[1].Bandwidth != 100 {
		t.Fatalf("unexpected bandwidth entries: %+v", info.Bandwidths)
	}
}

func TestBuildSystemSelectionPreferenceTLVs(t *testing.T) {
	extLTE := [4]uint64{1, 2, 3, 4}
	pref := SystemSelectionPreference{
		ModePreference:                NASRatModePreferenceLTE | NASRatModePreferenceNR5G,
		HasModePreference:             true,
		LTEBandPreference:             0x1234,
		HasLTEBandPreference:          true,
		NetworkSelectionPreference:    NASNetworkSelectionManual,
		HasNetworkSelectionPreference: true,
		ManualNetworkSelection: ManualNetworkSelection{
			MCC:              460,
			MNC:              1,
			IncludesPCSDigit: true,
		},
		HasManualNetworkSelection:    true,
		ChangeDuration:               NASChangeDurationPermanent,
		HasChangeDuration:            true,
		ServiceDomainPreference:      NASServiceDomainPreferenceCSPS,
		HasServiceDomainPreference:   true,
		ExtendedLTEBandPreference:    extLTE,
		HasExtendedLTEBandPreference: true,
	}

	tlvs, err := buildSystemSelectionPreferenceTLVs(pref)
	if err != nil {
		t.Fatalf("buildSystemSelectionPreferenceTLVs returned error: %v", err)
	}
	if len(tlvs) != 7 {
		t.Fatalf("expected 7 TLVs, got %d", len(tlvs))
	}
	if tlvs[0].Type != 0x11 || binary.LittleEndian.Uint16(tlvs[0].Value) != (NASRatModePreferenceLTE|NASRatModePreferenceNR5G) {
		t.Fatalf("unexpected mode preference TLV: %+v", tlvs[0])
	}
	if tlvs[2].Type != 0x16 || len(tlvs[2].Value) != 5 {
		t.Fatalf("unexpected network selection TLV: %+v", tlvs[2])
	}
	if tlvs[2].Value[0] != NASNetworkSelectionManual {
		t.Fatalf("expected manual selection mode, got %d", tlvs[2].Value[0])
	}
	if got := binary.LittleEndian.Uint16(tlvs[2].Value[1:3]); got != 460 {
		t.Fatalf("unexpected MCC: %d", got)
	}
	if got := binary.LittleEndian.Uint16(tlvs[2].Value[3:5]); got != 1 {
		t.Fatalf("unexpected MNC: %d", got)
	}
	if tlvs[5].Type != 0x1A || tlvs[5].Value[0] != 0x01 {
		t.Fatalf("unexpected PCS digit TLV: %+v", tlvs[5])
	}
	if tlvs[6].Type != 0x24 || len(tlvs[6].Value) != 32 {
		t.Fatalf("unexpected extended LTE band TLV: %+v", tlvs[6])
	}
}

func TestParseSystemSelectionPreferenceResponse(t *testing.T) {
	extended := make([]byte, 32)
	for i, v := range []uint64{10, 20, 30, 40} {
		binary.LittleEndian.PutUint64(extended[i*8:(i+1)*8], v)
	}
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x10, Value: []byte{0x01}},
			nasTLVUint16(0x11, NASRatModePreferenceLTE|NASRatModePreferenceNR5G),
			nasTLVUint64(0x15, 0x1234),
			{Type: 0x16, Value: []byte{NASNetworkSelectionManual}},
			nasTLVUint32(0x18, NASServiceDomainPreferenceCSPS),
			{Type: 0x1B, Value: []byte{0xCC, 0x01, 0x01, 0x00, 0x01}},
			{Type: 0x1C, Value: []byte{0x03, 0x04, 0x08, 0x0C}},
			nasTLVUint32(0x1F, 0x77),
			nasTLVUint32(0x20, 0x55),
			nasTLVUint16(0x22, 0x99),
			{Type: 0x23, Value: extended},
		},
	}

	info, err := parseSystemSelectionPreferenceResponse(resp)
	if err != nil {
		t.Fatalf("parseSystemSelectionPreferenceResponse returned error: %v", err)
	}
	if !info.HasEmergencyMode || !info.EmergencyMode {
		t.Fatalf("unexpected emergency mode: %+v", info)
	}
	if !info.HasModePreference || info.ModePreference != (NASRatModePreferenceLTE|NASRatModePreferenceNR5G) {
		t.Fatalf("unexpected mode preference: %+v", info)
	}
	if !info.HasLTEBandPreference || info.LTEBandPreference != 0x1234 {
		t.Fatalf("unexpected LTE band preference: %+v", info)
	}
	if !info.HasManualNetworkSelection || info.ManualNetworkSelection.MCC != 460 || info.ManualNetworkSelection.MNC != 1 || !info.ManualNetworkSelection.IncludesPCSDigit {
		t.Fatalf("unexpected manual network selection: %+v", info.ManualNetworkSelection)
	}
	if len(info.AcquisitionOrderPreference) != 3 || info.AcquisitionOrderPreference[2] != 0x0C {
		t.Fatalf("unexpected acquisition order preference: %+v", info.AcquisitionOrderPreference)
	}
	if !info.HasExtendedLTEBandPreference || info.ExtendedLTEBandPreference[3] != 40 {
		t.Fatalf("unexpected extended LTE band preference: %+v", info.ExtendedLTEBandPreference)
	}
}

func TestDecodeBCDPLMN(t *testing.T) {
	mcc, mnc := decodeBCDPLMN([]byte{0x13, 0x00, 0x62})
	if mcc != "310" || mnc != "260" {
		t.Fatalf("unexpected 3-digit MNC decode: %s/%s", mcc, mnc)
	}

	mcc, mnc = decodeBCDPLMN([]byte{0x64, 0xF0, 0x10})
	if mcc != "460" || mnc != "01" {
		t.Fatalf("unexpected 2-digit MNC decode: %s/%s", mcc, mnc)
	}
}

func TestParseCellLocationInfoResponse(t *testing.T) {
	lteTLV := []byte{
		0x01,
		0x13, 0x00, 0x62,
		0x64, 0x00,
		0x78, 0x56, 0x34, 0x12,
		0xA4, 0x01,
		0x21, 0x00,
		0x07, 0x08, 0x09, 0x0A,
	}
	nrTLV := make([]byte, 22)
	rsrq := int16(-11)
	rsrp := int16(-95)
	snr := int16(25)
	copy(nrTLV[0:3], []byte{0x13, 0x00, 0x62})
	copy(nrTLV[3:6], []byte{0x00, 0x01, 0x02})
	binary.LittleEndian.PutUint64(nrTLV[6:14], 0x1122334455667788)
	binary.LittleEndian.PutUint16(nrTLV[14:16], 321)
	binary.LittleEndian.PutUint16(nrTLV[16:18], uint16(rsrq))
	binary.LittleEndian.PutUint16(nrTLV[18:20], uint16(rsrp))
	binary.LittleEndian.PutUint16(nrTLV[20:22], uint16(snr))

	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x13, Value: lteTLV},
			nasTLVUint32(0x1E, 42),
			nasTLVUint32(0x2E, 635334),
			{Type: 0x2F, Value: nrTLV},
		},
	}

	info, err := parseCellLocationInfoResponse(resp)
	if err != nil {
		t.Fatalf("parseCellLocationInfoResponse returned error: %v", err)
	}
	if info.LTE == nil || info.LTE.MCC != "310" || info.LTE.MNC != "260" || info.LTE.TAC != 100 || info.LTE.GlobalCellID != 0x12345678 {
		t.Fatalf("unexpected LTE cell info: %+v", info.LTE)
	}
	if !info.LTE.HasTimingAdvance || info.LTE.TimingAdvance != 42 {
		t.Fatalf("unexpected LTE timing advance: %+v", info.LTE)
	}
	if info.NR5G == nil || !info.NR5G.HasARFCN || info.NR5G.ARFCN != 635334 {
		t.Fatalf("unexpected NR ARFCN: %+v", info.NR5G)
	}
	if info.NR5G.TAC != 258 || info.NR5G.GlobalCellID != 0x1122334455667788 || info.NR5G.PhysicalCellID != 321 {
		t.Fatalf("unexpected NR cell info: %+v", info.NR5G)
	}
}

func TestParseServingSystemIndication(t *testing.T) {
	packet := &Packet{
		TLVs: []TLV{
			{Type: 0x01, Value: []byte{byte(RegStateRegistered), 0x00, 0x01, 0x00, 0x01, 0x04}},
			{Type: 0x10, Value: []byte{0x01}},
			{Type: 0x12, Value: []byte{0xCC, 0x01, 0x01, 0x00}},
		},
	}

	info, err := ParseServingSystemIndication(packet)
	if err != nil {
		t.Fatalf("ParseServingSystemIndication returned error: %v", err)
	}
	if info.RegistrationState != RegStateRegistered || !info.PSAttached || info.RadioInterface != 0x04 {
		t.Fatalf("unexpected serving system indication payload: %+v", info)
	}
	if info.MCC != 460 || info.MNC != 1 {
		t.Fatalf("unexpected PLMN decode: %+v", info)
	}
}

func TestParseNetworkTimeResponse(t *testing.T) {
	resp := &Packet{
		TLVs: []TLV{
			successResultTLV(),
			{Type: 0x10, Value: []byte{0xE8, 0x07, 0x04, 0x07, 0x0A, 0x1E, 0x2D, 0x02, 0x08, 0x01, 0x04}},
			{Type: 0x11, Value: []byte{0xE8, 0x07, 0x04, 0x07, 0x0B, 0x1F, 0x2E, 0x02, 0x08, 0x00, 0x08}},
		},
	}

	info, err := parseNetworkTimeResponse(resp)
	if err != nil {
		t.Fatalf("parseNetworkTimeResponse returned error: %v", err)
	}
	if !info.HasThreeGPP2 || info.ThreeGPP2.Year != 2024 || info.ThreeGPP2.Hour != 10 || info.ThreeGPP2.RadioInterface != 0x04 {
		t.Fatalf("unexpected 3GPP2 time: %+v", info.ThreeGPP2)
	}
	if !info.HasThreeGPP || info.ThreeGPP.Minute != 31 || info.ThreeGPP.DaylightSavingsAdjustment != 0 || info.ThreeGPP.RadioInterface != 0x08 {
		t.Fatalf("unexpected 3GPP time: %+v", info.ThreeGPP)
	}
}
