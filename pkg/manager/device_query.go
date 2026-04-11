package manager

import (
	"context"
	"fmt"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
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

// GetManufacturer 获取模组厂商
func (m *Manager) GetManufacturer(ctx context.Context) (string, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return "", ErrServiceNotReady("DMS")
	}
	return dms.GetManufacturer(ctx)
}

// GetModel 获取模组型号
func (m *Manager) GetModel(ctx context.Context) (string, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return "", ErrServiceNotReady("DMS")
	}
	return dms.GetModel(ctx)
}

// GetHardwareRevision 获取硬件版本
func (m *Manager) GetHardwareRevision(ctx context.Context) (string, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return "", ErrServiceNotReady("DMS")
	}
	return dms.GetHardwareRevision(ctx)
}

// GetSoftwareVersion 获取软件版本
func (m *Manager) GetSoftwareVersion(ctx context.Context) (string, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return "", ErrServiceNotReady("DMS")
	}
	return dms.GetSoftwareVersion(ctx)
}

// GetMSISDN 获取设备关联的 MSISDN
func (m *Manager) GetMSISDN(ctx context.Context) (string, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return "", ErrServiceNotReady("DMS")
	}
	return dms.GetMSISDN(ctx)
}

// GetFactorySKU 获取出厂 SKU
func (m *Manager) GetFactorySKU(ctx context.Context) (string, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return "", ErrServiceNotReady("DMS")
	}
	return dms.GetFactorySKU(ctx)
}

// GetCapabilities 获取模组总体能力信息
func (m *Manager) GetCapabilities(ctx context.Context) (*qmi.DeviceCapabilities, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return nil, ErrServiceNotReady("DMS")
	}
	return dms.GetCapabilities(ctx)
}

// GetPowerState 获取供电与电池状态
func (m *Manager) GetPowerState(ctx context.Context) (*qmi.PowerStateInfo, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return nil, ErrServiceNotReady("DMS")
	}
	return dms.GetPowerState(ctx)
}

// GetTime 获取模组当前时间计数
func (m *Manager) GetTime(ctx context.Context) (*qmi.TimeInfo, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return nil, ErrServiceNotReady("DMS")
	}
	return dms.GetTime(ctx)
}

// GetPRLVersion 获取 PRL 版本信息
func (m *Manager) GetPRLVersion(ctx context.Context) (*qmi.PRLVersionInfo, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return nil, ErrServiceNotReady("DMS")
	}
	return dms.GetPRLVersion(ctx)
}

// GetActivationState 获取业务激活状态
func (m *Manager) GetActivationState(ctx context.Context) (qmi.ActivationState, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return qmi.ActivationStateNotActivated, ErrServiceNotReady("DMS")
	}
	return dms.GetActivationState(ctx)
}

// GetUserLockState 获取用户锁状态
func (m *Manager) GetUserLockState(ctx context.Context) (bool, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return false, ErrServiceNotReady("DMS")
	}
	return dms.GetUserLockState(ctx)
}

// SetUserLockState 设置用户锁状态
func (m *Manager) SetUserLockState(ctx context.Context, enabled bool, lockCode string) error {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return ErrServiceNotReady("DMS")
	}
	return dms.SetUserLockState(ctx, enabled, lockCode)
}

// SetUserLockCode 修改用户锁码
func (m *Manager) SetUserLockCode(ctx context.Context, oldCode, newCode string) error {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return ErrServiceNotReady("DMS")
	}
	return dms.SetUserLockCode(ctx, oldCode, newCode)
}

// ReadUserData 读取设备用户数据
func (m *Manager) ReadUserData(ctx context.Context) ([]byte, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return nil, ErrServiceNotReady("DMS")
	}
	return dms.ReadUserData(ctx)
}

// WriteUserData 写入设备用户数据
func (m *Manager) WriteUserData(ctx context.Context, data []byte) error {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return ErrServiceNotReady("DMS")
	}
	return dms.WriteUserData(ctx, data)
}

