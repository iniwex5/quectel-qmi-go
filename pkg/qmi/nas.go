package qmi

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

// ============================================================================
// NAS Registration States / NAS注册状态
// ============================================================================

type RegistrationState uint8

const (
	RegStateNotRegistered RegistrationState = 0
	RegStateRegistered    RegistrationState = 1
	RegStateSearching     RegistrationState = 2
	RegStateDenied        RegistrationState = 3
	RegStateUnknown       RegistrationState = 4
	RegStateRoaming       RegistrationState = 5
)

func (r RegistrationState) String() string {
	switch r {
	case RegStateNotRegistered:
		return "not_registered"
	case RegStateRegistered:
		return "registered"
	case RegStateSearching:
		return "searching"
	case RegStateDenied:
		return "denied"
	case RegStateRoaming:
		return "roaming"
	default:
		return "unknown"
	}
}

// ============================================================================
// NAS Service wrapper / NAS服务包装器
// ============================================================================

const (
	NASSetTechnologyPreference      uint16 = 0x002A
	NASGetTechnologyPreference      uint16 = 0x002B
	NASGetRFBandInfo                uint16 = 0x0031
	NASSetSystemSelectionPreference uint16 = 0x0033
	NASGetSystemSelectionPreference uint16 = 0x0034
	NASGetCellLocationInfo          uint16 = 0x0043
	NASGetSignalInfo                uint16 = 0x004F
	NASPerformNetworkScan           uint16 = 0x0021
	NASGetNetworkTime               uint16 = 0x007D
	/* Defined in frame.go / 在 frame.go 中定义
	NASGetServingSystem  uint16 = 0x0024
	NASGetSignalStrength uint16 = 0x0020
	NASGetSysInfo        uint16 = 0x004D
	*/
)

const (
	NASTechPreferenceAuto        uint16 = 0
	NASTechPreference3GPP2       uint16 = 1 << 0
	NASTechPreference3GPP        uint16 = 1 << 1
	NASTechPreferenceAMPSOrGSM   uint16 = 1 << 2
	NASTechPreferenceCDMAOrWCDMA uint16 = 1 << 3
	NASTechPreferenceHDR         uint16 = 1 << 4
	NASTechPreferenceLTE         uint16 = 1 << 5
)

const (
	NASPreferenceDurationPermanent     uint8 = 0x00
	NASPreferenceDurationPowerCycle    uint8 = 0x01
	NASPreferenceDurationOneCall       uint8 = 0x02
	NASPreferenceDurationOneCallOrTime uint8 = 0x03
	NASPreferenceDurationInternalCall1 uint8 = 0x04
	NASPreferenceDurationInternalCall2 uint8 = 0x05
	NASPreferenceDurationInternalCall3 uint8 = 0x06
)

const (
	NASRatModePreferenceCDMA1X     uint16 = 1 << 0
	NASRatModePreferenceCDMA1XEVDO uint16 = 1 << 1
	NASRatModePreferenceGSM        uint16 = 1 << 2
	NASRatModePreferenceUMTS       uint16 = 1 << 3
	NASRatModePreferenceLTE        uint16 = 1 << 4
	NASRatModePreferenceTDSCDMA    uint16 = 1 << 5
	NASRatModePreferenceNR5G       uint16 = 1 << 6
)

const (
	NASRoamingPreferenceOff         uint16 = 0x01
	NASRoamingPreferenceNotOff      uint16 = 0x02
	NASRoamingPreferenceNotFlashing uint16 = 0x03
	NASRoamingPreferenceAny         uint16 = 0xFF
)

const (
	NASNetworkSelectionAutomatic uint8 = 0x00
	NASNetworkSelectionManual    uint8 = 0x01
)

const (
	NASChangeDurationPowerCycle uint8 = 0x00
	NASChangeDurationPermanent  uint8 = 0x01
)

const (
	NASServiceDomainPreferenceCSOnly   uint32 = 0x00
	NASServiceDomainPreferencePSOnly   uint32 = 0x01
	NASServiceDomainPreferenceCSPS     uint32 = 0x02
	NASServiceDomainPreferencePSAttach uint32 = 0x03
	NASServiceDomainPreferencePSDetach uint32 = 0x04
)

// NetworkScanResult represents a network found during scan / NetworkScanResult 代表扫描期间发现的网络
type NetworkScanResult struct {
	MCC         string
	MNC         string
	Status      uint8 // 0: Unknown, 1: Current, 2: Available, 3: Forbidden
	Description string
	RATs        []uint8
}

// SignalInfo contains detailed signal strength information / SignalInfo 包含详细的信号强度信息
type SignalInfo struct {
	// LTE specific
	LTERSRP  int16 // Reference Signal Received Power
	LTERSRQ  int16 // Reference Signal Received Quality
	LTERSSNR int16 // Signal-to-Noise Ratio

	// 5G specific
	NR5GRSRP int16
	NR5GRSRQ int16
	NR5GSINR int16
}

// SysInfo contains system information / SysInfo 包含系统信息
type SysInfo struct {
	CellID uint64
	TAC    uint16 // Tracking Area Code
	LAC    uint16 // Location Area Code
}

// RFBandInfoEntry describes one active RF band/channel tuple.
type RFBandInfoEntry struct {
	RadioInterface  uint8
	ActiveBandClass uint16
	ActiveChannel   uint32
}

// RFBandwidthInfo describes the configured downlink bandwidth for one radio interface.
type RFBandwidthInfo struct {
	RadioInterface uint8
	Bandwidth      uint32
}

