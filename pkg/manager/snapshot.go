package manager

import (
	"sync"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

// ============================================================================
// DeviceSnapshot — 设备状态快照，由 NAS Indication 事件驱动更新
// ============================================================================
//
//
// 缓存的种类：
// 1. 完全动态：ServingSystem, Signal 等 (依赖 Indication / Polling)
// 2. 静态及半静态：IMEI, IMSI, ICCID, 固件版本等 (由 Manager 的拉取逻辑预热写入)
//
// 上层可通过 Manager.GetDeviceSnapshot() 零 IPC 读取。

// DeviceIdentities 设备的核心不变与半固化标识快照（如 SIM 信息）。
type DeviceIdentities struct {
	IMEI             string
	ICCID            string
	IMSI             string
	FirmwareRevision string
	HardwareRevision string
	Manufacturer     string
	Model            string
	OperatingMode    *qmi.OperatingMode
	SimInserted      *bool
}

// DeviceSnapshot 记录由 QMI Indication 事件驱动的最新设备网络状态。
type DeviceSnapshot struct {
	mu sync.RWMutex

	// 来自 NAS ServingSystemChanged indication
	servingSystem *qmi.ServingSystem
	lastServing   time.Time

	// 来自 SignalUpdate（doStatusCheck 或 Indication 均会触发）
	signal     *qmi.SignalStrength
	lastSignal time.Time

	// 来自 NASSysInfoInd (0 IPC 获取网络小区状态)
	sysInfo     *qmi.SysInfo
	lastSysInfo time.Time

	// 来自内部的 PreWarm 和刷新操作组
	identities      DeviceIdentities
	identitiesReady bool
}

// updateServing 由 handleIndication 在 EventServingSystemChanged 时调用。
// 内部加锁，调用方无需额外同步。
func (s *DeviceSnapshot) updateServing(ss *qmi.ServingSystem) {
	if ss == nil {
		return
	}
	s.mu.Lock()
	s.servingSystem = ss
	s.lastServing = time.Now()
	s.mu.Unlock()
}

func (s *DeviceSnapshot) updateSysInfo(si *qmi.SysInfo) {
	if si == nil {
		return
	}
	s.mu.Lock()
	s.sysInfo = si
	s.lastSysInfo = time.Now()
	s.mu.Unlock()
}

// updateSignal 由 emitSignalUpdate 时同步调用。
// 内部加锁，调用方无需额外同步。
func (s *DeviceSnapshot) updateSignal(sig *qmi.SignalStrength) {
	if sig == nil {
		return
	}
	s.mu.Lock()
	s.signal = sig
	s.lastSignal = time.Now()
	s.mu.Unlock()
}

// ServingSystem 返回最新的服务系统快照及其时间戳。
// 如果从未更新过，返回 nil 和 zero time。
func (s *DeviceSnapshot) ServingSystem() (*qmi.ServingSystem, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.servingSystem, s.lastServing
}

// Signal 返回最新的信号强度快照及其时间戳。
// 如果从未更新过，返回 nil 和 zero time。
func (s *DeviceSnapshot) Signal() (*qmi.SignalStrength, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.signal, s.lastSignal
}

// SysInfo 返回最新的小区系统信息及时间戳。
func (s *DeviceSnapshot) SysInfo() (*qmi.SysInfo, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sysInfo, s.lastSysInfo
}

// UpdateIdentities 由 Manager 组件异步拉取汇总后同步调用写入。
func (s *DeviceSnapshot) UpdateIdentities(ids DeviceIdentities) {
	s.mu.Lock()
	// 支持字段叠加而非全部覆盖，因为有时只会刷新 ICCID/IMSI，不需要覆盖掉 IMEI
	if ids.IMEI != "" {
		s.identities.IMEI = ids.IMEI
	}
	if ids.ICCID != "" {
		s.identities.ICCID = ids.ICCID
	}
	if ids.IMSI != "" {
		s.identities.IMSI = ids.IMSI
	}
	if ids.FirmwareRevision != "" {
		s.identities.FirmwareRevision = ids.FirmwareRevision
	}
	if ids.HardwareRevision != "" {
		s.identities.HardwareRevision = ids.HardwareRevision
	}
	if ids.Manufacturer != "" {
		s.identities.Manufacturer = ids.Manufacturer
	}
	if ids.Model != "" {
		s.identities.Model = ids.Model
	}
	if ids.OperatingMode != nil {
		s.identities.OperatingMode = ids.OperatingMode
	}
	if ids.SimInserted != nil {
		s.identities.SimInserted = ids.SimInserted
	}
	s.identitiesReady = true
	s.mu.Unlock()
}

// ResetIdentities 用于清除会随 SIM 卡变化的标识数据缓存（ICCID / IMSI），
// 或者在明确丢失底层数据时使用。对于 IMEI 坚固数据可以酌情保留。
func (s *DeviceSnapshot) ResetIdentities(clearAll bool) {
	s.mu.Lock()
	if clearAll {
		s.identities = DeviceIdentities{}
		s.identitiesReady = false
	} else {
		// 仅清空卡强相关字段
		s.identities.ICCID = ""
		s.identities.IMSI = ""
		// identitiesReady 依然保持 true，因为 IMEI 还在
	}
	s.mu.Unlock()
}

// Identities 返回设备标识缓存字典与当前是否可用的就绪状态。
func (s *DeviceSnapshot) Identities() (DeviceIdentities, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identities, s.identitiesReady
}

// Reset 清空所有动态与强相关快照数据。
// 在 Modem Reset 时由上层调用，确保不会读到旧数据。
func (s *DeviceSnapshot) Reset() {
	s.mu.Lock()
	s.servingSystem = nil
	s.lastServing = time.Time{}
	s.signal = nil
	s.lastSignal = time.Time{}
	// 清空卡关连信息，但可保留硬件坚固信息
	s.identities.ICCID = ""
	s.identities.IMSI = ""
	s.mu.Unlock()
}

// GetDeviceSnapshot 返回当前设备状态快照的指针。
// 调用方可通过 ServingSystem() 和 Signal() 方法分别读取。
// 该方法永远不会阻塞，不发出任何 QMI IPC。
func (m *Manager) GetDeviceSnapshot() *DeviceSnapshot {
	return &m.snapshot
}