// GetMACAddress 获取指定类型的 MAC 地址
func (m *Manager) GetMACAddress(ctx context.Context, macType uint32) (*qmi.MACAddressInfo, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return nil, ErrServiceNotReady("DMS")
	}
	return dms.GetMACAddress(ctx, macType)
}

// GetBandCapabilities 获取模组支持的频段能力
func (m *Manager) GetBandCapabilities(ctx context.Context) (*qmi.BandCapabilities, error) {
	m.mu.RLock()
	dms := m.dms
	m.mu.RUnlock()
	if dms == nil {
		return nil, ErrServiceNotReady("DMS")
	}
	return dms.GetBandCapabilities(ctx)
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
	dms := m.dms
	m.mu.RUnlock()

	var lastErr error
	if dms != nil {
		iccid, err := dms.GetICCID(ctx)
		if err == nil && iccid != "" {
			return iccid, nil
		}
		lastErr = err
	} else {
		lastErr = ErrServiceNotReady("DMS")
	}

	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return "", fmt.Errorf("无法获取 ICCID (DMS 通道失败: %v, 且 UIM 服务未就绪)", lastErr)
	}
	iccid, err := uim.GetICCID(ctx)
	if err == nil && iccid != "" {
		return iccid, nil
	}
	return "", fmt.Errorf("DMS & UIM 双通道均无法获取 ICCID (UIM error: %v, DMS lastError: %v)", err, lastErr)
}

// UIMGetSlotStatus 获取物理/逻辑卡槽状态
func (m *Manager) UIMGetSlotStatus(ctx context.Context) (*qmi.UIMSlotStatus, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return nil, ErrServiceNotReady("UIM")
	}
	return uim.GetSlotStatus(ctx)
}

// UIMSwitchSlot 切换逻辑 slot 到目标物理 slot
func (m *Manager) UIMSwitchSlot(ctx context.Context, logicalSlot uint8, physicalSlot uint32) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return ErrServiceNotReady("UIM")
	}
	return uim.SwitchSlot(ctx, logicalSlot, physicalSlot)
}

// UIMReadRecord 读取 record 型 EF 文件
func (m *Manager) UIMReadRecord(ctx context.Context, fileID uint16, path []uint8, recordNumber uint16, recordLength uint16) (*qmi.UIMRecordData, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return nil, ErrServiceNotReady("UIM")
	}
	return uim.ReadRecord(ctx, fileID, path, recordNumber, recordLength)
}

// UIMReadRecordWithSession 使用指定 session 读取 record 型 EF 文件
func (m *Manager) UIMReadRecordWithSession(ctx context.Context, sessionType uint8, fileID uint16, path []uint8, recordNumber uint16, recordLength uint16) (*qmi.UIMRecordData, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return nil, ErrServiceNotReady("UIM")
	}
	return uim.ReadRecordWithSession(ctx, sessionType, fileID, path, recordNumber, recordLength)
}

// UIMGetFileAttributes 获取 SIM 文件元数据
func (m *Manager) UIMGetFileAttributes(ctx context.Context, fileID uint16, path []uint8) (*qmi.UIMFileAttributes, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return nil, ErrServiceNotReady("UIM")
	}
	return uim.GetFileAttributes(ctx, fileID, path)
}

// UIMGetFileAttributesWithSession 使用指定 session 获取 SIM 文件元数据
func (m *Manager) UIMGetFileAttributesWithSession(ctx context.Context, sessionType uint8, fileID uint16, path []uint8) (*qmi.UIMFileAttributes, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return nil, ErrServiceNotReady("UIM")
	}
	return uim.GetFileAttributesWithSession(ctx, sessionType, fileID, path)
}