// RFBandInfo groups active band/channel and bandwidth information.
type RFBandInfo struct {
	Bands      []RFBandInfoEntry
	Bandwidths []RFBandwidthInfo
}

// TechnologyPreference contains active and persistent RAT preference information.
type TechnologyPreference struct {
	ActivePreference        uint16
	ActiveDuration          uint8
	PersistentPreference    uint16
	HasPersistentPreference bool
}

// ManualNetworkSelection holds manual PLMN selection fields.
type ManualNetworkSelection struct {
	MCC              uint16
	MNC              uint16
	IncludesPCSDigit bool
}

// SystemSelectionPreference models the commonly used NAS system-selection knobs.
type SystemSelectionPreference struct {
	EmergencyMode                              bool
	HasEmergencyMode                           bool
	ModePreference                             uint16
	HasModePreference                          bool
	BandPreference                             uint64
	HasBandPreference                          bool
	CDMAPRLPreference                          uint16
	HasCDMAPRLPreference                       bool
	RoamingPreference                          uint16
	HasRoamingPreference                       bool
	LTEBandPreference                          uint64
	HasLTEBandPreference                       bool
	TDSCDMABandPreference                      uint64
	HasTDSCDMABandPreference                   bool
	NetworkSelectionPreference                 uint8
	HasNetworkSelectionPreference              bool
	ManualNetworkSelection                     ManualNetworkSelection
	HasManualNetworkSelection                  bool
	ChangeDuration                             uint8
	HasChangeDuration                          bool
	ServiceDomainPreference                    uint32
	HasServiceDomainPreference                 bool
	GSMWCDMAAcquisitionOrderPreference         uint32
	HasGSMWCDMAAcquisitionOrderPreference      bool
	AcquisitionOrderPreference                 []uint8
	DisabledModes                              uint16
	HasDisabledModes                           bool
	NetworkSelectionRegistrationRestriction    uint32
	HasNetworkSelectionRegistrationRestriction bool
	UsagePreference                            uint32
	HasUsagePreference                         bool
	VoiceDomainPreference                      uint32
	HasVoiceDomainPreference                   bool
	ExtendedLTEBandPreference                  [4]uint64
	HasExtendedLTEBandPreference               bool
	NR5GSABandPreference                       [8]uint64
	HasNR5GSABandPreference                    bool
	NR5GNSABandPreference                      [8]uint64
	HasNR5GNSABandPreference                   bool
}

// GERANCellLocationInfo contains serving GERAN cell fields.
type GERANCellLocationInfo struct {
	CellID           uint32
	MCC              string
	MNC              string
	LAC              uint16
	ARFCN            uint16
	BaseStationID    uint8
	TimingAdvance    uint32
	HasTimingAdvance bool
	RXLevel          uint16
}

// UMTSCellLocationInfo contains serving UMTS cell fields.
type UMTSCellLocationInfo struct {
	CellID                uint32
	MCC                   string
	MNC                   string
	LAC                   uint16
	UARFCN                uint16
	PrimaryScramblingCode uint16
	RSCP                  int16
	ECIO                  int16
}

// LTECellLocationInfo contains serving LTE cell fields.
type LTECellLocationInfo struct {
	UEInIdle                 bool
	MCC                      string
	MNC                      string
	TAC                      uint16
	GlobalCellID             uint32
	EARFCN                   uint16
	ServingCellID            uint16
	CellReselectionPriority  uint8
	SNonIntraSearchThreshold uint8
	ServingCellLowThreshold  uint8
	SIntraSearchThreshold    uint8
	HasIdleThresholds        bool
	TimingAdvance            uint32
	HasTimingAdvance         bool
}

// NR5GCellLocationInfo contains serving NR5G cell fields.
type NR5GCellLocationInfo struct {
	MCC            string
	MNC            string
	TAC            uint32
	GlobalCellID   uint64
	PhysicalCellID uint16
	RSRQ           int16
	RSRP           int16
	SNR            int16
	ARFCN          uint32
	HasARFCN       bool
}

// CellLocationInfo combines serving-cell details from different RAT families.
type CellLocationInfo struct {
	GERAN *GERANCellLocationInfo
	UMTS  *UMTSCellLocationInfo
	LTE   *LTECellLocationInfo
	NR5G  *NR5GCellLocationInfo
}

// NetworkTime is one network time source returned by NAS.
type NetworkTime struct {
	Year                      uint16
	Month                     uint8
	Day                       uint8
	Hour                      uint8
	Minute                    uint8
	Second                    uint8
	DayOfWeek                 uint8
	TimezoneOffsetQuarters    int8
	DaylightSavingsAdjustment uint8
	RadioInterface            uint8
}

// NetworkTimeInfo groups 3GPP and 3GPP2 time values.
type NetworkTimeInfo struct {
	ThreeGPP     NetworkTime
	HasThreeGPP  bool
	ThreeGPP2    NetworkTime
	HasThreeGPP2 bool
}

type NASService struct {
	client   *Client
	clientID uint8
}

// NewNASService creates a NAS service wrapper / NewNASService创建一个NAS服务包装器
func NewNASService(client *Client) (*NASService, error) {
	clientID, err := client.AllocateClientID(ServiceNAS)
	if err != nil {
		return nil, err
	}
	return &NASService{client: client, clientID: clientID}, nil
}

// Close releases the NAS client ID / Close释放NAS客户端ID
func (n *NASService) Close() error {
	return n.client.ReleaseClientID(ServiceNAS, n.clientID)
}

func (n *NASService) ClientID() uint8 {
	return n.clientID
}

