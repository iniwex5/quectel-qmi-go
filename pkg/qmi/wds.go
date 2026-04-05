package qmi

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
)

const (
	WDSModifyProfileSettings uint16 = 0x0027
	WDSCreateProfile         uint16 = 0x0028
	/* Defined in frame.go / 在 frame.go 中定义
	WDSGetProfileSettings     uint16 = 0x002B
	WDSGetProfileList         uint16 = 0x002A
	WDSBindMuxDataPort        uint16 = 0x00A2
	*/
)

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
// WDS Service wrapper / WDS服务包装器
// ============================================================================

type WDSService struct {
	client               *Client
	clientID             uint8
	ProfileIndex         uint8
	TechnologyPreference uint16 // Bitmask: 0x8000=3GPP, 0x4000=3GPP2
}

type OutOfCallError struct {
	Operation string
}

func (e *OutOfCallError) Error() string {
	if e.Operation == "" {
		return "out of call"
	}
	return e.Operation + ": out of call"
}

type CallEndReason struct {
	Type uint16
	Code uint16
}

type StartNetworkError struct {
	Err    error
	Reason *CallEndReason
}

func (e *StartNetworkError) Error() string {
	if e.Err == nil {
		if e.Reason == nil {
			return "start network failed"
		}
		return fmt.Sprintf("start network failed, call end type=%d code=%d", e.Reason.Type, e.Reason.Code)
	}
	if e.Reason == nil {
		return fmt.Sprintf("start network failed: %v", e.Err)
	}
	return fmt.Sprintf("start network failed: %v, call end type=%d code=%d", e.Err, e.Reason.Type, e.Reason.Code)
}

func (e *StartNetworkError) Unwrap() error {
	return e.Err
}

// MuxBinding info for QMAP / QMAP 的 Mux 绑定信息
type MuxBinding struct {
	EpType     uint32 // Endpoint Type (e.g., 0x02 for HSUSB)
	EpIfID     uint32 // Interface ID (e.g., 4 for iface 4)
	MuxID      uint8  // QMAP Mux ID
	ClientType uint32 // Client Type (e.g., 1 for Tethered)
}

// ProfileInfo represents minimal profile information / ProfileInfo 代表最小化的 Profile 信息
type ProfileInfo struct {
	Type  uint8 // 0: 3GPP, 1: 3GPP2
	Index uint8
	Name  string
}

// WDSService implements the QMI Wireless Data Service

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

func (w *WDSService) ClientID() uint8 {
	return w.clientID
}

