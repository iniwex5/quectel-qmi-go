package qmi

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
)

// ============================================================================
// ============================================================================
// WDS Runtime Settings TLV Types (from QCQMUX.h) / WDS运行时设置TLV类型 (来自QCQMUX.h)
// ============================================================================

const (
	TLVWDSPrimaryDNSv4   uint8 = 0x15
	TLVWDSSecondaryDNSv4 uint8 = 0x16
	TLVWDSIPv4Address    uint8 = 0x1E
	TLVWDSIPv4Gateway    uint8 = 0x20
	TLVWDSIPv4Subnet     uint8 = 0x21
	TLVWDSIPv6Address    uint8 = 0x25
	TLVWDSIPv6Gateway    uint8 = 0x26
	TLVWDSPrimaryDNSv6   uint8 = 0x27
	TLVWDSSecondaryDNSv6 uint8 = 0x28
	TLVWDSMtu            uint8 = 0x29
)

// Runtime settings mask bits / 运行时设置掩码位
const (
	RuntimeMaskProfileID   uint32 = 1 << 0
	RuntimeMaskProfileName uint32 = 1 << 1
	RuntimeMaskPDPType     uint32 = 1 << 2
	RuntimeMaskAPNName     uint32 = 1 << 3
	RuntimeMaskDNS         uint32 = 1 << 4
	RuntimeMaskQoS         uint32 = 1 << 5
	RuntimeMaskUsername    uint32 = 1 << 6
	RuntimeMaskAuth        uint32 = 1 << 7
	RuntimeMaskIPAddr      uint32 = 1 << 8
	RuntimeMaskGateway     uint32 = 1 << 9
	RuntimeMaskPCSCFPCO    uint32 = 1 << 10
	RuntimeMaskPCSCFAddr   uint32 = 1 << 11
	RuntimeMaskPCSCFDomain uint32 = 1 << 12
	RuntimeMaskMTU         uint32 = 1 << 13
	RuntimeMaskDomainName  uint32 = 1 << 14
	RuntimeMaskIPFamily    uint32 = 1 << 15
)

// ============================================================================
// ============================================================================
// WDS Service wrapper / WDS服务包装器
// ============================================================================

type WDSService struct {
	client   *Client
	clientID uint8
}

// NewWDSService creates a WDS service wrapper / NewWDSService创建一个WDS服务包装器
func NewWDSService(client *Client) (*WDSService, error) {
	clientID, err := client.AllocateClientID(ServiceWDS)
	if err != nil {
		return nil, err
	}
	return &WDSService{client: client, clientID: clientID}, nil
}

// Close releases the WDS client ID / Close释放WDS客户端ID
func (w *WDSService) Close() error {
	return w.client.ReleaseClientID(ServiceWDS, w.clientID)
}

// SetIPFamilyPreference sets the IP family preference (IPv4 or IPv6) / SetIPFamilyPreference设置IP族偏好 (IPv4或IPv6)
func (w *WDSService) SetIPFamilyPreference(ctx context.Context, ipFamily uint8) error {
	tlvs := []TLV{NewTLVUint8(0x01, ipFamily)}
	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSSetClientIPFamilyPref, tlvs)
	if err != nil {
		return err
	}
	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return fmt.Errorf("set IP family pref failed: 0x%04x", qmiErr)
	}
	return nil
}

