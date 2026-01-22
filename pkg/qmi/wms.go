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

type MessageMode uint8

const (
	MessageModeCDMA MessageMode = 0x00
	MessageModeGW   MessageMode = 0x01
)

const (
	/* Defined in frame.go / 在 frame.go 中定义
	WMSDelete         uint16 = 0x0024
	*/
	WMSModifyTag      uint16 = 0x0023
	WMSGetSMSCAddress uint16 = 0x0034
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

	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("list messages failed: %w", err)
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

func (w *WMSService) ListMessagesAuto(ctx context.Context, storageType uint8) ([]struct {
	Index uint32
	Tag   MessageTagType
}, error) {
	try := func(tlvs []TLV) ([]struct {
		Index uint32
		Tag   MessageTagType
	}, error) {
		resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSListMessages, tlvs)
		if err != nil {
			return nil, err
		}
		if err := resp.CheckResult(); err != nil {
			return nil, fmt.Errorf("list messages failed: %w", err)
		}

		listTLV := FindTLV(resp.TLVs, 0x01)
		if listTLV == nil || len(listTLV.Value) < 4 {
			return nil, nil
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

	storage := TLV{Type: 0x01, Value: []byte{storageType}}
	mode := TLV{Type: 0x10, Value: []byte{uint8(MessageModeGW)}}

	attempts := [][]TLV{
		{storage, {Type: 0x11, Value: []byte{uint8(TagTypeMTNotRead)}}},
		{storage, {Type: 0x11, Value: []byte{uint8(TagTypeMTNotRead)}}, mode},
		{storage, {Type: 0x02, Value: []byte{uint8(TagTypeMTNotRead)}}},
		{storage, {Type: 0x02, Value: []byte{uint8(TagTypeMTNotRead)}}, mode},
		{storage, {Type: 0x11, Value: []byte{uint8(TagTypeMTRead)}}},
		{storage, {Type: 0x11, Value: []byte{uint8(TagTypeMTRead)}}, mode},
	}

	var lastErr error
	for _, tlvs := range attempts {
		msgs, err := try(tlvs)
		if err != nil {
			lastErr = err
			continue
		}
		return msgs, nil
	}
	return nil, lastErr
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

	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("read message failed: %w", err)
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
		// 如果无法根据长度启发式判断，尝试直接使用前 3 个字节之后的全部内容作为 fallback
		// 某些固件可能返回的长度字段不符合标准格式，或者包含了额外的填充
		if len(val) > 3 {
			// 假设格式为 [Format(1)][Length(2)][Data(N)]
			// 并信任返回的 Length 字段，即使它与 buffer 总长度不完全匹配
			len1 := binary.LittleEndian.Uint16(val[1:3])
			if int(3+len1) <= len(val) {
				length = len1
				dataOffset = 3
			} else {
				// 长度字段指示的数据比实际 buffer 还大，这是异常情况
				// 但作为最后的尝试，返回剩余的所有数据
				dataOffset = 3
				length = uint16(len(val) - 3)
			}
		} else {
			return nil, fmt.Errorf("could not parse raw message TLV: buffer too small (len: %d)", len(val))
		}
	}

	return val[dataOffset : dataOffset+int(length)], nil
}

func (w *WMSService) RawReadMessageMeta(ctx context.Context, storageType uint8, index uint32) (MessageTagType, bool, []byte, error) {
	buf := make([]byte, 5)
	buf[0] = storageType
	binary.LittleEndian.PutUint32(buf[1:5], index)

	tlvs := []TLV{
		{Type: 0x01, Value: buf},
		{Type: 0x10, Value: []byte{0x01}},
	}

	resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSRawRead, tlvs)
	if err != nil {
		return 0, false, nil, err
	}

	if err := resp.CheckResult(); err != nil {
		return 0, false, nil, fmt.Errorf("read message failed: %w", err)
	}

	msgTLV := FindTLV(resp.TLVs, 0x01)
	if msgTLV == nil || len(msgTLV.Value) < 3 {
		return 0, false, nil, fmt.Errorf("response missing raw message TLV or too short")
	}

	val := msgTLV.Value
	if len(val) >= 4 {
		len2 := binary.LittleEndian.Uint16(val[2:4])
		if int(4+len2) <= len(val) {
			tag := MessageTagType(val[0])
			return tag, true, val[4 : 4+len2], nil
		}
	}

	len1 := binary.LittleEndian.Uint16(val[1:3])
	if int(3+len1) <= len(val) {
		return 0, false, val[3 : 3+len1], nil
	}

	return 0, false, val[3:], nil
}