// UIMRegisterEvents 注册 UIM 事件掩码
func (m *Manager) UIMRegisterEvents(ctx context.Context, mask uint32) (uint32, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return 0, ErrServiceNotReady("UIM")
	}
	return uim.RegisterEvents(ctx, mask)
}

// UIMGetSupportedMessages 获取 UIM service 支持的消息 ID
func (m *Manager) UIMGetSupportedMessages(ctx context.Context) ([]uint8, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return nil, ErrServiceNotReady("UIM")
	}
	return uim.GetSupportedMessages(ctx)
}

// UIMReset 重置 UIM service 状态
func (m *Manager) UIMReset(ctx context.Context) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return ErrServiceNotReady("UIM")
	}
	return uim.Reset(ctx)
}

// UIMPowerOffSIM 关闭指定 slot 的 SIM 电源
func (m *Manager) UIMPowerOffSIM(ctx context.Context, slot uint8) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return ErrServiceNotReady("UIM")
	}
	return uim.PowerOffSIM(ctx, slot)
}

// UIMPowerOnSIM 打开指定 slot 的 SIM 电源
func (m *Manager) UIMPowerOnSIM(ctx context.Context, slot uint8) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return ErrServiceNotReady("UIM")
	}
	return uim.PowerOnSIM(ctx, slot)
}

// UIMChangeProvisioningSession 切换 UIM provisioning session
func (m *Manager) UIMChangeProvisioningSession(ctx context.Context, req qmi.UIMChangeProvisioningSessionRequest) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return ErrServiceNotReady("UIM")
	}
	return uim.ChangeProvisioningSession(ctx, req)
}

// UIMRefreshRegister 注册 UIM refresh 文件列表
func (m *Manager) UIMRefreshRegister(ctx context.Context, req qmi.UIMRefreshRegisterRequest) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return ErrServiceNotReady("UIM")
	}
	return uim.RefreshRegister(ctx, req)
}

// UIMRefreshComplete 上报 UIM refresh 处理完成
func (m *Manager) UIMRefreshComplete(ctx context.Context, req qmi.UIMRefreshCompleteRequest) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return ErrServiceNotReady("UIM")
	}
	return uim.RefreshComplete(ctx, req)
}

// UIMRefreshRegisterAll 注册 UIM 全文件 refresh
func (m *Manager) UIMRefreshRegisterAll(ctx context.Context, req qmi.UIMRefreshRegisterAllRequest) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return ErrServiceNotReady("UIM")
	}
	return uim.RefreshRegisterAll(ctx, req)
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

// NASGetRFBandInfo 获取当前频段与信道信息
func (m *Manager) NASGetRFBandInfo(ctx context.Context) (*qmi.RFBandInfo, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetRFBandInfo(ctx)
}

// NASGetTechnologyPreference 获取当前 RAT 偏好
func (m *Manager) NASGetTechnologyPreference(ctx context.Context) (*qmi.TechnologyPreference, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetTechnologyPreference(ctx)
}

// NASSetTechnologyPreference 设置当前 RAT 偏好
func (m *Manager) NASSetTechnologyPreference(ctx context.Context, pref qmi.TechnologyPreference) error {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return ErrServiceNotReady("NAS")
	}
	return nas.SetTechnologyPreference(ctx, pref)
}

// NASGetSystemSelectionPreference 获取系统选择策略
func (m *Manager) NASGetSystemSelectionPreference(ctx context.Context) (*qmi.SystemSelectionPreference, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetSystemSelectionPreference(ctx)
}

// NASSetSystemSelectionPreference 设置系统选择策略
func (m *Manager) NASSetSystemSelectionPreference(ctx context.Context, pref qmi.SystemSelectionPreference) error {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return ErrServiceNotReady("NAS")
	}
	return nas.SetSystemSelectionPreference(ctx, pref)
}

// NASGetCellLocationInfo 获取当前小区位置与制式信息
func (m *Manager) NASGetCellLocationInfo(ctx context.Context) (*qmi.CellLocationInfo, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetCellLocationInfo(ctx)
}