// ServingSystem contains network registration info / ServingSystem包含网络注册信息
type ServingSystem struct {
	RegistrationState RegistrationState
	PSAttached        bool
	RadioInterface    uint8 // 0=none, 1=CDMA, 2=UMTS, 4=LTE, 5=LTE-M, 6=NR5G / 0=无, 1=CDMA, 2=UMTS, 4=LTE, 5=LTE-M, 6=NR5G
	MCC               uint16
	MNC               uint16
}

// GetServingSystem queries the current serving system / GetServingSystem查询当前服务系统
func (n *NASService) GetServingSystem(ctx context.Context) (*ServingSystem, error) {
	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASGetServingSystem, nil)
	if err != nil {
		return nil, err
	}
	return parseServingSystemPacket(resp, true)
}

func ParseServingSystemIndication(packet *Packet) (*ServingSystem, error) {
	return parseServingSystemPacket(packet, false)
}

// IsRegistered checks if we're registered on the network / IsRegistered检查我们是否已在网络上注册
func (n *NASService) IsRegistered(ctx context.Context) (bool, error) {
	ss, err := n.GetServingSystem(ctx)
	if err != nil {
		return false, err
	}
	return (ss.RegistrationState == RegStateRegistered || ss.RegistrationState == RegStateRoaming) && ss.PSAttached, nil
}

// SignalStrength contains signal quality info / SignalStrength包含信号质量信息
type SignalStrength struct {
	RSSI int8  // dBm
	ECIO int16 // dB * 10 (for UMTS) / dB * 10 (用于UMTS)
	RSRP int16 // dBm (for LTE) / dBm (用于LTE)
	RSRQ int8  // dB (for LTE) / dB (用于LTE)
	SNR  int16 // dB * 10 (for LTE) / dB * 10 (用于LTE)
}

// GetSignalStrength queries current signal strength / GetSignalStrength查询当前信号强度
func (n *NASService) GetSignalStrength(ctx context.Context) (*SignalStrength, error) {
	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASGetSignalStrength, nil)
	if err != nil {
		return nil, err
	}

	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("get signal strength failed: %w", err)
	}

	sig := &SignalStrength{}

	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 1 {
		sig.RSSI = int8(tlv.Value[0])
	}
	if tlv := FindTLV(resp.TLVs, 0x16); tlv != nil && len(tlv.Value) >= 1 {
		sig.RSRQ = int8(tlv.Value[0])
	}
	if tlv := FindTLV(resp.TLVs, 0x17); tlv != nil && len(tlv.Value) >= 2 {
		sig.SNR = int16(binary.LittleEndian.Uint16(tlv.Value))
	}
	if tlv := FindTLV(resp.TLVs, 0x18); tlv != nil && len(tlv.Value) >= 2 {
		sig.RSRP = int16(binary.LittleEndian.Uint16(tlv.Value))
	}

	return sig, nil
}

