package qmi

import (
	"context"
	"encoding/binary"
	"fmt"
)

const (
	// UIM Message IDs / UIM消息ID
	UIMReadTransparent uint16 = 0x0020
	/* Defined in frame.go / 在 frame.go 中定义
	UIMVerifyPIN            uint16 = 0x0026
	*/
	UIMSetPINProtection uint16 = 0x0027
	UIMChangePIN        uint16 = 0x0028
	UIMUnblockPIN       uint16 = 0x0029
	/* Defined in frame.go / 在 frame.go 中定义
	UIMGetCardStatus        uint16 = 0x002F
	*/
)

// CardStatus represents the SIM card status / CardStatus代表SIM卡状态

// ============================================================================
// UIM Service wrapper / UIM服务包装器
// ============================================================================

type UIMService struct {
	client   *Client
	clientID uint8
}

type CardStatusDetails struct {
	CardState           uint8
	ErrorCode           uint8
	NumSlot             uint8
	NumApp              uint8
	AppType             uint8
	AppState            uint8
	PersoState          uint8
	PersoFeature        uint8
	PersoRetries        uint8
	PersoUnblockRetries uint8
	AID                 []byte
	PIN1State           PINStatus
	PIN1Retries         uint8
	PUK1Retries         uint8
	PIN2State           PINStatus
	PIN2Retries         uint8
	PUK2Retries         uint8
	UsesUPIN            bool
	UPINState           PINStatus
	UPINRetries         uint8
	UPUKRetries         uint8
}

type QMIUIM_PIN_STATE struct {
	UnivPIN     uint8
	PIN1State   uint8
	PIN1Retries uint8
	PUK1Retries uint8
	PIN2State   uint8
	PIN2Retries uint8
	PUK2Retries uint8
}

// NewUIMService creates a UIM service wrapper / NewUIMService创建一个UIM服务包装器
func NewUIMService(client *Client) (*UIMService, error) {
	clientID, err := client.AllocateClientID(ServiceUIM)
	if err != nil {
		return nil, err
	}
	return &UIMService{client: client, clientID: clientID}, nil
}

// Close releases the UIM client ID / Close释放UIM客户端ID
func (u *UIMService) Close() error {
	return u.client.ReleaseClientID(ServiceUIM, u.clientID)
}

