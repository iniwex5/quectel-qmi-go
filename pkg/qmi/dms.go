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

const (
	/* Defined in frame.go / 在 frame.go 中定义
	DMSUIMGetState      uint16 = 0x0044
	DMSUIMVerifyPIN     uint16 = 0x0028
	*/
	DMSUIMSetPINProtection uint16 = 0x0027
	DMSUIMChangePIN        uint16 = 0x0029
	DMSUIMUnblockPIN       uint16 = 0x002A
)

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

func (d *DMSService) ClientID() uint8 {
	return d.clientID
}

type NotSupportedError struct {
	Operation string
}

func (e *NotSupportedError) Error() string {
	if e.Operation == "" {
		return "not supported"
	}
	return e.Operation + ": not supported"
}

type PINStatus uint8

const (
	PINStatusNotInit     PINStatus = 0
	PINStatusNotVerified PINStatus = 1
	PINStatusVerified    PINStatus = 2
	PINStatusDisabled    PINStatus = 3
	PINStatusBlocked     PINStatus = 4
	PINStatusPermBlocked PINStatus = 5
	PINStatusUnblocked   PINStatus = 6
	PINStatusChanged     PINStatus = 7
)

type PINInfo struct {
	Status             PINStatus
	VerifyRetriesLeft  uint8
	UnblockRetriesLeft uint8
}

func (d *DMSService) GetPINStatus(ctx context.Context) (*PINInfo, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSUIMGetPINStatus, nil)
	if err != nil {
		return nil, err
	}

	if err := resp.CheckResult(); err != nil {
		if qe := GetQMIError(err); qe != nil && qe.ErrorCode == QMIErrNotSupported {
			return nil, &NotSupportedError{Operation: "get PIN status"}
		}
		return nil, fmt.Errorf("UIM get PIN status failed: %w", err)
	}

	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil && len(tlv.Value) >= 3 {
		return &PINInfo{
			Status:             PINStatus(tlv.Value[0]),
			VerifyRetriesLeft:  tlv.Value[1],
			UnblockRetriesLeft: tlv.Value[2],
		}, nil
	}

	return nil, fmt.Errorf("no PIN status in response")
}