func parseServingSystemPacket(packet *Packet, checkResult bool) (*ServingSystem, error) {
	if checkResult {
		if err := packet.CheckResult(); err != nil {
			return nil, fmt.Errorf("get serving system failed: %w", err)
		}
	}

	ss := &ServingSystem{}

	// TLV 0x01: Serving system / TLV 0x01: 服务系统
	if tlv := FindTLV(packet.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 3 {
		ss.RegistrationState = RegistrationState(tlv.Value[0])
		ss.PSAttached = tlv.Value[2] == 1

		if len(tlv.Value) >= 6 {
			numIfaces := int(tlv.Value[4])
			if numIfaces > 0 && len(tlv.Value) >= 5+numIfaces {
				ss.RadioInterface = tlv.Value[5]
			}
		}
	}

	// TLV 0x10: Roaming Indicator (0x00 = Roaming ON, 0x01 = Roaming OFF)
	if tlv := FindTLV(packet.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 1 {
		if tlv.Value[0] == 0x00 && ss.RegistrationState == RegStateRegistered {
			ss.RegistrationState = RegStateRoaming
		}
	}

	// TLV 0x12: Current PLMN / TLV 0x12: 当前PLMN
	if tlv := FindTLV(packet.TLVs, 0x12); tlv != nil && len(tlv.Value) >= 4 {
		ss.MCC = binary.LittleEndian.Uint16(tlv.Value[0:2])
		ss.MNC = binary.LittleEndian.Uint16(tlv.Value[2:4])
	}

	return ss, nil
}

// RegisterIndications enables NAS unsolicited indications / RegisterIndications启用NAS主动指示
func (n *NASService) RegisterIndications() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	thresholds := []int8{-60, -85}
	th := make([]byte, 0, len(thresholds))
	for _, v := range thresholds {
		th = append(th, byte(v))
	}
	tlvs := []TLV{
		{Type: 0x10, Value: append([]byte{0x01, uint8(len(thresholds))}, th...)},
	}

	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASSetEventReport, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

// GetSignalInfo gets detailed signal information (LTE/5G) / GetSignalInfo 获取详细信号信息 (LTE/5G)
func (s *NASService) GetSignalInfo(ctx context.Context) (*SignalInfo, error) {
	resp, err := s.client.SendRequest(ctx, ServiceNAS, s.clientID, NASGetSignalInfo, nil)
	if err != nil {
		return nil, err
	}

	if err := resp.CheckResult(); err != nil {
		return nil, err
	}

	info := &SignalInfo{}

	if tlv := FindTLV(resp.TLVs, 0x14); tlv != nil && len(tlv.Value) >= 6 {
		info.LTERSRQ = int16(int8(tlv.Value[1]))
		info.LTERSRP = int16(binary.LittleEndian.Uint16(tlv.Value[2:4]))
		info.LTERSSNR = int16(binary.LittleEndian.Uint16(tlv.Value[4:6]))
	}

	if tlv := FindTLV(resp.TLVs, 0x17); tlv != nil && len(tlv.Value) >= 6 {
		info.NR5GRSRP = int16(binary.LittleEndian.Uint16(tlv.Value[2:4]))
		info.NR5GRSRQ = int16(binary.LittleEndian.Uint16(tlv.Value[0:2]))
		info.NR5GSINR = int16(binary.LittleEndian.Uint16(tlv.Value[4:6]))
	}

	return info, nil
}

// GetSysInfo gets system information including Cell ID / GetSysInfo 获取系统信息，包括 Cell ID
func (s *NASService) GetSysInfo(ctx context.Context) (*SysInfo, error) {
	resp, err := s.client.SendRequest(ctx, ServiceNAS, s.clientID, NASGetSysInfo, nil)
	if err != nil {
		return nil, err
	}

	if err := resp.CheckResult(); err != nil {
		return nil, err
	}

	return ParseSysInfoIndication(resp)
}

func ParseSysInfoIndication(packet *Packet) (*SysInfo, error) {
	info := &SysInfo{}

	if tlv := FindTLV(packet.TLVs, 0x19); tlv != nil && len(tlv.Value) >= 16 {
		info.CellID = uint64(binary.LittleEndian.Uint32(tlv.Value[12:16]))
		if len(tlv.Value) >= 29 {
			info.TAC = binary.LittleEndian.Uint16(tlv.Value[27:29])
		}
	}

	return info, nil
}

// PerformNetworkScan scans for available networks / PerformNetworkScan 扫描可用网络
func (s *NASService) PerformNetworkScan(ctx context.Context) ([]NetworkScanResult, error) {
	resp, err := s.client.SendRequest(ctx, ServiceNAS, s.clientID, NASPerformNetworkScan, nil)
	if err != nil {
		return nil, err
	}

	if err := resp.CheckResult(); err != nil {
		return nil, err
	}

	var results []NetworkScanResult
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 2 {
		n := int(binary.LittleEndian.Uint16(tlv.Value[0:2]))
		offset := 2
		for i := 0; i < n; i++ {
			if len(tlv.Value)-offset < 6 {
				break
			}
			mcc := binary.LittleEndian.Uint16(tlv.Value[offset : offset+2])
			mnc := binary.LittleEndian.Uint16(tlv.Value[offset+2 : offset+4])
			status := tlv.Value[offset+4]
			descLen := int(tlv.Value[offset+5])
			offset += 6
			if len(tlv.Value)-offset < descLen {
				break
			}
			desc := ""
			if descLen > 0 {
				desc = string(tlv.Value[offset : offset+descLen])
				offset += descLen
			}
			results = append(results, NetworkScanResult{
				MCC:         fmt.Sprintf("%03d", mcc),
				MNC:         fmt.Sprintf("%03d", mnc),
				Status:      status,
				Description: desc,
			})
		}
	}

	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil && len(tlv.Value) >= 2 {
		n := int(binary.LittleEndian.Uint16(tlv.Value[0:2]))
		offset := 2
		for i := 0; i < n; i++ {
			if len(tlv.Value)-offset < 5 {
				break
			}
			mcc := fmt.Sprintf("%03d", binary.LittleEndian.Uint16(tlv.Value[offset:offset+2]))
			mnc := fmt.Sprintf("%03d", binary.LittleEndian.Uint16(tlv.Value[offset+2:offset+4]))
			rat := tlv.Value[offset+4]
			offset += 5
			for j := range results {
				if results[j].MCC == mcc && results[j].MNC == mnc {
					results[j].RATs = append(results[j].RATs, rat)
					break
				}
			}
		}
	}

	return results, nil
}

// GetRFBandInfo returns current active band/channel details.
func (n *NASService) GetRFBandInfo(ctx context.Context) (*RFBandInfo, error) {
	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASGetRFBandInfo, nil)
	if err != nil {
		return nil, err
	}
	return parseRFBandInfoResponse(resp)
}

// GetTechnologyPreference returns the active and persistent technology preference.
func (n *NASService) GetTechnologyPreference(ctx context.Context) (*TechnologyPreference, error) {
	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASGetTechnologyPreference, nil)
	if err != nil {
		return nil, err
	}
	return parseTechnologyPreferenceResponse(resp)
}

// SetTechnologyPreference updates the active technology preference.
func (n *NASService) SetTechnologyPreference(ctx context.Context, pref TechnologyPreference) error {
	tlvs := buildTechnologyPreferenceTLVs(pref)
	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASSetTechnologyPreference, tlvs)
	if err != nil {
		return err
	}
	if err := resp.CheckResult(); err != nil {
		return fmt.Errorf("set technology preference failed: %w", err)
	}
	return nil
}

// GetSystemSelectionPreference returns the modem system-selection policy.
func (n *NASService) GetSystemSelectionPreference(ctx context.Context) (*SystemSelectionPreference, error) {
	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASGetSystemSelectionPreference, nil)
	if err != nil {
		return nil, err
	}
	return parseSystemSelectionPreferenceResponse(resp)
}

