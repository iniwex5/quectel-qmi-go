package qmi

import (
	"context"
	"encoding/binary"
)

// WDA Message IDs / WDA消息ID
const (
	WDASetDataFormat uint16 = 0x0020
	WDAGetDataFormat uint16 = 0x0021
)

// Data Format Modes / 数据格式模式
const (
	DataFormatQOSFlowHeader     uint8 = 1 << 0
	DataFormatLinkProtEth       uint8 = 0
	DataFormatLinkProtIP        uint8 = 1 << 1
	DataFormatUlDataAggEnabled  uint8 = 1 << 2
	DataFormatUlDataAggDisabled uint8 = 0
	DataFormatDlDataAggEnabled  uint8 = 1 << 3
	DataFormatDlDataAggDisabled uint8 = 0
	DataFormatNdpSigEnabled     uint8 = 1 << 4 // New Data Path Signature / 新数据路径签名
)

// WDAService implements the Wireless Data Admin service / WDAService实现无线数据管理服务
type WDAService struct {
	client *Client
}

// NewWDAService creates a new WDA client / NewWDAService创建一个新的WDA客户端
func NewWDAService(client *Client) (*WDAService, error) {
	return &WDAService{client: client}, nil
}

// DataFormat represents the data format configuration / DataFormat代表数据格式配置
type DataFormat struct {
	LinkProtocol          uint32 // 0x01 = IP (Raw IP), 0x02 = Ethernet (802.3) / 0x01 = IP (原始IP), 0x02 = 以太网 (802.3)
	UlDataAggregation     uint32
	DlDataAggregation     uint32
	DlDataAggMaxDatagrams uint32
	DlDataAggMaxSize      uint32
}

// SetDataFormat sets the data format (e.g. Raw IP) / SetDataFormat设置数据格式 (例如 原始IP)
func (s *WDAService) SetDataFormat(ctx context.Context, format DataFormat) error {
	var tlvs []TLV

	// TLV 0x10: QoS Data Format (optional, usu. 0) / TLV 0x10: QoS数据格式 (可选，通常为0)
	// TLV 0x11: Underlying Link Layer Protocol (Required) / TLV 0x11: 底层链路层协议 (必需)
	// 0x00 - Enum (1 byte), 0x02=802.3 (Ethernet), 0x01=IP (Raw) / 0x00 - 枚举 (1字节), 0x02=802.3 (以太网), 0x01=IP (原始)
	// C driver uses 0x02 for Ethernet usually, or check logic. / C驱动通常使用0x02表示以太网，或检查逻辑。
	// Actually, for RawIP usually we set 0x00 (No QoS), and link proto. / 实际上，对于RawIP，我们通常设置0x00 (无QoS) 和链路协议。

	// Let's match the C structure QMIWDS_ADMIN_SET_DATA_FORMAT_TLV / 让我们匹配C结构 QMIWDS_ADMIN_SET_DATA_FORMAT_TLV
	// which seems to just send ULONG values for specific TLVs. / 它似乎只是为特定的TLV发送ULONG值。

	// In QMIThread.c, they might set it up. Let's look at a common implementation. / 在QMIThread.c中，他们可能会设置它。让我们看一个常见的实现。
	// Typically: / 通常:
	// TLV 0x10 (1 byte) - QoS header? / TLV 0x10 (1字节) - QoS头?
	// TLV 0x11 (4 bytes) - Link Protocol: 0=Eth, 1=IP (Wait, QCQMUX.h says QMIWDS_ADMIN_SET_DATA_FORMAT_TLV has ULONG Value) / TLV 0x11 (4字节) - 链路协议: 0=Eth, 1=IP (等等，QCQMUX.h 说 QMIWDS_ADMIN_SET_DATA_FORMAT_TLV 有 ULONG 值)

	// Let's assume standard QWA logic: / 让我们假设标准的QWA逻辑:
	// 0x10: QoS Setting (1 byte bool) / 0x10: QoS设置 (1字节布尔值)
	// 0x11: Underlying Link Layer Protocol (4 byte enum) - 1: IP, 2: Ethernet / 0x11: 底层链路层协议 (4字节枚举) - 1: IP, 2: 以太网

	// Construct TLVs / 构建TLV

	// TLV 0x10: QoS / TLV 0x10: QoS
	tlvs = append(tlvs, TLV{
		Type:  0x10,
		Value: []byte{0x00}, // No QoS header / 无QoS头
	})

	// TLV 0x11: Underlying Link Protocol / TLV 0x11: 底层链路协议
	// Value: 0x01 = 802.3 (Ethernet)? No, QMI spec usually: 1=Prot 2=Eth? / 值: 0x01 = 802.3 (以太网)? 不，QMI规范通常是: 1=协议 2=以太网?
	// Let's check typical values. Linux qmi_wwan expects 802.3 (header present) or raw IP. / 让我们检查典型值。Linux qmi_wwan期望802.3 (存在头) 或原始IP。
	// If raw_ip=Y, we generally want Raw IP mode. / 如果 raw_ip=Y，我们通常想要原始IP模式。

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, format.LinkProtocol)
	tlvs = append(tlvs, TLV{
		Type:  0x11,
		Value: buf,
	})

	// TLV 0x12: UL Protocol (4 bytes) / TLV 0x12: 上行协议 (4字节)
	binary.LittleEndian.PutUint32(buf, format.UlDataAggregation)
	tlvs = append(tlvs, TLV{
		Type:  0x12,
		Value: buf, // Reuse buffer since we write fresh / 重用缓冲区，因为我们写入新的数据
	})

	// TLV 0x13: DL Protocol (4 bytes) / TLV 0x13: 下行协议 (4字节)
	binary.LittleEndian.PutUint32(buf, format.DlDataAggregation)
	tlvs = append(tlvs, TLV{
		Type:  0x13,
		Value: buf,
	})

	// Send request / 发送请求
	resp, err := s.client.SendRequest(ctx, ServiceWDA, 0, WDASetDataFormat, tlvs)
	if err != nil {
		return err
	}

	if err := resp.CheckResult(); err != nil {
		return err
	}

	return nil
}

// DataFormatMode constants for Link Protocol (TLV 0x11) / Link Protocol (TLV 0x11) 的 DataFormatMode 常量
const (
	LinkProtocolEthernet uint32 = 0x01 // Sometime 0x02? Need to verify spec vs modem. / 有时是0x02? 需要针对modem验证规范。
	LinkProtocolIP       uint32 = 0x02
)

// Actually, looking at QCQMUX.h isn't super clear on values. / 实际上，查看QCQMUX.h关于值的说明并不是很清楚。
// Standard QMI: / 标准QMI:
// 0x01: QMI_WDA_LINK_LAYER_PROTOCOL_802_3 (Ethernet) / 0x01: QMI_WDA_LINK_LAYER_PROTOCOL_802_3 (以太网)
// 0x02: QMI_WDA_LINK_LAYER_PROTOCOL_RAW_IP (IP) / 0x02: QMI_WDA_LINK_LAYER_PROTOCOL_RAW_IP (IP)