func (u *UIMService) GetCardStatusDetails(ctx context.Context) (*CardStatusDetails, SIMStatus, error) {
	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMGetCardStatus, nil)
	if err != nil {
		return nil, SIMAbsent, err
	}

	if err := resp.CheckResult(); err != nil {
		return nil, SIMAbsent, fmt.Errorf("UIM get card status failed: %w", err)
	}

	tlv := FindTLV(resp.TLVs, 0x10)
	if tlv == nil || len(tlv.Value) < 15 {
		return nil, SIMNotReady, fmt.Errorf("card status TLV missing or too short")
	}

	v := tlv.Value
	details := &CardStatusDetails{}
	details.NumSlot = v[8]
	details.CardState = v[9]
	details.UPINState = PINStatus(v[10])
	details.UPINRetries = v[11]
	details.UPUKRetries = v[12]
	details.ErrorCode = v[13]
	details.NumApp = v[14]

	type app struct {
		appType             uint8
		appState            uint8
		persoState          uint8
		persoFeature        uint8
		persoRetries        uint8
		persoUnblockRetries uint8
		aid                 []byte
		aidLen              uint8
		pin                 QMIUIM_PIN_STATE
	}

	offset := 15
	apps := make([]app, 0, int(details.NumApp))
	for i := 0; i < int(details.NumApp); i++ {
		if offset+7 > len(v) {
			break
		}
		a := app{
			appType:             v[offset],
			appState:            v[offset+1],
			persoState:          v[offset+2],
			persoFeature:        v[offset+3],
			persoRetries:        v[offset+4],
			persoUnblockRetries: v[offset+5],
			aidLen:              v[offset+6],
		}
		offset += 7
		if offset+int(a.aidLen) > len(v) {
			break
		}
		if a.aidLen > 0 {
			a.aid = make([]byte, int(a.aidLen))
			copy(a.aid, v[offset:offset+int(a.aidLen)])
		}
		offset += int(a.aidLen)
		if offset+7 > len(v) {
			break
		}
		a.pin = QMIUIM_PIN_STATE{
			UnivPIN:     v[offset],
			PIN1State:   v[offset+1],
			PIN1Retries: v[offset+2],
			PUK1Retries: v[offset+3],
			PIN2State:   v[offset+4],
			PIN2Retries: v[offset+5],
			PUK2Retries: v[offset+6],
		}
		offset += 7
		apps = append(apps, a)
	}

	var chosen *app
	for i := range apps {
		if apps[i].appType == 0x02 {
			chosen = &apps[i]
			break
		}
	}
	if chosen == nil && len(apps) > 0 {
		chosen = &apps[0]
	}
	if chosen != nil {
		details.AppType = chosen.appType
		details.AppState = chosen.appState
		details.PersoState = chosen.persoState
		details.PersoFeature = chosen.persoFeature
		details.PersoRetries = chosen.persoRetries
		details.PersoUnblockRetries = chosen.persoUnblockRetries
		details.AID = chosen.aid
		details.UsesUPIN = chosen.pin.UnivPIN == 1
		details.PIN1State = PINStatus(chosen.pin.PIN1State)
		details.PIN1Retries = chosen.pin.PIN1Retries
		details.PUK1Retries = chosen.pin.PUK1Retries
		details.PIN2State = PINStatus(chosen.pin.PIN2State)
		details.PIN2Retries = chosen.pin.PIN2Retries
		details.PUK2Retries = chosen.pin.PUK2Retries
	}

	status := SIMNotReady
	switch details.CardState {
	case 0x00:
		status = SIMAbsent
	case 0x02:
		status = SIMBlocked
	case 0x01:
		state := details.PIN1State
		verifyRetries := details.PIN1Retries
		unblockRetries := details.PUK1Retries
		if details.UsesUPIN {
			state = details.UPINState
			verifyRetries = details.UPINRetries
			unblockRetries = details.UPUKRetries
		}
		_ = verifyRetries
		_ = unblockRetries
		switch state {
		case PINStatusNotVerified:
			status = SIMPINRequired
		case PINStatusBlocked:
			status = SIMPUKRequired
		case PINStatusPermBlocked:
			status = SIMBlocked
		case PINStatusNotInit, PINStatusVerified, PINStatusDisabled, PINStatusUnblocked, PINStatusChanged:
			status = SIMReady
		default:
			status = SIMNotReady
		}
		if status == SIMReady && (details.PersoState == 1 || details.PersoState == 3 || details.PersoState == 4) {
			status = SIMNetworkLocked
		}
	default:
		status = SIMNotReady
	}

	return details, status, nil
}

// GetCardStatus queries the UIM card status / GetCardStatus查询UIM卡状态
func (u *UIMService) GetCardStatus(ctx context.Context) (SIMStatus, error) {
	_, st, err := u.GetCardStatusDetails(ctx)
	return st, err
}