// SetSystemSelectionPreference updates one or more system-selection policy fields.
func (n *NASService) SetSystemSelectionPreference(ctx context.Context, pref SystemSelectionPreference) error {
	tlvs, err := buildSystemSelectionPreferenceTLVs(pref)
	if err != nil {
		return err
	}
	if len(tlvs) == 0 {
		return fmt.Errorf("set system selection preference requires at least one field")
	}

	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASSetSystemSelectionPreference, tlvs)
	if err != nil {
		return err
	}
	if err := resp.CheckResult(); err != nil {
		return fmt.Errorf("set system selection preference failed: %w", err)
	}
	return nil
}

// GetCellLocationInfo returns serving-cell details for the current RAT.
func (n *NASService) GetCellLocationInfo(ctx context.Context) (*CellLocationInfo, error) {
	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASGetCellLocationInfo, nil)
	if err != nil {
		return nil, err
	}
	return parseCellLocationInfoResponse(resp)
}

// GetNetworkTime returns network-provided time values from 3GPP/3GPP2.
func (n *NASService) GetNetworkTime(ctx context.Context) (*NetworkTimeInfo, error) {
	resp, err := n.client.SendRequest(ctx, ServiceNAS, n.clientID, NASGetNetworkTime, nil)
	if err != nil {
		return nil, err
	}
	return parseNetworkTimeResponse(resp)
}

// ============================================================================
// Internal helpers / 内部助手
// ============================================================================

func buildTechnologyPreferenceTLVs(pref TechnologyPreference) []TLV {
	buf := make([]byte, 3)
	binary.LittleEndian.PutUint16(buf[0:2], pref.ActivePreference)
	buf[2] = pref.ActiveDuration
	return []TLV{{Type: 0x01, Value: buf}}
}

func buildSystemSelectionPreferenceTLVs(pref SystemSelectionPreference) ([]TLV, error) {
	var tlvs []TLV

	if pref.HasEmergencyMode {
		tlvs = append(tlvs, NewTLVUint8(0x10, boolToUint8(pref.EmergencyMode)))
	}
	if pref.HasModePreference {
		tlvs = append(tlvs, NewTLVUint16(0x11, pref.ModePreference))
	}
	if pref.HasBandPreference {
		tlvs = append(tlvs, newTLVUint64(0x12, pref.BandPreference))
	}
	if pref.HasCDMAPRLPreference {
		tlvs = append(tlvs, NewTLVUint16(0x13, pref.CDMAPRLPreference))
	}
	if pref.HasRoamingPreference {
		tlvs = append(tlvs, NewTLVUint16(0x14, pref.RoamingPreference))
	}
	if pref.HasLTEBandPreference {
		tlvs = append(tlvs, newTLVUint64(0x15, pref.LTEBandPreference))
	}
	if pref.HasTDSCDMABandPreference {
		tlvs = append(tlvs, newTLVUint64(0x1D, pref.TDSCDMABandPreference))
	}
	if pref.HasNetworkSelectionPreference || pref.HasManualNetworkSelection {
		mode := pref.NetworkSelectionPreference
		mcc := uint16(0)
		mnc := uint16(0)
		if pref.HasManualNetworkSelection {
			mode = NASNetworkSelectionManual
			mcc = pref.ManualNetworkSelection.MCC
			mnc = pref.ManualNetworkSelection.MNC
		}
		buf := make([]byte, 5)
		buf[0] = mode
		binary.LittleEndian.PutUint16(buf[1:3], mcc)
		binary.LittleEndian.PutUint16(buf[3:5], mnc)
		tlvs = append(tlvs, TLV{Type: 0x16, Value: buf})
	}
	if pref.HasChangeDuration {
		tlvs = append(tlvs, NewTLVUint8(0x17, pref.ChangeDuration))
	}
	if pref.HasServiceDomainPreference {
		tlvs = append(tlvs, NewTLVUint32(0x18, pref.ServiceDomainPreference))
	}
	if pref.HasGSMWCDMAAcquisitionOrderPreference {
		tlvs = append(tlvs, NewTLVUint32(0x19, pref.GSMWCDMAAcquisitionOrderPreference))
	}
	if pref.HasManualNetworkSelection {
		tlvs = append(tlvs, NewTLVUint8(0x1A, boolToUint8(pref.ManualNetworkSelection.IncludesPCSDigit)))
	}
	if len(pref.AcquisitionOrderPreference) > 0 {
		buf := make([]byte, 1+len(pref.AcquisitionOrderPreference))
		buf[0] = uint8(len(pref.AcquisitionOrderPreference))
		copy(buf[1:], pref.AcquisitionOrderPreference)
		tlvs = append(tlvs, TLV{Type: 0x1E, Value: buf})
	}
	if pref.HasNetworkSelectionRegistrationRestriction {
		tlvs = append(tlvs, NewTLVUint32(0x1F, pref.NetworkSelectionRegistrationRestriction))
	}
	if pref.HasUsagePreference {
		tlvs = append(tlvs, NewTLVUint32(0x21, pref.UsagePreference))
	}
	if pref.HasVoiceDomainPreference {
		tlvs = append(tlvs, NewTLVUint32(0x23, pref.VoiceDomainPreference))
	}
	if pref.HasExtendedLTEBandPreference {
		tlv, err := newTLVUint64Sequence(0x24, pref.ExtendedLTEBandPreference[:])
		if err != nil {
			return nil, err
		}
		tlvs = append(tlvs, tlv)
	}
	if pref.HasNR5GSABandPreference {
		tlv, err := newTLVUint64Sequence(0x2F, pref.NR5GSABandPreference[:])
		if err != nil {
			return nil, err
		}
		tlvs = append(tlvs, tlv)
	}
	if pref.HasNR5GNSABandPreference {
		tlv, err := newTLVUint64Sequence(0x30, pref.NR5GNSABandPreference[:])
		if err != nil {
			return nil, err
		}
		tlvs = append(tlvs, tlv)
	}

	return tlvs, nil
}