// StartNetworkInterface initiates a data call / StartNetworkInterface发起数据呼叫
// Returns the handle needed to stop the call later / 返回稍后停止呼叫所需的句柄
func (w *WDSService) StartNetworkInterface(ctx context.Context, apn string, username string, password string, authType uint8, ipFamily uint8) (uint32, error) {
	// Set IP family first / 首先设置IP族
	if err := w.SetIPFamilyPreference(ctx, ipFamily); err != nil {
		// Non-fatal, continue / 非致命，继续
	}

	var tlvs []TLV

	// TLV 0x14: APN name / TLV 0x14: APN名称
	if apn != "" {
		tlvs = append(tlvs, NewTLVString(0x14, apn))
	}

	// TLV 0x17: Username / TLV 0x17: 用户名
	if username != "" {
		tlvs = append(tlvs, NewTLVString(0x17, username))
	}

	// TLV 0x18: Password / TLV 0x18: 密码
	if password != "" {
		tlvs = append(tlvs, NewTLVString(0x18, password))
	}

	// TLV 0x16: Authentication type (0=none, 1=PAP, 2=CHAP, 3=PAP|CHAP) / TLV 0x16: 认证类型
	if authType != 0 {
		tlvs = append(tlvs, NewTLVUint8(0x16, authType))
	}

	// TLV 0x19: IP family preference / TLV 0x19: IP族偏好
	tlvs = append(tlvs, NewTLVUint8(0x19, ipFamily))

	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSStartNetworkInterface, tlvs)
	if err != nil {
		return 0, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()

		// Try to get verbose error / 尝试获取详细错误信息
		verboseTLV := FindTLV(resp.TLVs, 0x11)
		if verboseTLV != nil && len(verboseTLV.Value) >= 4 {
			errType := binary.LittleEndian.Uint16(verboseTLV.Value[0:2])
			errCode := binary.LittleEndian.Uint16(verboseTLV.Value[2:4])
			return 0, fmt.Errorf("start network failed: QMI 0x%04x, call end type=%d code=%d", qmiErr, errType, errCode)
		}
		return 0, fmt.Errorf("start network failed: 0x%04x", qmiErr)
	}

	// Get handle from TLV 0x01 / 从TLV 0x01获取句柄
	handleTLV := FindTLV(resp.TLVs, 0x01)
	if handleTLV == nil || len(handleTLV.Value) < 4 {
		return 0, fmt.Errorf("no handle in response")
	}

	handle := binary.LittleEndian.Uint32(handleTLV.Value)
	return handle, nil
}

// StopNetworkInterface terminates a data call / StopNetworkInterface终止数据呼叫
func (w *WDSService) StopNetworkInterface(ctx context.Context, handle uint32) error {
	tlvs := []TLV{NewTLVUint32(0x01, handle)}

	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSStopNetworkInterface, tlvs)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return fmt.Errorf("stop network failed: 0x%04x", qmiErr)
	}

	return nil
}

// ConnectionStatus represents the current connection state / ConnectionStatus代表当前连接状态
type ConnectionStatus uint8

const (
	StatusUnknown        ConnectionStatus = 0
	StatusDisconnected   ConnectionStatus = 1
	StatusConnected      ConnectionStatus = 2
	StatusSuspended      ConnectionStatus = 3
	StatusAuthenticating ConnectionStatus = 4
)

func (s ConnectionStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "disconnected"
	case StatusConnected:
		return "connected"
	case StatusSuspended:
		return "suspended"
	case StatusAuthenticating:
		return "authenticating"
	default:
		return "unknown"
	}
}

// GetPacketServiceStatus queries the current connection status / GetPacketServiceStatus查询当前连接状态
func (w *WDSService) GetPacketServiceStatus(ctx context.Context) (ConnectionStatus, error) {
	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSGetPktSrvcStatus, nil)
	if err != nil {
		return StatusUnknown, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return StatusUnknown, fmt.Errorf("get status failed: 0x%04x", qmiErr)
	}

	// TLV 0x01: Connection status / TLV 0x01: 连接状态
	statusTLV := FindTLV(resp.TLVs, 0x01)
	if statusTLV == nil || len(statusTLV.Value) < 1 {
		return StatusUnknown, fmt.Errorf("no status TLV in response")
	}

	return ConnectionStatus(statusTLV.Value[0]), nil
}

// RuntimeSettings contains IP configuration from the network / RuntimeSettings包含来自网络的IP配置
type RuntimeSettings struct {
	IPv4Address net.IP
	IPv4Subnet  net.IPMask
	IPv4Gateway net.IP
	IPv4DNS1    net.IP
	IPv4DNS2    net.IP
	IPv6Address net.IP
	IPv6Prefix  int
	IPv6Gateway net.IP
	IPv6DNS1    net.IP
	IPv6DNS2    net.IP
	MTU         int
}