// NASGetNetworkTime 获取网络时间
func (m *Manager) NASGetNetworkTime(ctx context.Context) (*qmi.NetworkTimeInfo, error) {
	m.mu.RLock()
	nas := m.nas
	m.mu.RUnlock()
	if nas == nil {
		return nil, ErrServiceNotReady("NAS")
	}
	return nas.GetNetworkTime(ctx)
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

// WMSRawWriteMessage 将短信写入模组存储并返回索引
func (m *Manager) WMSRawWriteMessage(ctx context.Context, storageType uint8, format uint8, pdu []byte) (uint32, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return 0, ErrServiceNotReady("WMS")
	}
	return wms.RawWriteMessage(ctx, storageType, format, pdu)
}

// WMSGetMessageProtocol 获取当前短信协议
func (m *Manager) WMSGetMessageProtocol(ctx context.Context) (qmi.WMSMessageProtocol, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return 0, ErrServiceNotReady("WMS")
	}
	return wms.GetMessageProtocol(ctx)
}

// WMSGetSupportedMessages 获取 WMS service 支持的消息 ID
func (m *Manager) WMSGetSupportedMessages(ctx context.Context) ([]uint8, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return nil, ErrServiceNotReady("WMS")
	}
	return wms.GetSupportedMessages(ctx)
}

// WMSSetRoutes 设置短信路由表
func (m *Manager) WMSSetRoutes(ctx context.Context, routes []qmi.WMSRoute, transferStatusReportToClient bool) error {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return ErrServiceNotReady("WMS")
	}
	return wms.SetRoutes(ctx, routes, transferStatusReportToClient)
}

// WMSGetRoutes 获取短信路由表
func (m *Manager) WMSGetRoutes(ctx context.Context) (*qmi.WMSRouteConfig, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return nil, ErrServiceNotReady("WMS")
	}
	return wms.GetRoutes(ctx)
}

// WMSSendAck 发送短信 ACK
func (m *Manager) WMSSendAck(ctx context.Context, req qmi.WMSAckRequest) (*qmi.WMSAckResult, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return nil, ErrServiceNotReady("WMS")
	}
	return wms.SendAck(ctx, req)
}

// WMSSendFromStorage 从存储索引发送短信
func (m *Manager) WMSSendFromStorage(ctx context.Context, storageType uint8, index uint32, mode qmi.MessageMode, smsOnIMS bool) (*qmi.WMSSendFromStorageResult, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return nil, ErrServiceNotReady("WMS")
	}
	return wms.SendFromStorage(ctx, storageType, index, mode, smsOnIMS)
}

// WMSIndicationRegister 注册 WMS 指示上报开关
func (m *Manager) WMSIndicationRegister(ctx context.Context, reportTransportNetworkRegistration bool) error {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return ErrServiceNotReady("WMS")
	}
	return wms.IndicationRegister(ctx, reportTransportNetworkRegistration)
}

// WMSGetTransportNetworkRegistrationStatus 获取短信传输网络注册状态
func (m *Manager) WMSGetTransportNetworkRegistrationStatus(ctx context.Context) (qmi.WMSTransportNetworkRegistration, error) {
	m.mu.RLock()
	wms := m.wms
	m.mu.RUnlock()
	if wms == nil {
		return 0, ErrServiceNotReady("WMS")
	}
	return wms.GetTransportNetworkRegistrationStatus(ctx)
}

// WDSGetChannelRates 获取当前/最大信道速率
func (m *Manager) WDSGetChannelRates(ctx context.Context) (*qmi.ChannelRates, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetChannelRates(ctx)
}

// WDSGetPacketStatistics 获取 WDS 统计计数器
func (m *Manager) WDSGetPacketStatistics(ctx context.Context, mask uint32) (*qmi.PacketStatistics, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetPacketStatistics(ctx, mask)
}

