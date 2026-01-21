package qmi

import (
	"context"
	"fmt"
)

// ============================================================================
// SIM/UIM Status / SIM/UIM状态
// ============================================================================

type SIMStatus uint8

const (
	SIMAbsent        SIMStatus = 0
	SIMNotReady      SIMStatus = 1
	SIMReady         SIMStatus = 2
	SIMPINRequired   SIMStatus = 3
	SIMPUKRequired   SIMStatus = 4
	SIMBlocked       SIMStatus = 5
	SIMNetworkLocked SIMStatus = 6
)

func (s SIMStatus) String() string {
	switch s {
	case SIMAbsent:
		return "absent"
	case SIMNotReady:
		return "not_ready"
	case SIMReady:
		return "ready"
	case SIMPINRequired:
		return "pin_required"
	case SIMPUKRequired:
		return "puk_required"
	case SIMBlocked:
		return "blocked"
	case SIMNetworkLocked:
		return "network_locked"
	default:
		return "unknown"
	}
}

// ============================================================================
// Operating Mode / 操作模式
// ============================================================================

type OperatingMode uint8

const (
	ModeOnline       OperatingMode = 0x00
	ModeLowPower     OperatingMode = 0x01
	ModeFactoryTest  OperatingMode = 0x02
	ModeOffline      OperatingMode = 0x03
	ModeReset        OperatingMode = 0x04
	ModeShutdown     OperatingMode = 0x05
	ModePersistLow   OperatingMode = 0x06
	ModeOnlyLowPower OperatingMode = 0x07
)

// ============================================================================
// DMS Service wrapper / DMS服务包装器
// ============================================================================

type DMSService struct {
	client   *Client
	clientID uint8
}

// NewDMSService creates a DMS service wrapper / NewDMSService创建一个DMS服务包装器
func NewDMSService(client *Client) (*DMSService, error) {
	clientID, err := client.AllocateClientID(ServiceDMS)
	if err != nil {
		return nil, err
	}
	return &DMSService{client: client, clientID: clientID}, nil
}

// Close releases the DMS client ID / Close释放DMS客户端ID
func (d *DMSService) Close() error {
	return d.client.ReleaseClientID(ServiceDMS, d.clientID)
}

// GetSIMStatus queries the SIM card status / GetSIMStatus查询SIM卡状态
func (d *DMSService) GetSIMStatus(ctx context.Context) (SIMStatus, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSUIMGetState, nil)
	if err != nil {
		return SIMAbsent, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return SIMAbsent, fmt.Errorf("UIM get state failed: 0x%04x", qmiErr)
	}

	// TLV 0x01: UIM state / TLV 0x01: UIM状态
	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 1 {
		state := tlv.Value[0]
		switch state {
		case 0x00:
			return SIMReady, nil // UIM initialization completed / UIM初始化完成
		case 0x01:
			return SIMPINRequired, nil // UIM is locked / UIM被锁定
		case 0x02:
			return SIMAbsent, nil // UIM not present / UIM不在位
		default:
			return SIMNotReady, nil
		}
	}

	return SIMAbsent, fmt.Errorf("no UIM state in response")
}

// VerifyPIN verifies the SIM PIN / VerifyPIN验证SIM PIN
func (d *DMSService) VerifyPIN(ctx context.Context, pin string) error {
	if len(pin) == 0 || len(pin) > 8 {
		return fmt.Errorf("invalid PIN length")
	}

	// TLV 0x01: PIN ID (1) and PIN value / TLV 0x01: PIN ID (1) 和 PIN值
	tlvData := append([]byte{0x01, uint8(len(pin))}, []byte(pin)...)
	tlvs := []TLV{{Type: 0x01, Value: tlvData}}

	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSUIMVerifyPIN, tlvs)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		// Check for retries remaining in TLV 0x10 / 检查TLV 0x10中剩余重试次数
		if retrytlv := FindTLV(resp.TLVs, 0x10); retrytlv != nil && len(retrytlv.Value) >= 1 {
			return fmt.Errorf("PIN verification failed (0x%04x), %d retries left", qmiErr, retrytlv.Value[0])
		}
		return fmt.Errorf("PIN verification failed: 0x%04x", qmiErr)
	}

	return nil
}

// GetOperatingMode queries the current operating mode / GetOperatingMode查询当前操作模式
func (d *DMSService) GetOperatingMode(ctx context.Context) (OperatingMode, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSGetOperatingMode, nil)
	if err != nil {
		return ModeOnline, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return ModeOnline, fmt.Errorf("get operating mode failed: 0x%04x", qmiErr)
	}

	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 1 {
		return OperatingMode(tlv.Value[0]), nil
	}

	return ModeOnline, fmt.Errorf("no mode in response")
}

// SetOperatingMode changes the modem operating mode / SetOperatingMode更改modem操作模式
// Use ModeOnline to turn radio on, ModeLowPower to turn off, ModeReset to reboot modem / 使用ModeOnline打开射频，ModeLowPower关闭，ModeReset重启modem
func (d *DMSService) SetOperatingMode(ctx context.Context, mode OperatingMode) error {
	tlvs := []TLV{NewTLVUint8(0x01, uint8(mode))}

	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSSetOperatingMode, tlvs)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return fmt.Errorf("set operating mode failed: 0x%04x", qmiErr)
	}

	return nil
}

// RadioPower turns the radio on or off / RadioPower打开或关闭射频
func (d *DMSService) RadioPower(ctx context.Context, on bool) error {
	if on {
		return d.SetOperatingMode(ctx, ModeOnline)
	}
	return d.SetOperatingMode(ctx, ModeLowPower)
}

// DeviceInfo contains modem identification information / DeviceInfo包含modem识别信息
type DeviceInfo struct {
	Manufacturer string
	Model        string
	Revision     string
	IMEI         string
}

// GetDeviceSerialNumbers retrieves IMEI and other serial numbers / GetDeviceSerialNumbers检索IMEI和其他序列号
func (d *DMSService) GetDeviceSerialNumbers(ctx context.Context) (*DeviceInfo, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSGetDeviceSerialNumbers, nil)
	if err != nil {
		return nil, err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return nil, fmt.Errorf("get serial numbers failed: 0x%04x", qmiErr)
	}

	info := &DeviceInfo{}

	// TLV 0x10: ESN
	// TLV 0x11: IMEI
	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil {
		info.IMEI = string(tlv.Value)
	}

	return info, nil
}

// GetDeviceRevision retrieves firmware revision / GetDeviceRevision检索固件版本
func (d *DMSService) GetDeviceRevision(ctx context.Context) (string, string, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSGetDeviceRevID, nil)
	if err != nil {
		return "", "", err
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return "", "", fmt.Errorf("get revision failed: 0x%04x", qmiErr)
	}

	var revision, bootVersion string

	// TLV 0x01: Device revision / TLV 0x01: 设备版本
	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil {
		revision = string(tlv.Value)
	}

	// TLV 0x10: Boot version / TLV 0x10: Boot版本
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil {
		bootVersion = string(tlv.Value)
	}

	return revision, bootVersion, nil
}