// GetRuntimeSettings retrieves IP configuration / GetRuntimeSettings检索IP配置
func (w *WDSService) GetRuntimeSettings(ctx context.Context, ipFamily uint8) (*RuntimeSettings, error) {
	// Set IP family first / 首先设置IP族
	w.SetIPFamilyPreference(ctx, ipFamily)

	// Request mask: IP, Gateway, DNS, MTU / 请求掩码: IP, 网关, DNS, MTU
	mask := RuntimeMaskIPAddr | RuntimeMaskGateway | RuntimeMaskDNS | RuntimeMaskMTU
	tlvs := []TLV{NewTLVUint32(0x10, mask)}

	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSGetRuntimeSettings, tlvs)
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return nil, fmt.Errorf("get runtime settings failed: 0x%04x", qmiErr)
	}

	settings := &RuntimeSettings{}

	// Parse IPv4 settings / 解析IPv4设置
	if tlv := FindTLV(resp.TLVs, TLVWDSIPv4Address); tlv != nil && len(tlv.Value) >= 4 {
		settings.IPv4Address = net.IPv4(tlv.Value[3], tlv.Value[2], tlv.Value[1], tlv.Value[0])
	}
	if tlv := FindTLV(resp.TLVs, TLVWDSIPv4Subnet); tlv != nil && len(tlv.Value) >= 4 {
		settings.IPv4Subnet = net.IPv4Mask(tlv.Value[3], tlv.Value[2], tlv.Value[1], tlv.Value[0])
	}
	if tlv := FindTLV(resp.TLVs, TLVWDSIPv4Gateway); tlv != nil && len(tlv.Value) >= 4 {
		settings.IPv4Gateway = net.IPv4(tlv.Value[3], tlv.Value[2], tlv.Value[1], tlv.Value[0])
	}
	if tlv := FindTLV(resp.TLVs, TLVWDSPrimaryDNSv4); tlv != nil && len(tlv.Value) >= 4 {
		settings.IPv4DNS1 = net.IPv4(tlv.Value[3], tlv.Value[2], tlv.Value[1], tlv.Value[0])
	}
	if tlv := FindTLV(resp.TLVs, TLVWDSSecondaryDNSv4); tlv != nil && len(tlv.Value) >= 4 {
		settings.IPv4DNS2 = net.IPv4(tlv.Value[3], tlv.Value[2], tlv.Value[1], tlv.Value[0])
	}

	// Parse IPv6 settings / 解析IPv6设置
	if tlv := FindTLV(resp.TLVs, TLVWDSIPv6Address); tlv != nil && len(tlv.Value) >= 17 {
		settings.IPv6Address = net.IP(tlv.Value[0:16])
		settings.IPv6Prefix = int(tlv.Value[16])
	}
	if tlv := FindTLV(resp.TLVs, TLVWDSIPv6Gateway); tlv != nil && len(tlv.Value) >= 16 {
		settings.IPv6Gateway = net.IP(tlv.Value[0:16])
	}
	if tlv := FindTLV(resp.TLVs, TLVWDSPrimaryDNSv6); tlv != nil && len(tlv.Value) >= 16 {
		settings.IPv6DNS1 = net.IP(tlv.Value[0:16])
	}
	if tlv := FindTLV(resp.TLVs, TLVWDSSecondaryDNSv6); tlv != nil && len(tlv.Value) >= 16 {
		settings.IPv6DNS2 = net.IP(tlv.Value[0:16])
	}

	// MTU
	if tlv := FindTLV(resp.TLVs, TLVWDSMtu); tlv != nil && len(tlv.Value) >= 4 {
		settings.MTU = int(binary.LittleEndian.Uint32(tlv.Value))
	}

	return settings, nil
}

// RegisterEventReport registers for WDS indications / RegisterEventReport注册WDS指示
func (w *WDSService) RegisterEventReport(ctx context.Context) error {
	tlvs := []TLV{
		// TLV 0x10: Report channel rate (1=enable) / TLV 0x10: 报告通道速率 (1=启用)
		NewTLVUint8(0x10, 0x01),
		// TLV 0x12: Report data bearer (1=enable) / TLV 0x12: 报告数据承载 (1=启用)
		NewTLVUint8(0x12, 0x01),
		// TLV 0x13: Report dormancy (1=enable) / TLV 0x13: 报告休眠状态 (1=启用)
		NewTLVUint8(0x13, 0x01),
	}

	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSSetEventReport, tlvs)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return fmt.Errorf("register event report failed: 0x%04x", qmiErr)
	}

	return nil
}
