package qmi

import (
	"context"
	"encoding/binary"
	"fmt"
)

// ============================================================================
// WMS Service Wrapper / WMS 服务封装
// ============================================================================

type WMSService struct {
	client   *Client
	clientID uint8
}

// NewWMSService creates a WMS service wrapper / NewWMSService 创建 WMS 服务封装
func NewWMSService(client *Client) (*WMSService, error) {
	clientID, err := client.AllocateClientID(ServiceWMS)
	if err != nil {
		return nil, err
	}
	return &WMSService{client: client, clientID: clientID}, nil
}

// Close releases the WMS client ID / Close 释放 WMS 客户端 ID
func (w *WMSService) Close() error {
	return w.client.ReleaseClientID(ServiceWMS, w.clientID)
}

// ============================================================================
// SMS Operations / 短信操作
// ============================================================================

// MessageTagType for listing messages / 用于列出短信的消息标签类型
type MessageTagType uint8

const (
	TagTypeMTRead    MessageTagType = 0x00 // MT: Mobile Terminated (Received) - Read / MT: 移动终端终结（接收）- 已读
	TagTypeMTNotRead MessageTagType = 0x01 // MT: Mobile Terminated (Received) - Not Read / MT: 移动终端终结（接收）- 未读
	TagTypeMOSent    MessageTagType = 0x02 // MO: Mobile Originated (Sent) - Sent / MO: 移动终端发起（发送）- 已发送
	TagTypeMONotSent MessageTagType = 0x03 // MO: Mobile Originated (Sent) - Not Sent / MO: 移动终端发起（发送）- 未发送
)

// ListMessages lists messages from memory storage / ListMessages 从内存存储中列出消息
// Returns a list of (index, tag) tuples / 返回（索引，标签）元组列表
func (w *WMSService) ListMessages(ctx context.Context, storageType uint8, tagType MessageTagType) ([]struct {
	Index uint32
	Tag   MessageTagType
}, error) {
	// TLV 0x01: Memory Storage Identification / 内存存储识别
	tlvs := []TLV{{Type: 0x01, Value: []byte{storageType}}}

	// TLV 0x11: Message Tag (Some modems require this in 0x11, others in 0x02) / 消息标签（部分调制解调器需要在0x11，其他在0x02）
	tlvs = append(tlvs, TLV{Type: 0x11, Value: []byte{uint8(tagType)}})

	resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSListMessages, tlvs)
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return nil, fmt.Errorf("list messages failed: 0x%04x", qmiErr)
	}

	// TLV 0x01: Message List Identification / 消息列表识别
	// Format: count(4), [index(4), tag(1)] * count / 格式：数量(4), [索引(4), 标签(1)] * 数量
	listTLV := FindTLV(resp.TLVs, 0x01)
	if listTLV == nil || len(listTLV.Value) < 4 {
		return nil, nil // No messages / 没有消息
	}

	count := binary.LittleEndian.Uint32(listTLV.Value[0:4])
	var result []struct {
		Index uint32
		Tag   MessageTagType
	}

	offset := 4
	for i := uint32(0); i < count; i++ {
		if offset+5 > len(listTLV.Value) {
			break
		}
		idx := binary.LittleEndian.Uint32(listTLV.Value[offset : offset+4])
		tag := MessageTagType(listTLV.Value[offset+4])
		result = append(result, struct {
			Index uint32
			Tag   MessageTagType
		}{Index: idx, Tag: tag})
		offset += 5
	}

	return result, nil
}

// RawReadMessage reads a raw SMS PDU / RawReadMessage 读取原始短信 PDU
func (w *WMSService) RawReadMessage(ctx context.Context, storageType uint8, index uint32) ([]byte, error) {
	// TLV 0x01: Memory Storage Identification / 内存存储识别
	buf := make([]byte, 5)
	buf[0] = storageType
	binary.LittleEndian.PutUint32(buf[1:5], index)

	tlvs := []TLV{
		{Type: 0x01, Value: buf},
		{Type: 0x10, Value: []byte{0x01}}, // Message Mode: GW (0x01) / 消息模式：GW (0x01)
	}

	resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSRawRead, tlvs)
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return nil, fmt.Errorf("read message failed: 0x%04x", qmiErr)
	}

	// TLV 0x01: Raw Message Identification / 原始消息识别
	// Variations:
	// 1. [Format(1)], [Length(2)], [Data(N)]
	// 2. [Tag(1)], [Format(1)], [Length(2)], [Data(N)]
	msgTLV := FindTLV(resp.TLVs, 0x01)
	if msgTLV == nil || len(msgTLV.Value) < 3 {
		return nil, fmt.Errorf("response missing raw message TLV or too short")
	}

	val := msgTLV.Value
	var length uint16
	var dataOffset int

	// Simple heuristic: check if length at [1:3] makes sense for the buffer size
	len1 := binary.LittleEndian.Uint16(val[1:3])
	if int(3+len1) == len(val) {
		// Format: [Format(1)][Length(2)][Data(N)]
		length = len1
		dataOffset = 3
	} else if len(val) >= 4 {
		len2 := binary.LittleEndian.Uint16(val[2:4])
		if int(4+len2) == len(val) {
			// Format: [Tag(1)][Format(1)][Length(2)][Data(N)]
			length = len2
			dataOffset = 4
		}
	}

	if dataOffset == 0 {
		return nil, fmt.Errorf("could not parse raw message TLV: length mismatch (total len: %d)", len(val))
	}

	return val[dataOffset : dataOffset+int(length)], nil
}

// RegisterEventReport enables indications for new messages / RegisterEventReport 开启新消息指示
func (w *WMSService) RegisterEventReport(ctx context.Context) error {
	// TLV 0x10: New MT Message Indicator (1 = enable) / 新 MT 消息指示器 (1 = 启用)
	tlvs := []TLV{NewTLVUint8(0x10, 0x01)}

	resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSSetEventReport, tlvs)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return fmt.Errorf("register event report failed: 0x%04x", qmiErr)
	}
	return nil
}

// SendRawMessage sends a raw PDU / SendRawMessage 发送原始 PDU
// format: 0x06 (GSM/WCDMA), 0x00 (CDMA) / 格式：0x06 (GSM/WCDMA), 0x00 (CDMA)
func (w *WMSService) SendRawMessage(ctx context.Context, format uint8, pdu []byte) error {
	// TLV 0x01: Raw Message Write / 原始消息写入
	// Format: format(1), length(2), data(...) / 格式：格式(1)，长度(2)，数据(...)
	buf := make([]byte, 3+len(pdu))
	buf[0] = format
	binary.LittleEndian.PutUint16(buf[1:3], uint16(len(pdu)))
	copy(buf[3:], pdu)

	tlvs := []TLV{{Type: 0x01, Value: buf}}

	resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSRawSend, tlvs)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return fmt.Errorf("send message failed: 0x%04x", qmiErr)
	}
	return nil
}