func (w *WMSService) DeleteMessage(ctx context.Context, storageType uint8, index uint32) error {
	return w.DeleteMessageByIndex(ctx, storageType, index, MessageModeGW)
}

func (w *WMSService) DeleteMessageByIndex(ctx context.Context, storageType uint8, index uint32, mode MessageMode) error {
	attempts := [][]TLV{
		{NewTLVUint8(0x01, storageType), NewTLVUint32(0x10, index), NewTLVUint8(0x12, uint8(mode))},
		{NewTLVUint8(0x01, storageType), NewTLVUint32(0x02, index), NewTLVUint8(0x04, uint8(mode))},
	}

	var lastErr error
	for _, tlvs := range attempts {
		resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSDelete, tlvs)
		if err != nil {
			lastErr = err
			continue
		}
		if err := resp.CheckResult(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func (w *WMSService) DeleteMessagesByTag(ctx context.Context, storageType uint8, tag MessageTagType, mode MessageMode) error {
	attempts := [][]TLV{
		{NewTLVUint8(0x01, storageType), NewTLVUint8(0x11, uint8(tag)), NewTLVUint8(0x12, uint8(mode))},
		{NewTLVUint8(0x01, storageType), NewTLVUint8(0x03, uint8(tag)), NewTLVUint8(0x04, uint8(mode))},
	}

	var lastErr error
	for _, tlvs := range attempts {
		resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSDelete, tlvs)
		if err != nil {
			lastErr = err
			continue
		}
		if err := resp.CheckResult(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// ModifyMessageTag modifies the tag of a message (e.g. mark as read) / ModifyMessageTag 修改消息标签 (例如标记为已读)
func (w *WMSService) ModifyMessageTag(ctx context.Context, storageType uint8, index uint32, newTag MessageTagType) error {
	modeTLV := TLV{Type: 0x10, Value: []byte{0x01}}

	bufCombined := make([]byte, 6)
	bufCombined[0] = storageType
	binary.LittleEndian.PutUint32(bufCombined[1:5], index)
	bufCombined[5] = uint8(newTag)

	bufInfo := make([]byte, 5)
	bufInfo[0] = storageType
	binary.LittleEndian.PutUint32(bufInfo[1:5], index)

	attempts := [][]TLV{
		{{Type: 0x01, Value: bufCombined}, modeTLV},
		{{Type: 0x01, Value: bufCombined}},
		{NewTLVUint8(0x01, uint8(newTag)), {Type: 0x03, Value: bufInfo}, modeTLV},
		{NewTLVUint8(0x01, uint8(newTag)), {Type: 0x03, Value: bufInfo}},
		{NewTLVUint8(0x01, uint8(newTag)), NewTLVUint32(0x02, 0), {Type: 0x03, Value: bufInfo}, modeTLV},
	}

	var lastErr error
	for _, tlvs := range attempts {
		resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSModifyTag, tlvs)
		if err != nil {
			lastErr = err
			continue
		}
		if err := resp.CheckResult(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// GetSMSCAddress gets the SMS center address / GetSMSCAddress 获取短信中心地址
func (w *WMSService) GetSMSCAddress(ctx context.Context) (string, error) {
	resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSGetSMSCAddress, nil)
	if err != nil {
		return "", err
	}

	if err := resp.CheckResult(); err != nil {
		return "", err
	}

	// TLV 0x01: SMSC Address / 短信中心地址
	// Type(3 bytes for type/length) + Address...
	// Usually: [Type(3 chars string)] [Length(1)] [Digits...]
	// But QMI spec says:
	// string SMSCAddressType (max 3)
	// string SMSCAddress (max 20)
	// Let's look for TLV 0x01
	tlv := FindTLV(resp.TLVs, 0x01)
	if tlv == nil {
		return "", fmt.Errorf("SMSC address TLV not found")
	}

	// Parse as string for simplicity, though it might be binary encoded digits
	// Typically ASCII for type, and ASCII digits for address
	return string(tlv.Value), nil
}

// RegisterEventReport enables indications for new messages / RegisterEventReport 开启新消息指示
func (w *WMSService) RegisterEventReport(ctx context.Context) error {
	// TLV 0x10: New MT Message Indicator (1 = enable) / 新 MT 消息指示器 (1 = 启用)
	tlvs := []TLV{NewTLVUint8(0x10, 0x01)}

	resp, err := w.client.SendRequest(ctx, ServiceWMS, w.clientID, WMSSetEventReport, tlvs)
	if err != nil {
		return err
	}

	if err := resp.CheckResult(); err != nil {
		return fmt.Errorf("register event report failed: %w", err)
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

	if err := resp.CheckResult(); err != nil {
		return fmt.Errorf("send message failed: %w", err)
	}
	return nil
}