// GetSIMStatus queries the SIM card status / GetSIMStatus查询SIM卡状态
func (d *DMSService) GetSIMStatus(ctx context.Context) (SIMStatus, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSUIMGetState, nil)
	if err != nil {
		return SIMAbsent, err
	}

	if err := resp.CheckResult(); err != nil {
		if qe := GetQMIError(err); qe != nil && qe.ErrorCode == QMIErrNotSupported {
			uim, uerr := NewUIMService(d.client)
			if uerr != nil {
				return SIMAbsent, &NotSupportedError{Operation: "get SIM status"}
			}
			defer uim.Close()
			return uim.GetCardStatus(ctx)
		}
		return SIMAbsent, fmt.Errorf("UIM get state failed: %w", err)
	}

	// TLV 0x01: UIM state / TLV 0x01: UIM状态
	if tlv := FindTLV(resp.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 1 {
		state := tlv.Value[0]
		switch state {
		case 0x00:
			return SIMReady, nil // UIM initialization completed / UIM初始化完成
		case 0x01:
			pin, err := d.GetPINStatus(ctx)
			if err == nil {
				switch pin.Status {
				case PINStatusNotVerified:
					return SIMPINRequired, nil
				case PINStatusBlocked:
					return SIMPUKRequired, nil
				case PINStatusPermBlocked:
					return SIMBlocked, nil
				}
			}
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

	if err := resp.CheckResult(); err != nil {
		if retrytlv := FindTLV(resp.TLVs, 0x10); retrytlv != nil && len(retrytlv.Value) >= 1 {
			return fmt.Errorf("PIN verification failed, %d retries left: %w", retrytlv.Value[0], err)
		}
		return fmt.Errorf("PIN verification failed: %w", err)
	}

	return nil
}

// GetOperatingMode queries the current operating mode / GetOperatingMode查询当前操作模式
func (d *DMSService) GetOperatingMode(ctx context.Context) (OperatingMode, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSGetOperatingMode, nil)
	if err != nil {
		return ModeOnline, err
	}

	if err := resp.CheckResult(); err != nil {
		return ModeOnline, fmt.Errorf("get operating mode failed: %w", err)
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

	if err := resp.CheckResult(); err != nil {
		return fmt.Errorf("set operating mode failed: %w", err)
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

// SetPINProtection enables or disables PIN protection / SetPINProtection 启用或禁用 PIN 保护
func (s *DMSService) SetPINProtection(ctx context.Context, pinID uint8, enabled bool, pin string) error {
	var tlvs []TLV

	// TLV 0x01: PIN Protection Info / PIN 保护信息
	// 1 byte PIN ID + 1 byte Enable/Disable (0/1) + PIN Length + PIN
	pinBytes := []byte(pin)
	buf := make([]byte, 2+1+len(pinBytes))
	buf[0] = pinID
	if enabled {
		buf[1] = 1
	} else {
		buf[1] = 0
	}
	buf[2] = uint8(len(pinBytes))
	copy(buf[3:], pinBytes)
	tlvs = append(tlvs, TLV{Type: 0x01, Value: buf})

	resp, err := s.client.SendRequest(ctx, ServiceDMS, s.clientID, DMSUIMSetPINProtection, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

// ChangePIN changes the PIN code / ChangePIN 修改 PIN 码
func (s *DMSService) ChangePIN(ctx context.Context, pinID uint8, oldPIN, newPIN string) error {
	var tlvs []TLV

	// TLV 0x01: PIN Info / PIN 信息
	// 1 byte PIN ID + Old PIN Length + Old PIN + New PIN Length + New PIN
	oldBytes := []byte(oldPIN)
	newBytes := []byte(newPIN)
	buf := make([]byte, 1+1+len(oldBytes)+1+len(newBytes))

	buf[0] = pinID
	buf[1] = uint8(len(oldBytes))
	copy(buf[2:], oldBytes)
	buf[2+len(oldBytes)] = uint8(len(newBytes))
	copy(buf[3+len(oldBytes):], newBytes)

	tlvs = append(tlvs, TLV{Type: 0x01, Value: buf})

	resp, err := s.client.SendRequest(ctx, ServiceDMS, s.clientID, DMSUIMChangePIN, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

// UnblockPIN unblocks the PIN using PUK / UnblockPIN 使用 PUK 解锁 PIN
func (s *DMSService) UnblockPIN(ctx context.Context, pinID uint8, puk, newPIN string) error {
	var tlvs []TLV

	// TLV 0x01: Unblock Info / 解锁信息
	// 1 byte PIN ID + PUK Length + PUK + New PIN Length + New PIN
	pukBytes := []byte(puk)
	newBytes := []byte(newPIN)
	buf := make([]byte, 1+1+len(pukBytes)+1+len(newBytes))

	buf[0] = pinID
	buf[1] = uint8(len(pukBytes))
	copy(buf[2:], pukBytes)
	buf[2+len(pukBytes)] = uint8(len(newBytes))
	copy(buf[3+len(pukBytes):], newBytes)

	tlvs = append(tlvs, TLV{Type: 0x01, Value: buf})

	resp, err := s.client.SendRequest(ctx, ServiceDMS, s.clientID, DMSUIMUnblockPIN, tlvs)
	if err != nil {
		return err
	}
	return resp.CheckResult()
}

// ============================================================================
// Device Info / 设备信息
// ============================================================================

// DeviceInfo contains modem identification information / DeviceInfo包含modem识别信息
type DeviceInfo struct {
	Manufacturer string
	Model        string
	Revision     string
	IMEI         string
	ESN          string
	MEID         string
}

// GetDeviceSerialNumbers retrieves IMEI and other serial numbers / GetDeviceSerialNumbers检索IMEI和其他序列号
func (d *DMSService) GetDeviceSerialNumbers(ctx context.Context) (*DeviceInfo, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSGetDeviceSerialNumbers, nil)
	if err != nil {
		return nil, err
	}

	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("get serial numbers failed: %w", err)
	}

	info := &DeviceInfo{}

	// TLV 0x10: ESN
	// TLV 0x11: IMEI
	// TLV 0x12: MEID
	if tlv := FindTLV(resp.TLVs, 0x10); tlv != nil {
		info.ESN = string(tlv.Value)
	}
	if tlv := FindTLV(resp.TLVs, 0x11); tlv != nil {
		info.IMEI = string(tlv.Value)
	}
	if tlv := FindTLV(resp.TLVs, 0x12); tlv != nil {
		info.MEID = string(tlv.Value)
	}

	return info, nil
}

// GetDeviceRevision retrieves firmware revision / GetDeviceRevision检索固件版本
func (d *DMSService) GetDeviceRevision(ctx context.Context) (string, string, error) {
	resp, err := d.client.SendRequest(ctx, ServiceDMS, d.clientID, DMSGetDeviceRevID, nil)
	if err != nil {
		return "", "", err
	}

	if err := resp.CheckResult(); err != nil {
		return "", "", fmt.Errorf("get revision failed: %w", err)
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