// SetIPFamilyPreference sets the IP family preference (IPv4 or IPv6) / SetIPFamilyPreference设置IP族偏好 (IPv4或IPv6)
func (w *WDSService) SetIPFamilyPreference(ctx context.Context, ipFamily uint8) error {
	tlvs := []TLV{NewTLVUint8(0x01, ipFamily)}
	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSSetClientIPFamilyPref, tlvs)
	if err != nil {
		return err
	}
	if err := resp.CheckResult(); err != nil {
		return fmt.Errorf("set IP family pref failed: %w", err)
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

	// TLV 0x30: Profile Index / Profile 索引 (Optional)
	if w.ProfileIndex > 0 {
		tlvs = append(tlvs, NewTLVUint8(0x30, w.ProfileIndex))
	}

	// TLV 0x34: Technology Preference / 技术偏好 (Optional)
	if w.TechnologyPreference > 0 {
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, w.TechnologyPreference)
		tlvs = append(tlvs, TLV{Type: 0x34, Value: buf})
	}

	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSStartNetworkInterface, tlvs)
	if err != nil {
		return 0, err
	}

	if err := resp.CheckResult(); err != nil {
		var reason *CallEndReason
		if verboseTLV := FindTLV(resp.TLVs, 0x11); verboseTLV != nil && len(verboseTLV.Value) >= 4 {
			reason = &CallEndReason{
				Type: binary.LittleEndian.Uint16(verboseTLV.Value[0:2]),
				Code: binary.LittleEndian.Uint16(verboseTLV.Value[2:4]),
			}
		}
		return 0, &StartNetworkError{Err: err, Reason: reason}
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

	if err := resp.CheckResult(); err != nil {
		return fmt.Errorf("stop network failed: %w", err)
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

	if err := resp.CheckResult(); err != nil {
		return StatusUnknown, fmt.Errorf("get status failed: %w", err)
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
	if err := w.SetIPFamilyPreference(ctx, ipFamily); err != nil {
		return nil, err
	}

	// Request mask: IP, Gateway, DNS, MTU / 请求掩码: IP, 网关, DNS, MTU
	mask := RuntimeMaskIPAddr | RuntimeMaskGateway | RuntimeMaskDNS | RuntimeMaskMTU
	tlvs := []TLV{NewTLVUint32(0x10, mask)}

	resp, err := w.client.SendRequest(ctx, ServiceWDS, w.clientID, WDSGetRuntimeSettings, tlvs)
	if err != nil {
		return nil, err
	}

	if err := resp.CheckResult(); err != nil {
		if qe := GetQMIError(err); qe != nil && qe.ErrorCode == QMIErrOutOfCall {
			return nil, &OutOfCallError{Operation: "get runtime settings"}
		}
		return nil, fmt.Errorf("get runtime settings failed: %w", err)
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

	if err := resp.CheckResult(); err != nil {
		return fmt.Errorf("register event report failed: %w", err)
	}
	return nil
}

// BindMuxDataPort binds the WDS client to a specific Mux ID (for QMAP) / BindMuxDataPort 将 WDS 客户端绑定到特定的 Mux ID (用于 QMAP)
func (s *WDSService) BindMuxDataPort(ctx context.Context, binding MuxBinding) error {
	var tlvs []TLV

	// TLV 0x10: Endpoint Info / 端点信息
	// EpType (4) + EpIfID (4)
	bufEp := make([]byte, 8)
	binary.LittleEndian.PutUint32(bufEp[0:4], binding.EpType)
	binary.LittleEndian.PutUint32(bufEp[4:8], binding.EpIfID)
	tlvs = append(tlvs, TLV{Type: 0x10, Value: bufEp})

	// TLV 0x11: Mux ID / Mux ID
	bufMux := make([]byte, 1)
	bufMux[0] = binding.MuxID
	tlvs = append(tlvs, TLV{Type: 0x11, Value: bufMux})

	// TLV 0x13: Client Type / 客户端类型 (Optional but recommended)
	if binding.ClientType > 0 {
		bufClient := make([]byte, 4)
		binary.LittleEndian.PutUint32(bufClient, binding.ClientType)
		tlvs = append(tlvs, TLV{Type: 0x13, Value: bufClient})
	}

	resp, err := s.client.SendRequest(ctx, ServiceWDS, s.clientID, WDSBindMuxDataPort, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

// GetProfileList retrieves the list of profiles / GetProfileList 获取 Profile 列表
func (s *WDSService) GetProfileList(ctx context.Context, profileType uint8) ([]ProfileInfo, error) {
	attempts := [][]TLV{
		nil,
		{NewTLVUint8(0x11, profileType)},
		{NewTLVUint8(0x01, profileType)},
	}

	var lastErr error
	for _, tlvs := range attempts {
		resp, err := s.client.SendRequest(ctx, ServiceWDS, s.clientID, WDSGetProfileList, tlvs)
		if err != nil {
			lastErr = err
			continue
		}
		if err := resp.CheckResult(); err != nil {
			lastErr = err
			continue
		}

		if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 1 {
			count := int(tlv.Value[0])
			profiles := make([]ProfileInfo, 0, count)

			if len(tlv.Value) >= 1+count*3 {
				offset := 1
				for i := 0; i < count; i++ {
					if offset+3 > len(tlv.Value) {
						break
					}
					pType := tlv.Value[offset]
					pIndex := tlv.Value[offset+1]
					profiles = append(profiles, ProfileInfo{Type: pType, Index: pIndex})
					offset += 3
				}
				return profiles, nil
			}

			if len(tlv.Value) >= 1+count*2 {
				offset := 1
				for i := 0; i < count; i++ {
					if offset+2 > len(tlv.Value) {
						break
					}
					pType := tlv.Value[offset]
					pIndex := tlv.Value[offset+1]
					profiles = append(profiles, ProfileInfo{Type: pType, Index: pIndex})
					offset += 2
				}
				return profiles, nil
			}

			return profiles, nil
		}

		if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 1 {
			count := int(tlv.Value[0])
			offset := 1
			profiles := make([]ProfileInfo, 0, count)
			for i := 0; i < count && offset < len(tlv.Value); i++ {
				if offset+3 > len(tlv.Value) {
					break
				}
				pType := tlv.Value[offset]
				pIndex := tlv.Value[offset+1]
				pNameLen := int(tlv.Value[offset+2])
				offset += 3

				pName := ""
				if offset+pNameLen <= len(tlv.Value) {
					pName = string(tlv.Value[offset : offset+pNameLen])
					offset += pNameLen
				} else {
					// 防止出现半截断数据导致后续遍历全乱，直接截断退出
					break
				}

				profiles = append(profiles, ProfileInfo{
					Type:  pType,
					Index: pIndex,
					Name:  pName,
				})
			}
			return profiles, nil
		}

		return nil, nil
	}
	return nil, lastErr
}

// GetProfileSettings retrieves settings for a specific profile / GetProfileSettings 获取特定 Profile 的设置
// Note: This returns raw TLVs or a map as profile structure is very complex
// simplified here to just return "success" if it exists for now, or implement basic APN reading
func (s *WDSService) GetProfileSettings(ctx context.Context, profileType, profileIndex uint8) (string, error) {
	bufId := make([]byte, 2)
	bufId[0] = profileType
	bufId[1] = profileIndex

	attempts := [][]TLV{
		{{Type: 0x01, Value: bufId}},
		{{Type: 0x10, Value: bufId}},
	}

	var lastErr error
	for _, tlvs := range attempts {
		resp, err := s.client.SendRequest(ctx, ServiceWDS, s.clientID, WDSGetProfileSettings, tlvs)
		if err != nil {
			lastErr = err
			continue
		}

		if err := resp.CheckResult(); err != nil {
			lastErr = err
			continue
		}

		if tlv := FindTLV(resp.TLVs, 0x14); tlv != nil {
			return string(tlv.Value), nil
		}

		return "", nil
	}
	return "", lastErr
}