func parseRFBandInfoResponse(resp *Packet) (*RFBandInfo, error) {
	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("get RF band information failed: %w", err)
	}

	info := &RFBandInfo{}

	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil && len(tlv.Value) >= 1 {
		count := int(tlv.Value[0])
		offset := 1
		info.Bands = make([]RFBandInfoEntry, 0, count)
		for i := 0; i < count; i++ {
			if offset+7 > len(tlv.Value) {
				break
			}
			info.Bands = append(info.Bands, RFBandInfoEntry{
				RadioInterface:  tlv.Value[offset],
				ActiveBandClass: binary.LittleEndian.Uint16(tlv.Value[offset+1 : offset+3]),
				ActiveChannel:   binary.LittleEndian.Uint32(tlv.Value[offset+3 : offset+7]),
			})
			offset += 7
		}
	} else if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 1 {
		count := int(tlv.Value[0])
		offset := 1
		info.Bands = make([]RFBandInfoEntry, 0, count)
		for i := 0; i < count; i++ {
			if offset+5 > len(tlv.Value) {
				break
			}
			info.Bands = append(info.Bands, RFBandInfoEntry{
				RadioInterface:  tlv.Value[offset],
				ActiveBandClass: binary.LittleEndian.Uint16(tlv.Value[offset+1 : offset+3]),
				ActiveChannel:   uint32(binary.LittleEndian.Uint16(tlv.Value[offset+3 : offset+5])),
			})
			offset += 5
		}
	}

	if tlv := FindTLV(resp.TLVs, 0x12); tlv != nil && len(tlv.Value) >= 1 {
		count := int(tlv.Value[0])
		offset := 1
		info.Bandwidths = make([]RFBandwidthInfo, 0, count)
		for i := 0; i < count; i++ {
			if offset+5 > len(tlv.Value) {
				break
			}
			info.Bandwidths = append(info.Bandwidths, RFBandwidthInfo{
				RadioInterface: tlv.Value[offset],
				Bandwidth:      binary.LittleEndian.Uint32(tlv.Value[offset+1 : offset+5]),
			})
			offset += 5
		}
	}

	if len(info.Bands) == 0 && len(info.Bandwidths) == 0 {
		return nil, fmt.Errorf("no RF band information TLVs in response")
	}
	return info, nil
}

func parseTechnologyPreferenceResponse(resp *Packet) (*TechnologyPreference, error) {
	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("get technology preference failed: %w", err)
	}

	info := &TechnologyPreference{}
	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 3 {
		info.ActivePreference = binary.LittleEndian.Uint16(tlv.Value[0:2])
		info.ActiveDuration = tlv.Value[2]
	}
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 2 {
		info.PersistentPreference = binary.LittleEndian.Uint16(tlv.Value[0:2])
		info.HasPersistentPreference = true
	}
	return info, nil
}

func parseSystemSelectionPreferenceResponse(resp *Packet) (*SystemSelectionPreference, error) {
	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("get system selection preference failed: %w", err)
	}

	info := &SystemSelectionPreference{}
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 1 {
		info.EmergencyMode = tlv.Value[0] != 0
		info.HasEmergencyMode = true
	}
	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil && len(tlv.Value) >= 2 {
		info.ModePreference = binary.LittleEndian.Uint16(tlv.Value[0:2])
		info.HasModePreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x12); tlv != nil && len(tlv.Value) >= 8 {
		info.BandPreference = binary.LittleEndian.Uint64(tlv.Value[0:8])
		info.HasBandPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x13); tlv != nil && len(tlv.Value) >= 2 {
		info.CDMAPRLPreference = binary.LittleEndian.Uint16(tlv.Value[0:2])
		info.HasCDMAPRLPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x14); tlv != nil && len(tlv.Value) >= 2 {
		info.RoamingPreference = binary.LittleEndian.Uint16(tlv.Value[0:2])
		info.HasRoamingPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x15); tlv != nil && len(tlv.Value) >= 8 {
		info.LTEBandPreference = binary.LittleEndian.Uint64(tlv.Value[0:8])
		info.HasLTEBandPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x1A); tlv != nil && len(tlv.Value) >= 8 {
		info.TDSCDMABandPreference = binary.LittleEndian.Uint64(tlv.Value[0:8])
		info.HasTDSCDMABandPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x16); tlv != nil && len(tlv.Value) >= 1 {
		info.NetworkSelectionPreference = tlv.Value[0]
		info.HasNetworkSelectionPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x18); tlv != nil && len(tlv.Value) >= 4 {
		info.ServiceDomainPreference = binary.LittleEndian.Uint32(tlv.Value[0:4])
		info.HasServiceDomainPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x19); tlv != nil && len(tlv.Value) >= 4 {
		info.GSMWCDMAAcquisitionOrderPreference = binary.LittleEndian.Uint32(tlv.Value[0:4])
		info.HasGSMWCDMAAcquisitionOrderPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x1B); tlv != nil && len(tlv.Value) >= 5 {
		info.ManualNetworkSelection = ManualNetworkSelection{
			MCC:              binary.LittleEndian.Uint16(tlv.Value[0:2]),
			MNC:              binary.LittleEndian.Uint16(tlv.Value[2:4]),
			IncludesPCSDigit: tlv.Value[4] != 0,
		}
		info.HasManualNetworkSelection = true
	}
	if tlv := FindTLV(resp.TLVs, 0x1C); tlv != nil && len(tlv.Value) >= 1 {
		count := int(tlv.Value[0])
		if len(tlv.Value) >= 1+count {
			info.AcquisitionOrderPreference = append([]uint8(nil), tlv.Value[1:1+count]...)
		}
	}
	if tlv := FindTLV(resp.TLVs, 0x1D); tlv != nil && len(tlv.Value) >= 4 {
		info.NetworkSelectionRegistrationRestriction = binary.LittleEndian.Uint32(tlv.Value[0:4])
		info.HasNetworkSelectionRegistrationRestriction = true
	}
	if tlv := FindTLV(resp.TLVs, 0x1F); tlv != nil && len(tlv.Value) >= 4 {
		info.UsagePreference = binary.LittleEndian.Uint32(tlv.Value[0:4])
		info.HasUsagePreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x20); tlv != nil && len(tlv.Value) >= 4 {
		info.VoiceDomainPreference = binary.LittleEndian.Uint32(tlv.Value[0:4])
		info.HasVoiceDomainPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x22); tlv != nil && len(tlv.Value) >= 2 {
		info.DisabledModes = binary.LittleEndian.Uint16(tlv.Value[0:2])
		info.HasDisabledModes = true
	}
	if tlv := FindTLV(resp.TLVs, 0x23); tlv != nil {
		values, err := parseUint64Sequence(tlv.Value, 4)
		if err != nil {
			return nil, err
		}
		copy(info.ExtendedLTEBandPreference[:], values)
		info.HasExtendedLTEBandPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x2C); tlv != nil {
		values, err := parseUint64Sequence(tlv.Value, 8)
		if err != nil {
			return nil, err
		}
		copy(info.NR5GSABandPreference[:], values)
		info.HasNR5GSABandPreference = true
	}
	if tlv := FindTLV(resp.TLVs, 0x2D); tlv != nil {
		values, err := parseUint64Sequence(tlv.Value, 8)
		if err != nil {
			return nil, err
		}
		copy(info.NR5GNSABandPreference[:], values)
		info.HasNR5GNSABandPreference = true
	}

	return info, nil
}