// VerifyPIN verifies the PIN code / VerifyPIN 验证 PIN 码
func (u *UIMService) VerifyPIN(ctx context.Context, pinID uint8, pin string) error {
	var tlvs []TLV

	// TLV 0x01: PIN Info / PIN 信息
	// PIN ID (1) + PIN Len (1) + PIN Value
	pinBytes := []byte(pin)
	buf := make([]byte, 2+len(pinBytes))
	buf[0] = pinID
	buf[1] = uint8(len(pinBytes))
	copy(buf[2:], pinBytes)
	tlvs = append(tlvs, TLV{Type: 0x01, Value: buf})

	// TLV 0x02: Session Info / 会话信息 (Optional, default usually works)
	// AidLen (1) + Aid...
	// For simplicity, we omit session info assuming default primary session

	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMVerifyPIN, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

// SetPINProtection enables or disables PIN protection / SetPINProtection 启用或禁用 PIN 保护
func (u *UIMService) SetPINProtection(ctx context.Context, pinID uint8, enabled bool, pin string) error {
	var tlvs []TLV

	// TLV 0x01: PIN Info / PIN 信息
	pinBytes := []byte(pin)
	buf := make([]byte, 2+1+len(pinBytes)) // ID + Enable + Len + PIN
	buf[0] = pinID
	if enabled {
		buf[1] = 1
	} else {
		buf[1] = 0
	}
	buf[2] = uint8(len(pinBytes))
	copy(buf[3:], pinBytes)
	tlvs = append(tlvs, TLV{Type: 0x01, Value: buf})

	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMSetPINProtection, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

// ChangePIN changes the PIN code / ChangePIN 修改 PIN 码
func (u *UIMService) ChangePIN(ctx context.Context, pinID uint8, oldPIN, newPIN string) error {
	var tlvs []TLV

	// TLV 0x01: PIN Info / PIN 信息
	oldBytes := []byte(oldPIN)
	newBytes := []byte(newPIN)
	buf := make([]byte, 1+1+len(oldBytes)+1+len(newBytes))

	buf[0] = pinID
	buf[1] = uint8(len(oldBytes))
	copy(buf[2:], oldBytes)
	buf[2+len(oldBytes)] = uint8(len(newBytes))
	copy(buf[3+len(oldBytes):], newBytes)

	tlvs = append(tlvs, TLV{Type: 0x01, Value: buf})

	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMChangePIN, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

// UnblockPIN unblocks the PIN using PUK / UnblockPIN 使用 PUK 解锁 PIN
func (u *UIMService) UnblockPIN(ctx context.Context, pinID uint8, puk, newPIN string) error {
	var tlvs []TLV

	// TLV 0x01: Unblock Info / 解锁信息
	pukBytes := []byte(puk)
	newBytes := []byte(newPIN)
	buf := make([]byte, 1+1+len(pukBytes)+1+len(newBytes))

	buf[0] = pinID
	buf[1] = uint8(len(pukBytes))
	copy(buf[2:], pukBytes)
	buf[2+len(pukBytes)] = uint8(len(newBytes))
	copy(buf[3+len(pukBytes):], newBytes)

	tlvs = append(tlvs, TLV{Type: 0x01, Value: buf})

	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMUnblockPIN, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

func buildOpenLogicalChannelTLVs(slot uint8, aid []byte) []TLV {
	value := append([]byte{byte(len(aid))}, aid...)
	return []TLV{
		{Type: 0x10, Value: value},
		{Type: 0x01, Value: []byte{slot}},
	}
}

func buildCloseLogicalChannelTLVs(slot uint8, channel uint8) []TLV {
	return []TLV{
		{Type: 0x01, Value: []byte{slot}},
		{Type: 0x11, Value: []byte{channel}},
		{Type: 0x13, Value: []byte{0x01}},
	}
}

func buildSendAPDUTLVs(slot uint8, channel uint8, command []byte) []TLV {
	length := len(command)
	value := make([]byte, 2+len(command))
	binary.LittleEndian.PutUint16(value[0:2], uint16(length))
	copy(value[2:], command)
	return []TLV{
		{Type: 0x10, Value: []byte{channel}},
		{Type: 0x02, Value: value},
		{Type: 0x01, Value: []byte{slot}},
	}
}

func wrapUIMNotSupported(operation string, err error) error {
	if qe := GetQMIError(err); qe != nil && (qe.ErrorCode == QMIErrNotSupported || qe.ErrorCode == QMIErrInvalidQmiCmd) {
		return &NotSupportedError{Operation: operation}
	}
	return err
}

func parseOpenLogicalChannelResponse(resp *Packet) (byte, error) {
	if err := resp.CheckResult(); err != nil {
		return 0, wrapUIMNotSupported("open logical channel", err)
	}
	tlv := FindTLV(resp.TLVs, 0x10)
	if tlv == nil || len(tlv.Value) < 1 {
		return 0, fmt.Errorf("logical channel TLV missing or too short")
	}
	return tlv.Value[0], nil
}

func parseCloseLogicalChannelResponse(resp *Packet) error {
	if err := resp.CheckResult(); err != nil {
		return wrapUIMNotSupported("close logical channel", err)
	}
	return nil
}

func parseSendAPDUResponse(resp *Packet) ([]byte, error) {
	if err := resp.CheckResult(); err != nil {
		return nil, wrapUIMNotSupported("send APDU", err)
	}
	tlv := FindTLV(resp.TLVs, 0x10)
	if tlv == nil || len(tlv.Value) < 2 {
		return nil, fmt.Errorf("APDU response TLV missing or too short")
	}
	responseLen := int(binary.LittleEndian.Uint16(tlv.Value[0:2]))
	if len(tlv.Value) < 2+responseLen {
		return nil, fmt.Errorf("APDU response truncated")
	}
	return append([]byte(nil), tlv.Value[2:2+responseLen]...), nil
}

// OpenLogicalChannel opens a logical channel on the target slot and selects the given AID.
func (u *UIMService) OpenLogicalChannel(ctx context.Context, slot uint8, aid []byte) (byte, error) {
	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMOpenLogicalChannel, buildOpenLogicalChannelTLVs(slot, aid))
	if err != nil {
		return 0, err
	}
	return parseOpenLogicalChannelResponse(resp)
}

// CloseLogicalChannel closes the given logical channel on the target slot.
func (u *UIMService) CloseLogicalChannel(ctx context.Context, slot uint8, channel uint8) error {
	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMCloseLogicalChannel, buildCloseLogicalChannelTLVs(slot, channel))
	if err != nil {
		return err
	}
	return parseCloseLogicalChannelResponse(resp)
}

// SendAPDU transmits a raw APDU on the given logical channel and slot.
func (u *UIMService) SendAPDU(ctx context.Context, slot uint8, channel uint8, command []byte) ([]byte, error) {
	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMSendAPDU, buildSendAPDUTLVs(slot, channel, command))
	if err != nil {
		return nil, err
	}
	return parseSendAPDUResponse(resp)
}

// ReadTransparent reads a transparent file from the SIM card / ReadTransparent 从 SIM 卡读取透明文件
// fileID: e.g. 0x2FE2 for ICCID, 0x6F07 for IMSI
func (u *UIMService) ReadTransparent(ctx context.Context, fileID uint16, path []uint8) ([]byte, error) {
	return u.ReadTransparentWithSession(ctx, 0x00, fileID, path)
}

func (u *UIMService) ReadTransparentWithSession(ctx context.Context, sessionType uint8, fileID uint16, path []uint8) ([]byte, error) {
	var tlvs []TLV

	tlvs = append(tlvs, TLV{Type: 0x01, Value: []byte{sessionType, 0x00}})

	// TLV 0x02: File ID (Mandatory)
	// FileID (2) + PathLen (1) + Path...
	bufFile := make([]byte, 2+1+len(path))
	binary.LittleEndian.PutUint16(bufFile[0:2], fileID)
	bufFile[2] = uint8(len(path))
	if len(path) > 0 {
		copy(bufFile[3:], path)
	}
	tlvs = append(tlvs, TLV{Type: 0x02, Value: bufFile})

	// TLV 0x03: Read Information (Optional but good practice)
	// Offset (2) + Length (2)
	// 0, 0 means read entire file
	bufRead := make([]byte, 4)
	binary.LittleEndian.PutUint16(bufRead[0:2], 0)
	binary.LittleEndian.PutUint16(bufRead[2:4], 0)
	tlvs = append(tlvs, TLV{Type: 0x03, Value: bufRead})

	resp, err := u.client.SendRequest(ctx, ServiceUIM, u.clientID, UIMReadTransparent, tlvs)
	if err != nil {
		return nil, err
	}

	if err := resp.CheckResult(); err != nil {
		return nil, err
	}

	// TLV 0x11: Read Result (Content) - quectel-CM uses 0x11
	// Format: ContentLen (2) + Content...
	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil {
		if len(tlv.Value) < 2 {
			return nil, fmt.Errorf("read result too short")
		}
		contentLen := binary.LittleEndian.Uint16(tlv.Value[0:2])
		if len(tlv.Value) < int(2+contentLen) {
			return nil, fmt.Errorf("read result truncated")
		}
		return tlv.Value[2 : 2+contentLen], nil
	}

	// Fallback to 0x10 just in case
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil {
		return tlv.Value, nil
	}

	return nil, nil
}

func (u *UIMService) GetICCID(ctx context.Context) (string, error) {
	data, err := u.ReadTransparentWithSession(ctx, 0x06, 0x2FE2, []byte{0x00, 0x3F})
	if err != nil {
		data, err = u.ReadTransparentWithSession(ctx, 0x06, 0x2FE2, []byte{})
		if err != nil {
			return "", err
		}
	}
	if len(data) == 0 {
		return "", fmt.Errorf("empty ICCID")
	}
	return decodeSwappedBCD(data), nil
}

func (u *UIMService) GetIMSI(ctx context.Context) (string, error) {
	data, err := u.ReadTransparentWithSession(ctx, 0x00, 0x6F07, []byte{0x00, 0x3F, 0xFF, 0x7F})
	if err != nil {
		data, err = u.ReadTransparentWithSession(ctx, 0x00, 0x6F07, []byte{0x20, 0x7F})
		if err != nil {
			data, err = u.ReadTransparentWithSession(ctx, 0x00, 0x6F07, []byte{})
			if err != nil {
				return "", err
			}
		}
	}
	if len(data) <= 1 {
		return "", fmt.Errorf("invalid IMSI length")
	}
	bcd := data[1:]
	if int(data[0]) <= len(data)-1 {
		bcd = data[1 : 1+int(data[0])]
	}
	imsi := decodeSwappedBCD(bcd)
	if imsi == "" {
		return "", fmt.Errorf("empty IMSI")
	}

	// 核心修复: 3GPP TS 31.102 规范说明 EF_IMSI 文件的第一个字节低 4 位
	// 并非 IMSI 实际数字，而是奇偶校验/身份类型前缀（通常为 0x01 或 0x09）。
	// decodeSwappedBCD 毫无差别地将该 nibble (1 或 9) 放到了第一位输出。
	// 这会导致正常的譬如 "234..." 被加上 9 前缀变成 "9234..." ！
	// 故必须切掉错误解析出的第一位。
	if len(imsi) > 0 {
		imsi = imsi[1:]
	}

	return imsi, nil
}

func (u *UIMService) GetNativeMCCMNC(ctx context.Context) (mcc string, mnc string, err error) {
	// 1. 获取 IMSI
	imsi, err := u.GetIMSI(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get IMSI: %w", err)
	}
	if len(imsi) < 5 {
		return "", "", fmt.Errorf("invalid IMSI length: %s", imsi)
	}

	// 2. 尝试读取 EF_AD (0x6FAD) 获取 MNC 长度
	mncLen := 2 // 默认安全回退到 2 位
	adData, adErr := u.ReadTransparentWithSession(ctx, 0x00, 0x6FAD, []byte{0x00, 0x3F, 0xFF, 0x7F})
	if adErr != nil {
		adData, adErr = u.ReadTransparentWithSession(ctx, 0x00, 0x6FAD, []byte{0x20, 0x7F})
		if adErr != nil {
			adData, _ = u.ReadTransparentWithSession(ctx, 0x00, 0x6FAD, []byte{})
		}
	}

	// EF_AD 文件如果存在且长度足够，第 4 字节（索引为 3）存放了 MNC 的长度
	if len(adData) >= 4 {
		// 第 4 字节规定了 MNC 位数 (0x02 或 0x03)
		if adData[3] == 0x02 || adData[3] == 0x03 {
			mncLen = int(adData[3])
		}
	}

	if len(imsi) < 3+mncLen {
		return "", "", fmt.Errorf("invalid IMSI length %d for MNC length %d", len(imsi), mncLen)
	}

	mcc = imsi[0:3]
	mnc = imsi[3: 3+mncLen]

	return mcc, mnc, nil
}

func decodeSwappedBCD(data []byte) string {
	out := make([]byte, 0, len(data)*2)
	for _, b := range data {
		low := b & 0x0F
		high := (b >> 4) & 0x0F

		if low <= 9 {
			out = append(out, '0'+byte(low))
		}
		if high <= 9 {
			out = append(out, '0'+byte(high))
		}
	}
	return string(out)
}
