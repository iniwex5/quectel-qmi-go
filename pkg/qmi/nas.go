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
	default:
		return "unknown"
	}
}

// ============================================================================
// NAS Service wrapper / NAS服务包装器
// ============================================================================

const (
	NASGetRFBandInfo uint16 = 0x0031
	NASGetSignalInfo uint16 = 0x004F
	/* Defined in frame.go / 在 frame.go 中定义
	NASGetSysInfo         uint16 = 0x004D
	*/
	NASPerformNetworkScan uint16 = 0x0021
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

	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("get serving system failed: %w", err)
	}

	ss := &ServingSystem{}

	// TLV 0x01: Serving system / TLV 0x01: 服务系统
	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 3 {
		ss.RegistrationState = RegistrationState(tlv.Value[0])
		// Value[1] = CS attach state, Value[2] = PS attach state / Value[1] = CS附着状态, Value[2] = PS附着状态
		ss.PSAttached = tlv.Value[2] == 1

		// Value[3] = Selected Network, Value[4] = Radio Interfaces Length
		if len(tlv.Value) >= 6 {
			numIfaces := int(tlv.Value[4])
			if numIfaces > 0 && len(tlv.Value) >= 5+numIfaces {
				ss.RadioInterface = tlv.Value[5] // 取首个激活的空口制式
			}
		}
	}

	// TLV 0x12: Current PLMN / TLV 0x12: 当前PLMN
	if tlv := FindTLV(resp.TLVs, 0x12); tlv != nil && len(tlv.Value) >= 4 {
		ss.MCC = binary.LittleEndian.Uint16(tlv.Value[0:2])
		ss.MNC = binary.LittleEndian.Uint16(tlv.Value[2:4])
	}

	return ss, nil
}

// IsRegistered checks if we're registered on the network / IsRegistered检查我们是否已在网络上注册
func (n *NASService) IsRegistered(ctx context.Context) (bool, error) {
	ss, err := n.GetServingSystem(ctx)
	if err != nil {
		return false, err
	}
	return ss.RegistrationState == RegStateRegistered && ss.PSAttached, nil
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

	// TLV 0x01: Signal strength (RSSI) / TLV 0x01: 信号强度(RSSI)
	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 1 {
		sig.RSSI = int8(tlv.Value[0])
	}

	// TLV 0x16: RSRQ (Sequence: int8 RSRQ, int8 Radio Interface)
	if tlv := FindTLV(resp.TLVs, 0x16); tlv != nil && len(tlv.Value) >= 1 {
		sig.RSRQ = int8(tlv.Value[0])
	}

	// TLV 0x17: LTE SNR (int16)
	if tlv := FindTLV(resp.TLVs, 0x17); tlv != nil && len(tlv.Value) >= 2 {
		sig.SNR = int16(binary.LittleEndian.Uint16(tlv.Value))
	}

	// TLV 0x18: LTE RSRP (int16)
	if tlv := FindTLV(resp.TLVs, 0x18); tlv != nil && len(tlv.Value) >= 2 {
		sig.RSRP = int16(binary.LittleEndian.Uint16(tlv.Value))
	}

	return sig, nil
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

	// TLV 0x14: LTE Signal Info / LTE 信号信息
	// 偏移结构根据 libqmi NAS: RSSI(int8), RSRQ(int8), RSRP(int16), SNR(int16)
	if tlv := FindTLV(resp.TLVs, 0x14); tlv != nil && len(tlv.Value) >= 6 {
		info.LTERSRQ = int16(int8(tlv.Value[1]))
		info.LTERSRP = int16(binary.LittleEndian.Uint16(tlv.Value[2:4]))
		info.LTERSSNR = int16(binary.LittleEndian.Uint16(tlv.Value[4:6]))
	}

	// TLV 0x17: 5G Signal Info (Simplified) / 5G 信号信息 (简化)
	if tlv := FindTLV(resp.TLVs, 0x17); tlv != nil && len(tlv.Value) >= 6 {
		// Assuming similar structure for demo purposes, real structure is more complex
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

	info := &SysInfo{}

	if tlv := FindTLV(resp.TLVs, 0x19); tlv != nil && len(tlv.Value) >= 16 {
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
	// TLV 0x10: Network Information
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

// ============================================================================
// Internal helpers / 内部助手