// WDSCreateProfile 创建 PDP Profile
func (m *Manager) WDSCreateProfile(ctx context.Context, profileType uint8, settings qmi.WDSProfileSettings) (*qmi.ProfileInfo, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.CreateProfile(ctx, profileType, settings)
}

// WDSModifyProfileSettings 修改 PDP Profile
func (m *Manager) WDSModifyProfileSettings(ctx context.Context, profileType, profileIndex uint8, settings qmi.WDSProfileSettings) error {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return ErrServiceNotReady("WDS")
	}
	return wds.ModifyProfileSettings(ctx, profileType, profileIndex, settings)
}

// WDSDeleteProfile 删除 PDP Profile
func (m *Manager) WDSDeleteProfile(ctx context.Context, profileType, profileIndex uint8) error {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return ErrServiceNotReady("WDS")
	}
	return wds.DeleteProfile(ctx, profileType, profileIndex)
}

// WDSGetAutoconnectSettings 获取自动拨号设置
func (m *Manager) WDSGetAutoconnectSettings(ctx context.Context) (*qmi.AutoconnectSettings, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetAutoconnectSettings(ctx)
}

// WDSSetAutoconnectSettings 设置自动拨号参数
func (m *Manager) WDSSetAutoconnectSettings(ctx context.Context, settings qmi.AutoconnectSettings) error {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return ErrServiceNotReady("WDS")
	}
	return wds.SetAutoconnectSettings(ctx, settings)
}

// WDSGetDataBearerTechnology 获取传统承载制式信息
func (m *Manager) WDSGetDataBearerTechnology(ctx context.Context) (*qmi.DataBearerTechnologyInfo, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetDataBearerTechnology(ctx)
}

// WDSGetCurrentDataBearerTechnology 获取当前承载网络/RAT/SO 信息
func (m *Manager) WDSGetCurrentDataBearerTechnology(ctx context.Context) (*qmi.CurrentBearerTechnologyInfo, error) {
	m.mu.RLock()
	wds := m.wds
	m.mu.RUnlock()
	if wds == nil {
		return nil, ErrServiceNotReady("WDS")
	}
	return wds.GetCurrentDataBearerTechnology(ctx)
}

// IMSABind 显式绑定 IMSA 到指定 subscription/binding
func (m *Manager) IMSABind(ctx context.Context, binding uint32) error {
	m.mu.RLock()
	imsa := m.imsa
	m.mu.RUnlock()
	if imsa == nil {
		return ErrServiceNotReady("IMSA")
	}
	return imsa.Bind(ctx, binding)
}

// IMSAGetIMSRegistrationStatus 获取 IMS 注册状态
func (m *Manager) IMSAGetIMSRegistrationStatus(ctx context.Context) (*qmi.IMSARegistrationStatus, error) {
	m.mu.RLock()
	imsa := m.imsa
	m.mu.RUnlock()
	if imsa == nil {
		return nil, ErrServiceNotReady("IMSA")
	}
	return imsa.GetIMSRegistrationStatus(ctx)
}

// IMSAGetIMSServicesStatus 获取 IMS 各业务可用状态
func (m *Manager) IMSAGetIMSServicesStatus(ctx context.Context) (*qmi.IMSAServicesStatus, error) {
	m.mu.RLock()
	imsa := m.imsa
	m.mu.RUnlock()
	if imsa == nil {
		return nil, ErrServiceNotReady("IMSA")
	}
	return imsa.GetIMSServicesStatus(ctx)
}

// IMSARegisterIndications 注册 IMSA 指示开关
func (m *Manager) IMSARegisterIndications(ctx context.Context, cfg qmi.IMSAIndicationRegistration) error {
	m.mu.RLock()
	imsa := m.imsa
	m.mu.RUnlock()
	if imsa == nil {
		return ErrServiceNotReady("IMSA")
	}
	return imsa.RegisterIndications(ctx, cfg)
}

