package manager

import (
	"context"
	"fmt"

	"github.com/iniwex5/quectel-cm-go/pkg/qmi"
)

// ============================================================================
// 设备信息查询方法 — 将底层 DMS/UIM/NAS/WMS 服务的能力暴露给上层调用者
// ============================================================================

// GetDeviceSerialNumbers 获取设备序列号信息（含 IMEI）
func (m *Manager) GetDeviceSerialNumbers(ctx context.Context) (*qmi.DeviceInfo, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return nil, ErrServiceNotReady("DMS")
	}
	return dms.GetDeviceSerialNumbers(ctx)
}

// GetDeviceRevision 获取设备固件版本
func (m *Manager) GetDeviceRevision(ctx context.Context) (string, string, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return "", "", ErrServiceNotReady("DMS")
	}
	return dms.GetDeviceRevision(ctx)
}

// GetIMSI 获取 SIM 卡 IMSI / GetIMSI retrieves SIM IMSI via fallback strategy
func (m *Manager) GetIMSI(ctx context.Context) (string, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()

	var lastErr error

	// 优先尝试 DMS 获取 (DMS 通常最稳定且自带缓存)
	if dms != nil {
		imsi, err := dms.GetIMSI(ctx)
		if err == nil && imsi != "" {
			return imsi, nil
		}
		lastErr = err
	} else {
		lastErr = ErrServiceNotReady("DMS")
	}

	// 降级尝试 UIM 透明获取
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()

	if uim != nil {
		imsi, err := uim.GetIMSI(ctx)
		if err == nil && imsi != "" {
			return imsi, nil
		}
		return "", fmt.Errorf("DMS & UIM 双通道均无法获取 IMSI (UIM error: %v, DMS lastError: %v)", err, lastErr)
	}

	return "", fmt.Errorf("无法获取 IMSI (DMS 通道失败: %v, 且 UIM 服务未就绪)", lastErr)
}

// GetICCID 获取 SIM 卡 ICCID
func (m *Manager) GetICCID(ctx context.Context) (string, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return "", ErrServiceNotReady("UIM")
	}
	return uim.GetICCID(ctx)
}

// GetSIMStatus 获取 SIM 卡状态
func (m *Manager) GetSIMStatus(ctx context.Context) (qmi.SIMStatus, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return qmi.SIMAbsent, ErrServiceNotReady("DMS")
	}
	return dms.GetSIMStatus(ctx)
}

// GetServingSystem 获取当前网络服务系统信息
func (m *Manager) GetServingSystem(ctx context.Context) (*qmi.ServingSystem, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetServingSystem(ctx)
}

// GetSignalStrength 获取信号强度
func (m *Manager) GetSignalStrength(ctx context.Context) (*qmi.SignalStrength, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetSignalStrength(ctx)
}

// GetSignalInfo 获取详细信号信息（LTE/5G）
func (m *Manager) GetSignalInfo(ctx context.Context) (*qmi.SignalInfo, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetSignalInfo(ctx)
}

// GetSysInfo 获取系统信息（CellID/TAC/LAC）
func (m *Manager) GetSysInfo(ctx context.Context) (*qmi.SysInfo, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetSysInfo(ctx)
}

// GetOperatingMode 获取设备当前操作模式
func (m *Manager) GetOperatingMode(ctx context.Context) (qmi.OperatingMode, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return 0, ErrServiceNotReady("DMS")
	}
	return dms.GetOperatingMode(ctx)
}

// SetOperatingMode 设置设备操作模式（飞行模式 / 在线 / 低功耗等）
func (m *Manager) SetOperatingMode(ctx context.Context, mode qmi.OperatingMode) error {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return ErrServiceNotReady("DMS")
	}
	return dms.SetOperatingMode(ctx, mode)
}

// WMSSendRawMessage 发送原始短信 PDU
func (m *Manager) WMSSendRawMessage(ctx context.Context, format uint8, pdu []byte) error {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return ErrServiceNotReady("WMS")
	}
	return wms.SendRawMessage(ctx, format, pdu)
}

// WMSRawReadMessage 读取原始短信 PDU
func (m *Manager) WMSRawReadMessage(ctx context.Context, storageType uint8, index uint32) ([]byte, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return nil, ErrServiceNotReady("WMS")
	}
	return wms.RawReadMessage(ctx, storageType, index)
}

// WMSDeleteMessage 删除短信
func (m *Manager) WMSDeleteMessage(ctx context.Context, storageType uint8, index uint32) error {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return ErrServiceNotReady("WMS")
	}
	return wms.DeleteMessage(ctx, storageType, index)
}

// WMSListMessagesAuto 列出短信
func (m *Manager) WMSListMessagesAuto(ctx context.Context, storageType uint8) ([]struct {
	Index uint32
	Tag   qmi.MessageTagType
}, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return nil, ErrServiceNotReady("WMS")
	}
	return wms.ListMessagesAuto(ctx, storageType)
}

// WMSDeleteMessagesByTag 按标签删除短信
func (m *Manager) WMSDeleteMessagesByTag(ctx context.Context, storageType uint8, tag qmi.MessageTagType, mode qmi.MessageMode) error {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return ErrServiceNotReady("WMS")
	}
	return wms.DeleteMessagesByTag(ctx, storageType, tag, mode)
}

// ErrServiceNotReady 返回指定 QMI 服务未初始化或未就绪的错误
func ErrServiceNotReady(service string) error {
	return &ServiceNotReadyError{Service: service}
}

// ServiceNotReadyError 表示 QMI 服务未就绪
type ServiceNotReadyError struct {
	Service string
}

func (e *ServiceNotReadyError) Error() string {
	return "QMI 服务未就绪: " + e.Service
}
