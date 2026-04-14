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
// 只缓存 Indication 天然携带的字段（ServingSystem + Signal），
// 不做主动 IPC 拉取。上层可通过 Manager.GetDeviceSnapshot() 零 IPC 读取。

// DeviceSnapshot 记录由 QMI Indication 事件驱动的最新设备网络状态。
type DeviceSnapshot struct {
	mu sync.RWMutex

	// 来自 NAS ServingSystemChanged indication
	servingSystem *qmi.ServingSystem
	lastServing   time.Time

	// 来自 SignalUpdate（doStatusCheck 或 Indication 均会触发）
	signal     *qmi.SignalStrength
	lastSignal time.Time
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

// Reset 清空所有快照数据。
// 在 Modem Reset 时由上层调用，确保不会读到旧数据。
func (s *DeviceSnapshot) Reset() {
	s.mu.Lock()
	s.servingSystem = nil
	s.lastServing = time.Time{}
	s.signal = nil
	s.lastSignal = time.Time{}
	s.mu.Unlock()
}

// GetDeviceSnapshot 返回当前设备状态快照的指针。
// 调用方可通过 ServingSystem() 和 Signal() 方法分别读取。
// 该方法永远不会阻塞，不发出任何 QMI IPC。
func (m *Manager) GetDeviceSnapshot() *DeviceSnapshot {
	return &m.snapshot
}
