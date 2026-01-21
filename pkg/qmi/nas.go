package qmi

import (
	"context"
	"fmt"
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

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return nil, fmt.Errorf("get serving system failed: 0x%04x", qmiErr)
	}

	ss := &ServingSystem{}

	// TLV 0x01: Serving system / TLV 0x01: 服务系统
	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 3 {
		ss.RegistrationState = RegistrationState(tlv.Value[0])
		// Value[1] = CS attach state, Value[2] = PS attach state / Value[1] = CS附着状态, Value[2] = PS附着状态
		ss.PSAttached = tlv.Value[2] == 1
	}

	// TLV 0x10: Radio interface / TLV 0x10: 无线接口
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 1 {
		ss.RadioInterface = tlv.Value[0]
	}

	// TLV 0x12: Current PLMN / TLV 0x12: 当前PLMN
	if tlv := FindTLV(resp.TLVs, 0x12); tlv != nil && len(tlv.Value) >= 5 {
		ss.MCC = uint16(tlv.Value[0])<<8 | uint16(tlv.Value[1])
		ss.MNC = uint16(tlv.Value[2])<<8 | uint16(tlv.Value[3])
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

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return nil, fmt.Errorf("get signal strength failed: 0x%04x", qmiErr)
	}

	sig := &SignalStrength{}

	// TLV 0x01: Signal strength / TLV 0x01: 信号强度
	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 2 {
		sig.RSSI = int8(tlv.Value[0])
	}

	// TLV 0x11: RSRQ / TLV 0x11: RSRQ
	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil && len(tlv.Value) >= 1 {
		sig.RSRQ = int8(tlv.Value[0])
	}

	// TLV 0x16: RSRP / TLV 0x16: RSRP
	if tlv := FindTLV(resp.TLVs, 0x16); tlv != nil && len(tlv.Value) >= 2 {
		sig.RSRP = int16(int8(tlv.Value[0]))
	}

	return sig, nil
}

// RegisterIndications enables NAS unsolicited indications / RegisterIndications启用NAS主动指示
func (n *NASService) RegisterIndications() error {
	// For most modems, indications are enabled by default after connecting / 对于大多数modem，连接后默认启用指示
	// Some may require explicit registration via NAS_INDICATION_REGISTER (0x0003) / 某些可能需要通过 NAS_INDICATION_REGISTER (0x0003) 显式注册
	return nil
}