func parseCellLocationInfoResponse(resp *Packet) (*CellLocationInfo, error) {
	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("get cell location info failed: %w", err)
	}

	info := &CellLocationInfo{}

	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 18 {
		mcc, mnc := decodeBCDPLMN(tlv.Value[4:7])
		geran := &GERANCellLocationInfo{
			CellID:        binary.LittleEndian.Uint32(tlv.Value[0:4]),
			MCC:           mcc,
			MNC:           mnc,
			LAC:           binary.LittleEndian.Uint16(tlv.Value[7:9]),
			ARFCN:         binary.LittleEndian.Uint16(tlv.Value[9:11]),
			BaseStationID: tlv.Value[11],
			RXLevel:       binary.LittleEndian.Uint16(tlv.Value[16:18]),
		}
		timingAdvance := binary.LittleEndian.Uint32(tlv.Value[12:16])
		if timingAdvance != 0xFFFFFFFF {
			geran.TimingAdvance = timingAdvance
			geran.HasTimingAdvance = true
		}
		info.GERAN = geran
	}

	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil && len(tlv.Value) >= 15 {
		mcc, mnc := decodeBCDPLMN(tlv.Value[2:5])
		info.UMTS = &UMTSCellLocationInfo{
			CellID:                uint32(binary.LittleEndian.Uint16(tlv.Value[0:2])),
			MCC:                   mcc,
			MNC:                   mnc,
			LAC:                   binary.LittleEndian.Uint16(tlv.Value[5:7]),
			UARFCN:                binary.LittleEndian.Uint16(tlv.Value[7:9]),
			PrimaryScramblingCode: binary.LittleEndian.Uint16(tlv.Value[9:11]),
			RSCP:                  int16(binary.LittleEndian.Uint16(tlv.Value[11:13])),
			ECIO:                  int16(binary.LittleEndian.Uint16(tlv.Value[13:15])),
		}
	}

	if tlv := FindTLV(resp.TLVs, 0x13); tlv != nil && len(tlv.Value) >= 18 {
		mcc, mnc := decodeBCDPLMN(tlv.Value[1:4])
		lte := &LTECellLocationInfo{
			UEInIdle:                 tlv.Value[0] != 0,
			MCC:                      mcc,
			MNC:                      mnc,
			TAC:                      binary.LittleEndian.Uint16(tlv.Value[4:6]),
			GlobalCellID:             binary.LittleEndian.Uint32(tlv.Value[6:10]),
			EARFCN:                   binary.LittleEndian.Uint16(tlv.Value[10:12]),
			ServingCellID:            binary.LittleEndian.Uint16(tlv.Value[12:14]),
			CellReselectionPriority:  tlv.Value[14],
			SNonIntraSearchThreshold: tlv.Value[15],
			ServingCellLowThreshold:  tlv.Value[16],
			SIntraSearchThreshold:    tlv.Value[17],
		}
		if lte.UEInIdle {
			lte.HasIdleThresholds = true
		}
		info.LTE = lte
	}

	if tlv := FindTLV(resp.TLVs, 0x17); tlv != nil && len(tlv.Value) >= 4 {
		if info.UMTS == nil {
			info.UMTS = &UMTSCellLocationInfo{}
		}
		info.UMTS.CellID = binary.LittleEndian.Uint32(tlv.Value[0:4])
	}

	if tlv := FindTLV(resp.TLVs, 0x1E); tlv != nil && len(tlv.Value) >= 4 {
		if info.LTE == nil {
			info.LTE = &LTECellLocationInfo{}
		}
		timingAdvance := binary.LittleEndian.Uint32(tlv.Value[0:4])
		if timingAdvance != 0xFFFFFFFF {
			info.LTE.TimingAdvance = timingAdvance
			info.LTE.HasTimingAdvance = true
		}
	}

	if tlv := FindTLV(resp.TLVs, 0x2E); tlv != nil && len(tlv.Value) >= 4 {
		if info.NR5G == nil {
			info.NR5G = &NR5GCellLocationInfo{}
		}
		info.NR5G.ARFCN = binary.LittleEndian.Uint32(tlv.Value[0:4])
		info.NR5G.HasARFCN = true
	}

	if tlv := FindTLV(resp.TLVs, 0x2F); tlv != nil && len(tlv.Value) >= 20 {
		mcc, mnc := decodeBCDPLMN(tlv.Value[0:3])
		if info.NR5G == nil {
			info.NR5G = &NR5GCellLocationInfo{}
		}
		info.NR5G.MCC = mcc
		info.NR5G.MNC = mnc
		info.NR5G.TAC = decodeUint24(tlv.Value[3:6])
		info.NR5G.GlobalCellID = binary.LittleEndian.Uint64(tlv.Value[6:14])
		info.NR5G.PhysicalCellID = binary.LittleEndian.Uint16(tlv.Value[14:16])
		info.NR5G.RSRQ = int16(binary.LittleEndian.Uint16(tlv.Value[16:18]))
		info.NR5G.RSRP = int16(binary.LittleEndian.Uint16(tlv.Value[18:20]))
		if len(tlv.Value) >= 22 {
			info.NR5G.SNR = int16(binary.LittleEndian.Uint16(tlv.Value[20:22]))
		}
	}

	if info.GERAN == nil && info.UMTS == nil && info.LTE == nil && info.NR5G == nil {
		return nil, fmt.Errorf("no cell location TLVs in response")
	}

	return info, nil
}