// IMSBind 显式绑定 IMS settings service
func (m *Manager) IMSBind(ctx context.Context, binding uint32) error {
	m.mu.RLock()
	ims := m.ims
	m.mu.RUnlock()
	if ims == nil {
		return ErrServiceNotReady("IMS")
	}
	return ims.Bind(ctx, binding)
}

// IMSGetServicesEnabledSetting 获取 IMS enable setting
func (m *Manager) IMSGetServicesEnabledSetting(ctx context.Context) (*qmi.IMSServicesEnabledSettings, error) {
	m.mu.RLock()
	ims := m.ims
	m.mu.RUnlock()
	if ims == nil {
		return nil, ErrServiceNotReady("IMS")
	}
	return ims.GetServicesEnabledSetting(ctx)
}

// IMSSetServicesEnabledSetting 显式修改 IMS enable setting
func (m *Manager) IMSSetServicesEnabledSetting(ctx context.Context, update qmi.IMSServicesEnabledSettingsUpdate) error {
	m.mu.RLock()
	ims := m.ims
	m.mu.RUnlock()
	if ims == nil {
		return ErrServiceNotReady("IMS")
	}
	return ims.SetServicesEnabledSetting(ctx, update)
}

// IMSPGetEnablerState 获取 IMSP enabler 状态
func (m *Manager) IMSPGetEnablerState(ctx context.Context) (qmi.IMSPEnablerState, error) {
	m.mu.RLock()
	imsp := m.imsp
	m.mu.RUnlock()
	if imsp == nil {
		return 0, ErrServiceNotReady("IMSP")
	}
	return imsp.GetEnablerState(ctx)
}

// VOICEIndicationRegister 注册 VOICE 指示开关
func (m *Manager) VOICEIndicationRegister(ctx context.Context, cfg qmi.VoiceIndicationRegistration) error {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return ErrServiceNotReady("VOICE")
	}
	return voice.IndicationRegister(ctx, cfg)
}

// VOICEGetSupportedMessages 获取 VOICE service 支持的消息 ID
func (m *Manager) VOICEGetSupportedMessages(ctx context.Context) ([]uint8, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return nil, ErrServiceNotReady("VOICE")
	}
	return voice.GetSupportedMessages(ctx)
}

// VOICEDialCall 拨打语音电话
func (m *Manager) VOICEDialCall(ctx context.Context, number string) (uint8, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return 0, ErrServiceNotReady("VOICE")
	}
	return voice.DialCall(ctx, number)
}

// VOICEEndCall 挂断语音电话
func (m *Manager) VOICEEndCall(ctx context.Context, callID uint8) (uint8, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return 0, ErrServiceNotReady("VOICE")
	}
	return voice.EndCall(ctx, callID)
}

// VOICEAnswerCall 接听语音电话
func (m *Manager) VOICEAnswerCall(ctx context.Context, callID uint8) (uint8, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return 0, ErrServiceNotReady("VOICE")
	}
	return voice.AnswerCall(ctx, callID)
}

// VOICEBurstDTMF 发送一串 DTMF 按键
func (m *Manager) VOICEBurstDTMF(ctx context.Context, callID uint8, digits string) (uint8, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return 0, ErrServiceNotReady("VOICE")
	}
	return voice.BurstDTMF(ctx, callID, digits)
}

// VOICEStartContinuousDTMF 开始持续 DTMF
func (m *Manager) VOICEStartContinuousDTMF(ctx context.Context, callID uint8, digit uint8) (uint8, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return 0, ErrServiceNotReady("VOICE")
	}
	return voice.StartContinuousDTMF(ctx, callID, digit)
}

// VOICEStopContinuousDTMF 停止持续 DTMF
func (m *Manager) VOICEStopContinuousDTMF(ctx context.Context, callID uint8) (uint8, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return 0, ErrServiceNotReady("VOICE")
	}
	return voice.StopContinuousDTMF(ctx, callID)
}