func parseNetworkTimeResponse(resp *Packet) (*NetworkTimeInfo, error) {
	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("get network time failed: %w", err)
	}

	info := &NetworkTimeInfo{}
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil {
		value, err := parseNetworkTimeTLV(tlv)
		if err != nil {
			return nil, err
		}
		info.ThreeGPP2 = value
		info.HasThreeGPP2 = true
	}
	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil {
		value, err := parseNetworkTimeTLV(tlv)
		if err != nil {
			return nil, err
		}
		info.ThreeGPP = value
		info.HasThreeGPP = true
	}

	if !info.HasThreeGPP && !info.HasThreeGPP2 {
		return nil, fmt.Errorf("no network time TLV in response")
	}
	return info, nil
}

func parseNetworkTimeTLV(tlv *TLV) (NetworkTime, error) {
	if tlv == nil {
		return NetworkTime{}, fmt.Errorf("network time TLV is nil")
	}
	if len(tlv.Value) < 11 {
		return NetworkTime{}, fmt.Errorf("network time TLV too short: %d", len(tlv.Value))
	}
	return NetworkTime{
		Year:                      binary.LittleEndian.Uint16(tlv.Value[0:2]),
		Month:                     tlv.Value[2],
		Day:                       tlv.Value[3],
		Hour:                      tlv.Value[4],
		Minute:                    tlv.Value[5],
		Second:                    tlv.Value[6],
		DayOfWeek:                 tlv.Value[7],
		TimezoneOffsetQuarters:    int8(tlv.Value[8]),
		DaylightSavingsAdjustment: tlv.Value[9],
		RadioInterface:            tlv.Value[10],
	}, nil
}

func parseUint64Sequence(value []byte, count int) ([]uint64, error) {
	if len(value) < count*8 {
		return nil, fmt.Errorf("uint64 sequence too short: need %d, have %d", count*8, len(value))
	}
	out := make([]uint64, count)
	for i := 0; i < count; i++ {
		offset := i * 8
		out[i] = binary.LittleEndian.Uint64(value[offset : offset+8])
	}
	return out, nil
}

func newTLVUint64(t uint8, v uint64) TLV {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, v)
	return TLV{Type: t, Value: buf}
}

func newTLVUint64Sequence(t uint8, values []uint64) (TLV, error) {
	if len(values) == 0 {
		return TLV{}, fmt.Errorf("uint64 sequence cannot be empty")
	}
	buf := make([]byte, 0, len(values)*8)
	for _, v := range values {
		part := make([]byte, 8)
		binary.LittleEndian.PutUint64(part, v)
		buf = append(buf, part...)
	}
	return TLV{Type: t, Value: buf}, nil
}

func boolToUint8(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func decodeUint24(b []byte) uint32 {
	if len(b) < 3 {
		return 0
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

func decodeBCDPLMN(plmn []byte) (string, string) {
	if len(plmn) < 3 {
		return "", ""
	}

	mcc1 := plmn[0] & 0x0F
	mcc2 := (plmn[0] >> 4) & 0x0F
	mcc3 := plmn[1] & 0x0F
	mnc3 := (plmn[1] >> 4) & 0x0F
	mnc1 := plmn[2] & 0x0F
	mnc2 := (plmn[2] >> 4) & 0x0F

	mcc := fmt.Sprintf("%d%d%d", mcc1, mcc2, mcc3)
	if mnc3 == 0x0F {
		return mcc, fmt.Sprintf("%d%d", mnc1, mnc2)
	}
	return mcc, fmt.Sprintf("%d%d%d", mnc1, mnc2, mnc3)
}