// VOICEGetAllCallInfo 获取当前全部通话信息
func (m *Manager) VOICEGetAllCallInfo(ctx context.Context) (*qmi.VoiceAllCallInfo, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return nil, ErrServiceNotReady("VOICE")
	}
	return voice.GetAllCallInfo(ctx)
}

// VOICEManageCalls 执行保持/恢复/切换等通话管理动作
func (m *Manager) VOICEManageCalls(ctx context.Context, req qmi.VoiceManageCallsRequest) error {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return ErrServiceNotReady("VOICE")
	}
	return voice.ManageCalls(ctx, req)
}

// VOICESetSupplementaryService 设置补充业务
func (m *Manager) VOICESetSupplementaryService(ctx context.Context, req qmi.VoiceSupplementaryServiceRequest) (*qmi.VoiceSupplementaryServiceStatus, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return nil, ErrServiceNotReady("VOICE")
	}
	return voice.SetSupplementaryService(ctx, req)
}

// VOICEGetCallWaiting 查询呼叫等待状态
func (m *Manager) VOICEGetCallWaiting(ctx context.Context, serviceClass uint8) (uint8, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return 0, ErrServiceNotReady("VOICE")
	}
	return voice.GetCallWaiting(ctx, serviceClass)
}

// VOICEOriginateUSSD 发起 USSD
func (m *Manager) VOICEOriginateUSSD(ctx context.Context, req qmi.VoiceUSSDRequest) (*qmi.VoiceUSSDResponse, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return nil, ErrServiceNotReady("VOICE")
	}
	return voice.OriginateUSSD(ctx, req)
}

// VOICEAnswerUSSD 回复 USSD
func (m *Manager) VOICEAnswerUSSD(ctx context.Context, req qmi.VoiceUSSDRequest) error {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return ErrServiceNotReady("VOICE")
	}
	return voice.AnswerUSSD(ctx, req)
}

// VOICECancelUSSD 取消当前 USSD 会话
func (m *Manager) VOICECancelUSSD(ctx context.Context) error {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return ErrServiceNotReady("VOICE")
	}
	return voice.CancelUSSD(ctx)
}

// VOICEGetConfig 查询语音配置
func (m *Manager) VOICEGetConfig(ctx context.Context, query qmi.VoiceConfigQuery) (*qmi.VoiceConfig, error) {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return nil, ErrServiceNotReady("VOICE")
	}
	return voice.GetConfig(ctx, query)
}

// VOICEOriginateUSSDNoWait 发起异步 USSD
func (m *Manager) VOICEOriginateUSSDNoWait(ctx context.Context, req qmi.VoiceUSSDRequest) error {
	m.mu.RLock()
	voice := m.voice
	m.mu.RUnlock()
	if voice == nil {
		return ErrServiceNotReady("VOICE")
	}
	return voice.OriginateUSSDNoWait(ctx, req)
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

// SMSNotReadyError 表示短信控制面尚未恢复就绪。
type SMSNotReadyError struct {
	TransportStatus      string
	TransportKnown       bool
	TransportUnsupported bool
	TransportQueryError  string
	SMSCAvailable        bool
	RoutesKnown          bool
	NASRegistered        *bool
}

func (e *SMSNotReadyError) Error() string {
	nasRegistered := "unknown"
	if e.NASRegistered != nil {
		nasRegistered = fmt.Sprintf("%t", *e.NASRegistered)
	}
	return fmt.Sprintf(
		"QMI 短信未就绪: transport_status=%s transport_known=%t transport_unsupported=%t smsc_available=%t routes_known=%t nas_registered=%s transport_query_error=%q",
		e.TransportStatus,
		e.TransportKnown,
		e.TransportUnsupported,
		e.SMSCAvailable,
		e.RoutesKnown,
		nasRegistered,
		e.TransportQueryError,
	)
}
