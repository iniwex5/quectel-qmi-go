package manager

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/iniwex5/quectel-cm-go/pkg/device"
	"github.com/iniwex5/quectel-cm-go/pkg/netcfg"
	"github.com/iniwex5/quectel-cm-go/pkg/qmi"
	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
)

// ============================================================================
// Connection State Machine / 连接状态机
// ============================================================================

type State int

const (
	StateDisconnected State = iota // Disconnected / 已断开
	StateConnecting                // Connecting / 连接中
	StateConnected                 // Connected / 已连接
	StateStopping                  // Stopping / 正在停止
)

func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

// ============================================================================
// Configuration / 配置
// ============================================================================

type Config struct {
	Device        device.ModemDevice // Modem device info / Modem 设备信息
	APN           string             // APN (Access Point Name) / APN（接入点名称）
	Username      string             // Authentication username / 认证用户名
	Password      string             // Authentication password / 认证密码
	AuthType      uint8              // 0=none, 1=PAP, 2=CHAP, 3=PAP|CHAP / 认证类型
	EnableIPv4    bool               // Enable IPv4 / 启用 IPv4
	EnableIPv6    bool               // Enable IPv6 / 启用 IPv6
	PINCode       string             // SIM PIN code / SIM 卡 PIN 码
	AutoReconnect bool               // Automatically reconnect on disconnect / 断开后自动重连
	NoRoute       bool               // Don't add default route (useful for debugging) / 不添加默认路由 (用于调试)
	NoDNS         bool               // Don't configure DNS (useful for debugging) / 不配置DNS (用于调试)
	DisableWMSInd bool               // Disable WMS indications (Event Report) / 禁用 WMS 指示 (事件报告)

	// 多路拨号 (QMAP) 配置
	ProfileIndex uint8 // PDN Profile 索引 (对应 -n 参数, 默认 0 表示使用模组默认 Profile)
	MuxID        uint8 // QMAP Mux ID (对应 -m 参数, 默认 0 表示不启用多路复用)
}

// ============================================================================
// Manager - Core connection manager / 核心连接管理器
// ============================================================================

type Manager struct {
	cfg Config
	log Logger

	// QMI services / QMI服务
	client *qmi.Client
	wds    *qmi.WDSService
	wdsV6  *qmi.WDSService // Separate WDS for IPv6 / 用于IPv6的独立WDS服务
	nas    *qmi.NASService
	dms    *qmi.DMSService
	uim    *qmi.UIMService
	wda    *qmi.WDAService
	wms    *qmi.WMSService // SMS

	// Connection handles / 连接句柄
	handleV4 uint32
	handleV6 uint32

	// State
	mu       sync.RWMutex
	state    State
	settings *qmi.RuntimeSettings

	// Event handling
	// Event handling / 事件处理
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	eventCh chan internalEvent
	events  *EventEmitter // External event callbacks / 外部事件回调

	// Reconnection / 重连相关
	retryCount   int
	retryDelays  []time.Duration
	isRotating   bool // Flag to suppress status checks during IP rotation / 标志位: IP轮换期间抑制状态检查
	recoverCount int

	// Internal notification / 内部通知
	regNotify chan bool // For fast registration detection / 用于快速注册检测

	// 多路拨号 (QMAP) / Multi-PDN
	muxIface string // QMAP 绑定后的虚拟网卡名 (如 qmimux0)
}

// internalEvent represents an internal event for the manager's event loop. / internalEvent 表示管理器事件循环的内部事件。
type internalEvent int

const (
	eventStart                internalEvent = iota // Start connection / 开始连接
	eventStop                                      // Stop connection / 停止连接
	eventCheck                                     // Physical status check / 物理状态检查
	eventPacketStatusChanged                       // Packet service status changed indication / 数据包服务状态改变指示
	eventServingSystemChanged                      // Serving system changed indication / 服务系统改变指示
	eventModemReset                                // Modem reset indication / Modem重置指示
)

var defaultRetryDelays = []time.Duration{
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	40 * time.Second,
	60 * time.Second,
}

// New creates a new connection manager / New 创建新的连接管理器
// logger is optional, if nil a default logger will be used / logger 是可选的，如果为 nil 则使用默认日志器
func New(cfg Config, logger Logger) *Manager {
	if logger == nil {
		logger = NewNopLogger()
	}

	return &Manager{
		cfg:         cfg,
		log:         logger.WithField("iface", cfg.Device.NetInterface),
		retryDelays: defaultRetryDelays,
		eventCh:     make(chan internalEvent, 16),
		events:      NewEventEmitter(),
	}
}

// Start initializes and starts the connection manager / Start 初始化并启动连接管理器
func (m *Manager) Start() error {
	m.mu.Lock()
	// Check if already started / 检查是否已启动
	if m.state != StateDisconnected {
		m.mu.Unlock()
		return fmt.Errorf("manager already started")
	}
	m.state = StateConnecting
	m.mu.Unlock()

	if err := m.openClientAndAllocateServices(); err != nil {
		m.cleanup()
		m.setState(StateDisconnected)
		return err
	}

	// Check SIM status / 检查SIM卡状态
	if err := m.checkSIM(); err != nil {
		m.log.WithError(err).Warn("SIM check failed")
		// Continue anyway - might work / 继续尝试，也许能工作
	}

	// Start event loop / 启动事件循环
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(2)
	go m.eventLoop()
	go m.indicationHandler()

	// Trigger initial connection / 触发初始连接
	m.eventCh <- eventStart

	return nil
}

// Stop gracefully stops the connection manager / Stop 优雅停止连接管理器
func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.state == StateDisconnected || m.state == StateStopping {
		m.mu.Unlock()
		return nil
	}
	m.state = StateStopping
	m.mu.Unlock()

	m.log.Info("Stopping connection manager...")
	m.eventCh <- eventStop

	// Wait for loops to finish / 等待循环结束
	m.cancel()
	m.wg.Wait()

	m.cleanup()
	m.setState(StateDisconnected)
	m.log.Info("Connection manager stopped")
	return nil
}

// State returns the current connection state / State 返回当前的连接状态
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Settings returns the current IP settings / Settings 返回当前的 IP 设置
func (m *Manager) Settings() *qmi.RuntimeSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

// OpenLogicalChannel opens a UIM logical channel using a fixed 10s timeout.
func (m *Manager) OpenLogicalChannel(slot uint8, aid []byte) (byte, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return 0, fmt.Errorf("uim service not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return uim.OpenLogicalChannel(ctx, slot, aid)
}

// CloseLogicalChannel closes a UIM logical channel using a fixed 10s timeout.
func (m *Manager) CloseLogicalChannel(slot uint8, channel uint8) error {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return fmt.Errorf("uim service not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return uim.CloseLogicalChannel(ctx, slot, channel)
}

// SendAPDU transmits a raw APDU using a fixed 10s timeout.
func (m *Manager) SendAPDU(slot uint8, channel uint8, command []byte) ([]byte, error) {
	m.mu.RLock()
	uim := m.uim
	m.mu.RUnlock()
	if uim == nil {
		return nil, fmt.Errorf("uim service not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return uim.SendAPDU(ctx, slot, channel, command)
}

// RotateIP disconnects and reconnects to get a new IP address / RotateIP 断开并重新连接以获取新 IP 地址
func (m *Manager) RotateIP() error {
	m.mu.Lock()
	if m.state != StateConnected {
		m.mu.Unlock()
		return fmt.Errorf("not connected, cannot rotate IP")
	}
	m.isRotating = true // Suppress status checks / 抑制状态检查
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.isRotating = false
		m.mu.Unlock()
	}()

	oldIP := ""
	if m.settings != nil && m.settings.IPv4Address != nil {
		oldIP = m.settings.IPv4Address.String()
	}
	m.log.Infof("Rotating IP (current: %s)...", oldIP)

	ctx := context.Background()

	// 1. Disconnect data call / 1. 断开数据呼叫
	if m.handleV4 != 0 && m.wds != nil {
		m.wds.StopNetworkInterface(ctx, m.handleV4)
		m.handleV4 = 0
	}

	// Flush old addresses to avoid duplicates / 清除旧地址以避免重复
	netcfg.FlushAddresses(m.cfg.Device.NetInterface)

	// 2. Wait a bit (reduced for efficiency) / 2. 短暂等待 (为了效率而缩减)
	time.Sleep(100 * time.Millisecond)

	// 3. Reconnect / 3. 重新连接
	handle, err := m.wds.StartNetworkInterface(ctx,
		m.cfg.APN, m.cfg.Username, m.cfg.Password,
		m.cfg.AuthType, qmi.IpFamilyV4)
	if err != nil {
		return m.rotateViaRadioReset()
	}

	// CHECK BEFORE CONFIG: Quickly check if IP actually changed / 配置前检查: 快速检查 IP 是否真的变了
	settings, err := m.wds.GetRuntimeSettings(ctx, qmi.IpFamilyV4)
	if err == nil && settings.IPv4Address != nil && settings.IPv4Address.String() == oldIP {
		m.log.Warn("IP same after redial, forcing radio reset...")
		m.wds.StopNetworkInterface(ctx, handle)
		return m.rotateViaRadioReset()
	}
	m.handleV4 = handle

	// 4. Reconfigure network / 4. 重新配置网络
	if err := m.configureNetwork(); err != nil {
		return err
	}

	// Restore connected state / 恢复已连接状态
	m.setState(StateConnected)
	m.retryCount = 0

	newIP := ""
	if m.settings != nil && m.settings.IPv4Address != nil {
		newIP = m.settings.IPv4Address.String()
	}

	if oldIP == newIP {
		m.log.Warn("IP unchanged, trying radio reset...")
		return m.rotateViaRadioReset()
	}

	m.log.Infof("IP rotated: %s -> %s", oldIP, newIP)

	// Emit IP change event / 5. 发送 IP 变化事件
	if m.events != nil {
		m.events.Emit(Event{
			Type:     EventIPChanged,
			State:    StateConnected,
			Settings: m.settings,
		})
	}

	return nil
}

// rotateViaRadioReset performs IP rotation by resetting the radio / rotateViaRadioReset 通过重置射频执行 IP 轮换
func (m *Manager) rotateViaRadioReset() error {
	ctx := context.Background()

	oldIP := ""
	if m.settings != nil && m.settings.IPv4Address != nil {
		oldIP = m.settings.IPv4Address.String()
	}

	// 1. Disconnect current call / 1. 断开当前呼叫
	if m.handleV4 != 0 && m.wds != nil {
		m.wds.StopNetworkInterface(ctx, m.handleV4)
		m.handleV4 = 0
	}

	// Flush old addresses / 2. 清除旧地址
	netcfg.FlushAddresses(m.cfg.Device.NetInterface)

	// 2. Radio off / 3. 关闭射频
	if m.dms != nil {
		m.log.Info("Turning radio off...")
		m.dms.RadioPower(ctx, false)
		time.Sleep(200 * time.Millisecond) // Short delay to let firmware process / 短暂延迟让固件处理

		// 3. Radio on / 4. 打开射频
		m.log.Info("Turning radio on...")
		m.dms.RadioPower(ctx, true)
		// No fixed sleep here, start polling immediately / 此处无固定睡眠，立即开始轮询
	}

	// 4. Wait for registration / 5. 等待注册
	m.mu.Lock()
	m.regNotify = make(chan bool, 1)
	notify := m.regNotify
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.regNotify = nil
		m.mu.Unlock()
	}()

	// Initial check in case we already registered / 初始检查，以防我们已经注册了
	if registered, _ := m.nas.IsRegistered(ctx); registered {
		goto registered
	}

	m.log.Info("Waiting for network registration (via indication)...")
	select {
	case <-notify:
		m.log.Debug("Received registration indication")
	case <-time.After(10 * time.Second):
		m.log.Warn("Registration timeout, trying to connect anyway")
	case <-ctx.Done():
		return ctx.Err()
	}

registered:

	// 5. Reconnect / 6. 重新连接
	handle, err := m.wds.StartNetworkInterface(ctx,
		m.cfg.APN, m.cfg.Username, m.cfg.Password,
		m.cfg.AuthType, qmi.IpFamilyV4)
	if err != nil {
		return fmt.Errorf("redial after radio reset failed: %w", err)
	}
	m.handleV4 = handle

	// 6. Reconfigure network / 7. 重新配置网络
	if err := m.configureNetwork(); err != nil {
		return err
	}

	// Restore connected state / 恢复已连接状态
	m.setState(StateConnected)
	m.retryCount = 0

	newIP := ""
	if m.settings != nil && m.settings.IPv4Address != nil {
		newIP = m.settings.IPv4Address.String()
	}

	m.log.Infof("IP rotated via radio reset: %s -> %s", oldIP, newIP)

	// Emit IP change event / 8. 发送 IP 变化事件
	if m.events != nil {
		m.events.Emit(Event{
			Type:     EventIPChanged,
			State:    StateConnected,
			Settings: m.settings,
		})
	}

	return nil
}

// ============================================================================
// Internal methods / 内部方法
// ============================================================================

func (m *Manager) setState(s State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != s {
		m.log.Infof("State: %s -> %s", m.state, s)
		m.state = s
	}
}

func (m *Manager) allocateServices() error {
	var err error

	// WDS for IPv4 / IPv4的WDS服务
	if m.cfg.EnableIPv4 {
		m.log.Debug("Allocating WDS client for IPv4...")
		m.wds, err = qmi.NewWDSService(m.client)
		if err != nil {
			return fmt.Errorf("failed to allocate WDS client: %w", err)
		}
		m.log.Debug("Allocated WDS client for IPv4")
		time.Sleep(500 * time.Millisecond)
	}

	// WDS for IPv6 (needs separate client) / IPv6的WDS服务 (需要单独的客户端)
	if m.cfg.EnableIPv6 {
		m.log.Debug("Allocating WDS client for IPv6...")
		m.wdsV6, err = qmi.NewWDSService(m.client)
		if err != nil {
			m.log.WithError(err).Warn("Failed to allocate IPv6 WDS client")
		} else {
			m.log.Debug("Allocated WDS client for IPv6")
		}
		time.Sleep(500 * time.Millisecond)
	}

	// NAS
	m.log.Debug("Allocating NAS client...")
	m.nas, err = qmi.NewNASService(m.client)
	if err != nil {
		m.log.WithError(err).Warn("Failed to allocate NAS client")
	} else {
		m.log.Debug("Allocated NAS client")
	}
	time.Sleep(500 * time.Millisecond)

	// DMS
	m.log.Debug("Allocating DMS client...")
	m.dms, err = qmi.NewDMSService(m.client)
	if err != nil {
		m.log.WithError(err).Warn("Failed to allocate DMS client")
	} else {
		m.log.Debug("Allocated DMS client")
	}
	time.Sleep(500 * time.Millisecond)

	// UIM
	m.log.Debug("Allocating UIM client...")
	m.uim, err = qmi.NewUIMService(m.client)
	if err != nil {
		m.log.WithError(err).Warn("Failed to allocate UIM client")
	} else {
		m.log.Debug("Allocated UIM client")
	}

	// WDA (Backup/Optional) / WDA服务 (备份/可选)
	m.log.Debug("Allocating WDA client...")
	m.wda, err = qmi.NewWDAService(m.client)
	if err != nil {
		m.log.WithError(err).Warn("Failed to allocate WDA client")
	} else {
		m.log.Debug("Allocated WDA client")

		// 尝试启用 RawIP 模式 (Modern 4G/5G modems usually require this)
		if err := m.enableRawIP(); err != nil {
			m.log.WithError(err).Warn("Failed to enable RawIP mode, falling back to 802.3")
		}
	}

	// WMS (SMS)
	m.log.Debug("Allocating WMS client...")
	m.wms, err = qmi.NewWMSService(m.client)
	if err != nil {
		m.log.WithError(err).Warn("Failed to allocate WMS client")
	} else {
		m.log.Debug("Allocated WMS client")
		// Enable SMS indications unless disabled / 开启短信指示 (除非禁用)
		if !m.cfg.DisableWMSInd {
			if err := m.wms.RegisterEventReport(context.Background()); err != nil {
				m.log.WithError(err).Warn("Failed to register SMS indications")
			}
		} else {
			m.log.Debug("WMS indications disabled by config")
		}
	}

	return nil
}

// enableRawIP enables RawIP mode on both the modem (WDA) and the kernel interface / 启用RawIP模式：同时在Modem(WDA)和内核接口上启用
func (m *Manager) enableRawIP() error {
	if m.wda == nil {
		return fmt.Errorf("WDA service not available")
	}

	// 1. Kernel Check (Linux Only) / 1. 内核检查 (仅限Linux)
	// On Windows/Darwin, we don't have sysfs qmi/raw_ip, so we might skip kernel part / 在Windows/Darwin上，没有sysfs qmi/raw_ip，所以跳过内核部分
	// or assume the driver handles it differently. / 或者假设驱动程序以不同方式处理。
	isLinux := runtime.GOOS == "linux"
	ifname := m.cfg.Device.NetInterface
	sysfsPath := filepath.Join("/sys/class/net", ifname, "qmi/raw_ip")
	kernelEnabled := false

	if isLinux {
		// Check if raw_ip sysfs attribute exists / 检查 raw_ip sysfs 属性是否存在
		if _, err := os.Stat(sysfsPath); os.IsNotExist(err) {
			// Not supported by kernel driver, skip / 内核驱动不支持，跳过
			m.log.Warn("Kernel driver does not support raw_ip (sysfs entry missing), skipping kernel config")
		} else {
			// Optimization: Check if already enabled in Kernel / 优化：检查内核中是否已启用
			if content, err := os.ReadFile(sysfsPath); err == nil {
				s := string(content)
				if len(s) > 0 && (s[0] == 'Y' || s[0] == 'y' || s[0] == '1') {
					kernelEnabled = true
				}
			}
		}
	} else {
		// Non-Linux platforms: Assume kernel/driver doesn't need manual raw_ip toggle via sysfs / 非Linux平台：假设内核/驱动不需要通过sysfs手动切换raw_ip
		// or it's always enabled/handled by driver. / 或者它总是由驱动程序启用/处理。
		// We still proceed to configure the Modem, as that's platform independent QMI. / 我们仍然继续配置Modem，因为那是与平台无关的QMI。
		kernelEnabled = true // Treat as "done" for the purpose of the combined check / 将其视为“已完成”以进行组合检查
	}

	// Optimization: Check if already enabled in Modem (if WDA available) / 优化：检查Modem中是否已启用 (如果WDA可用)
	modemEnabled := false
	if currentFormat, err := m.wda.GetDataFormat(context.Background()); err == nil {
		if currentFormat.LinkProtocol == qmi.LinkProtocolIP {
			modemEnabled = true
		}
	} else {
		m.log.WithError(err).Debug("Failed to get current data format, assuming mismatch")
	}

	if kernelEnabled && modemEnabled {
		m.log.Info("Raw IP mode already enabled, skipping configuration")
		return nil
	}

	// 2. Set Modem Data Format to Raw IP / 2. 将Modem数据格式设置为Raw IP
	m.log.Info("Setting modem data format to Raw IP...")
	format := qmi.DataFormat{
		LinkProtocol:      qmi.LinkProtocolIP, // 0x02 = Raw IP
		UlDataAggregation: uint32(qmi.DataFormatUlDataAggDisabled),
		DlDataAggregation: uint32(qmi.DataFormatDlDataAggDisabled),
	}
	if err := m.wda.SetDataFormat(context.Background(), format); err != nil {
		m.log.WithError(err).Warn("Failed to set modem data format to Raw IP (might already be set or not supported), continuing to force kernel...")
	} else {
		m.log.Info("Modem data format set to Raw IP")
	}

	// 3. Enable Raw IP in kernel (Linux Only) / 3. 在内核中启用Raw IP (仅限Linux)
	if isLinux && !kernelEnabled {
		// Check again if file exists before writing / 在写入之前再次检查文件是否存在
		if _, err := os.Stat(sysfsPath); os.IsNotExist(err) {
			return nil // Skip if not supported / 如果不支持则跳过
		}

		m.log.Info("Enabling Raw IP in kernel...")

		// Ensure interface is down before changing mode (sometimes required) / 确保在更改模式之前接口已关闭 (有时是必需的)
		if err := netcfg.BringDown(ifname); err != nil {
			m.log.WithError(err).Warn("Failed to bring down interface for Raw IP switch")
		}

		if err := os.WriteFile(sysfsPath, []byte("Y"), 0644); err != nil {
			// Try 'Y' with newline just in case / 尝试带换行符的 'Y' 以防万一
			if err2 := os.WriteFile(sysfsPath, []byte("Y\n"), 0644); err2 != nil {
				return fmt.Errorf("failed to write to raw_ip sysfs: %w", err)
			}
		}

		// Bring interface back up? configureNetwork will do it later. / 重新启动接口？ configureNetwork稍后会做。
		m.log.Info("Raw IP mode enabled successfully in kernel")
	}

	return nil
}

func (m *Manager) checkSIM() error {
	status := qmi.SIMAbsent
	var err error

	// Try UIM service first (modern modems) / 优先尝试UIM服务 (现代modem)
	if m.uim != nil {
		status, err = m.uim.GetCardStatus(context.Background())
		if err == nil {
			m.log.Infof("SIM status (UIM): %s", status)
		}
	}

	// Fallback to DMS if UIM failed or not ready / 如果UIM失败或未就绪，回退到DMS
	if (err != nil || status != qmi.SIMReady) && m.dms != nil {
		dmsStatus, dmsErr := m.dms.GetSIMStatus(context.Background())
		if dmsErr == nil {
			status = dmsStatus
			m.log.Infof("SIM status (DMS): %s", status)
		} else if err == nil {
			err = dmsErr
		}
	}

	if err != nil {
		return err
	}

	if status == qmi.SIMPINRequired && m.cfg.PINCode != "" {
		m.log.Info("Verifying PIN...")
		if err := m.dms.VerifyPIN(context.Background(), m.cfg.PINCode); err != nil {
			return fmt.Errorf("PIN verification failed: %w", err)
		}
		m.log.Info("PIN verified successfully")
	}

	return nil
}

func (m *Manager) cleanup() {
	// Use timeout context for cleanup operations / 使用超时上下文进行清理操作
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	m.mu.Lock()
	wds := m.wds
	wdsV6 := m.wdsV6
	nas := m.nas
	dms := m.dms
	uim := m.uim
	wda := m.wda
	wms := m.wms
	client := m.client
	handleV4 := m.handleV4
	handleV6 := m.handleV6
	ifname := m.cfg.Device.NetInterface

	muxIface := m.muxIface
	muxID := m.cfg.MuxID
	masterIface := m.cfg.Device.NetInterface

	m.wds = nil
	m.wdsV6 = nil
	m.nas = nil
	m.dms = nil
	m.uim = nil
	m.wda = nil
	m.wms = nil
	m.client = nil
	m.handleV4 = 0
	m.handleV6 = 0
	m.settings = nil
	m.muxIface = ""
	m.mu.Unlock()

	// 清理 QMAP 虚拟网卡
	if muxIface != "" && muxID > 0 {
		go func() {
			netcfg.FlushAddresses(muxIface)
			netcfg.BringDown(muxIface)
			netcfg.DelQMAPMux(masterIface, muxID)
		}()
	}

	// Disconnect data call with timeout / 带超时断开数据呼叫
	if handleV4 != 0 && wds != nil {
		go func() {
			wds.StopNetworkInterface(cleanupCtx, handleV4)
		}()
	}
	if handleV6 != 0 && wdsV6 != nil {
		go func() {
			wdsV6.StopNetworkInterface(cleanupCtx, handleV6)
		}()
	}

	// Flush network config (non-blocking, ignore errors) / 清除网络配置 (非阻塞，忽略错误)
	go func() {
		netcfg.FlushAddresses(ifname)
		netcfg.BringDown(ifname)
	}()

	// Wait a bit for async cleanup, but don't block / 等待异步清理，但不阻塞
	time.Sleep(100 * time.Millisecond)

	// Release clients / 释放客户端
	if wds != nil {
		wds.Close()
	}
	if wdsV6 != nil {
		wdsV6.Close()
	}
	if nas != nil {
		nas.Close()
	}
	if dms != nil {
		dms.Close()
	}
	if uim != nil {
		uim.Close()
	}
	if wda != nil {
		wda.Close()
	}
	if wms != nil {
		wms.Close()
	}

	if client != nil {
		client.Close()
	}
}

// ============================================================================
// Event Loop / 事件循环
// ============================================================================

func (m *Manager) eventLoop() {
	defer m.wg.Done()

	checkTicker := time.NewTicker(15 * time.Second)
	defer checkTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return

		case evt := <-m.eventCh:
			m.handleEvent(evt)

		case <-checkTicker.C:
			m.eventCh <- eventCheck
		}
	}
}

func (m *Manager) handleEvent(evt internalEvent) {
	switch evt {
	case eventStart:
		m.doConnect()

	case eventStop:
		m.doDisconnect()

	case eventCheck:
		m.doStatusCheck()

	case eventPacketStatusChanged, eventServingSystemChanged:
		m.log.Debug("Received indication - checking status")
		m.doStatusCheck()

	case eventModemReset:
		m.log.Warn("Modem reset detected!")
		m.doRecoverFromModemReset()
	}
}

func (m *Manager) openClientAndAllocateServices() error {
	if runtime.GOOS == "linux" {
		rawIPPath := filepath.Join("/sys/class/net", m.cfg.Device.NetInterface, "qmi/raw_ip")
		if b, err := os.ReadFile(rawIPPath); err == nil && len(b) > 0 {
			if b[0] != 'Y' && b[0] != 'y' && b[0] != '1' {
				if err := os.WriteFile(rawIPPath, []byte("Y"), 0); err != nil {
					m.log.WithError(err).Warn("Failed to enable kernel raw_ip")
				}
			}
		}
	}

	const maxAttempts = 4
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		client, err := qmi.NewClient(m.cfg.Device.ControlPath)
		if err != nil {
			lastErr = fmt.Errorf("failed to open QMI device: %w", err)
		} else {
			m.mu.Lock()
			m.client = client
			m.mu.Unlock()

			err = m.allocateServices()
			if err == nil {
				return nil
			}
			lastErr = err

			client.Close()
			m.mu.Lock()
			if m.client == client {
				m.client = nil
			}
			m.mu.Unlock()
		}

		var timeoutErr *qmi.TimeoutError
		shouldRetry := errors.As(lastErr, &timeoutErr) || errors.Is(lastErr, context.DeadlineExceeded)
		if !shouldRetry || attempt == maxAttempts {
			return lastErr
		}

		delay := time.Duration(attempt) * 2 * time.Second
		m.log.WithError(lastErr).Warnf("QMI init failed, retrying in %v (%d/%d)", delay, attempt, maxAttempts)
		time.Sleep(delay)
	}

	return lastErr
}

func (m *Manager) doRecoverFromModemReset() {
	m.doDisconnect()
	m.cleanup()
	m.mu.Lock()
	m.settings = nil
	m.mu.Unlock()

	if err := m.openClientAndAllocateServices(); err != nil {
		m.log.WithError(err).Warn("Failed to reinitialize QMI after modem reset")
		m.setState(StateDisconnected)
		m.recoverCount++
		if m.cfg.AutoReconnect {
			delay := m.getRecoverDelay()
			m.log.Infof("Will retry reinit in %v (%d/%d)", delay, m.recoverCount, len(m.retryDelays))
			time.AfterFunc(delay, func() {
				m.eventCh <- eventModemReset
			})
		}
		return
	}
	m.recoverCount = 0

	if err := m.checkSIM(); err != nil {
		m.log.WithError(err).Warn("SIM check failed after modem reset")
	}

	m.setState(StateDisconnected)
	if m.cfg.AutoReconnect {
		time.AfterFunc(2*time.Second, func() {
			m.eventCh <- eventStart
		})
	}
}

func (m *Manager) getRecoverDelay() time.Duration {
	if m.recoverCount <= 0 {
		return m.retryDelays[0]
	}
	idx := m.recoverCount - 1
	if idx < len(m.retryDelays) {
		return m.retryDelays[idx]
	}
	return m.retryDelays[len(m.retryDelays)-1]
}

func (m *Manager) doConnect() {
	m.mu.Lock()
	if m.state == StateConnected || m.state == StateStopping {
		m.mu.Unlock()
		return
	}
	if m.state == StateConnecting && m.handleV4 != 0 {
		m.mu.Unlock()
		return
	}
	m.state = StateConnecting
	m.mu.Unlock()

	if m.wds == nil && m.wdsV6 == nil {
		m.log.Error("WDS service not available")
		m.setState(StateDisconnected)
		return
	}

	// ========== 多路拨号 (QMAP) 准备 ==========
	if m.cfg.MuxID > 0 {
		masterIface := m.cfg.Device.NetInterface
		m.log.Infof("多路拨号模式: MuxID=%d, ProfileIndex=%d, 物理网卡=%s",
			m.cfg.MuxID, m.cfg.ProfileIndex, masterIface)

		// 1. 确保 Raw IP 模式已开启
		if err := netcfg.EnableRawIP(masterIface); err != nil {
			m.log.WithError(err).Warn("开启 Raw IP 模式失败")
		}

		// 2. 创建 QMAP 虚拟网卡 (如果不存在)
		muxIfname, err := netcfg.AddQMAPMux(masterIface, m.cfg.MuxID)
		if err != nil {
			m.log.WithError(err).Errorf("创建 MUX ID=%d 虚拟网卡失败", m.cfg.MuxID)
			// 继续尝试，也许用户已手动创建
		} else {
			m.log.Infof("QMAP 虚拟网卡: %s (MuxID=%d)", muxIfname, m.cfg.MuxID)
			m.mu.Lock()
			m.muxIface = muxIfname
			m.mu.Unlock()
		}

		// 3. 绑定 WDS Client 到 Mux Data Port
		binding := qmi.MuxBinding{
			EpType:     0x02, // HSUSB
			EpIfID:     0x04, // 默认 Interface ID
			MuxID:      m.cfg.MuxID,
			ClientType: 1, // Tethered
		}
		if m.wds != nil {
			if err := m.wds.BindMuxDataPort(context.Background(), binding); err != nil {
				m.log.WithError(err).Error("WDS IPv4 BindMuxDataPort 失败")
				// 非致命，继续
			} else {
				m.log.Infof("WDS IPv4 已绑定 MuxID=%d", m.cfg.MuxID)
			}
		}

		// 如果有 IPv6 WDS，也需要绑定
		if m.wdsV6 != nil {
			if err := m.wdsV6.BindMuxDataPort(context.Background(), binding); err != nil {
				m.log.WithError(err).Warn("WDS IPv6 BindMuxDataPort 失败")
			} else {
				m.log.Infof("WDS IPv6 已绑定 MuxID=%d", m.cfg.MuxID)
			}
		}
	}

	// 设置 ProfileIndex (多路模式和非多路模式都可用)
	if m.cfg.ProfileIndex > 0 {
		if m.wds != nil {
			m.wds.ProfileIndex = m.cfg.ProfileIndex
		}
		if m.wdsV6 != nil {
			m.wdsV6.ProfileIndex = m.cfg.ProfileIndex
		}
		m.log.Infof("使用 Profile Index=%d", m.cfg.ProfileIndex)
	}

	// Log current signal and registration for context / 记录当前信号和注册状态以便调试
	if m.nas != nil {
		if sig, err := m.nas.GetSignalStrength(context.Background()); err == nil {
			m.log.Infof("Signal: RSSI=%d, RSRP=%d, RSRQ=%d", sig.RSSI, sig.RSRP, sig.RSRQ)
		}
		if ss, err := m.nas.GetServingSystem(context.Background()); err == nil {
			m.log.Infof("Network: %s (%d-%d) Tech:%d", ss.RegistrationState, ss.MCC, ss.MNC, ss.RadioInterface)
		}
	}

	// Check registration / 检查注册状态
	if m.nas != nil {
		registered, _ := m.nas.IsRegistered(context.Background())
		if !registered {
			m.log.Info("Waiting for network registration...")
			// Don't fail - continue and let the dial fail if not registered / 不报错 - 继续执行，让拨号过程去处理未注册的情况
		}
	}

	// Start IPv4 data call / 启动IPv4数据呼叫
	if m.cfg.EnableIPv4 {
		m.log.Info("Starting IPv4 data call...")
		handle, err := m.wds.StartNetworkInterface(context.Background(),
			m.cfg.APN, m.cfg.Username, m.cfg.Password, m.cfg.AuthType, qmi.IpFamilyV4)
		if err != nil {
			m.log.WithError(err).Error("IPv4 dial failed")
			m.handleDialFailure()
			return
		}
		m.handleV4 = handle
		m.log.Infof("IPv4 connected, handle=0x%08x", handle)
	}

	// Start IPv6 data call / 启动IPv6数据呼叫
	if m.cfg.EnableIPv6 && m.wdsV6 != nil {
		m.log.Info("Starting IPv6 data call...")
		handle, err := m.wdsV6.StartNetworkInterface(context.Background(),
			m.cfg.APN, m.cfg.Username, m.cfg.Password, m.cfg.AuthType, qmi.IpFamilyV6)
		if err != nil {
			m.log.WithError(err).Warn("IPv6 dial failed")
			// Continue with IPv4 only
		} else {
			m.handleV6 = handle
			m.log.Infof("IPv6 connected, handle=0x%08x", handle)
		}
	}

	// Get IP settings and configure interface / 获取IP设置并配置接口
	if err := m.configureNetwork(); err != nil {
		m.log.WithError(err).Error("Network configuration failed")
		m.handleDialFailure()
		return
	}

	m.setState(StateConnected)
	m.retryCount = 0
	m.log.Info("Connection established successfully!")

	// Emit connected event / 发送连接事件
	if m.events != nil {
		m.mu.RLock()
		settings := m.settings
		m.mu.RUnlock()
		m.events.Emit(Event{Type: EventConnected, State: StateConnected, Settings: settings})
	}
}

func (m *Manager) configureNetwork() error {
	// 多路拨号模式下，IP/DNS/Route 配置在虚拟网卡上
	ifname := m.cfg.Device.NetInterface
	m.mu.RLock()
	if m.muxIface != "" {
		ifname = m.muxIface
	}
	m.mu.RUnlock()
	m.log.Infof("Configuring network interface %s...", ifname)

	// 多路拨号时也要确保物理网卡是 up 的
	if m.muxIface != "" && ifname != m.cfg.Device.NetInterface {
		if err := netcfg.BringUp(m.cfg.Device.NetInterface); err != nil {
			m.log.WithError(err).Warn("Failed to bring master interface up")
		}
	}

	// Bring interface up / 启动接口
	if err := netcfg.BringUp(ifname); err != nil {
		m.log.WithError(err).Warn("Failed to bring interface up")
	}

	// 1. IPv4 Configuration / 1. IPv4配置
	if m.wds != nil {
		m.log.Debug("Querying IPv4 runtime settings...")
		settings, err := m.wds.GetRuntimeSettings(context.Background(), qmi.IpFamilyV4)
		if err != nil {
			m.log.WithError(err).Warn("Failed to get IPv4 settings")
		} else {
			m.mu.Lock()
			m.settings = settings
			m.mu.Unlock()

			if settings.IPv4Address != nil {
				prefix, _ := settings.IPv4Subnet.Size()
				if prefix == 0 {
					prefix = 32
				}
				m.log.Infof("Configuring IPv4: %s/%d via %s (DNS: %v, %v)",
					settings.IPv4Address, prefix, settings.IPv4Gateway,
					settings.IPv4DNS1, settings.IPv4DNS2)

				if err := netcfg.SetIPAddress(ifname, settings.IPv4Address, prefix); err != nil {
					m.log.WithError(err).Error("Failed to set IPv4 address")
				}

				// Add default route (unless disabled) / 添加默认路由 (除非被禁用)
				if !m.cfg.NoRoute {
					if settings.IPv4Gateway != nil && !settings.IPv4Gateway.Equal(net.IPv4zero) {
						m.log.Infof("Adding IPv4 route via %s", settings.IPv4Gateway)
						if err := netcfg.AddDefaultRoute(ifname, settings.IPv4Gateway); err != nil {
							m.log.WithError(err).Error("Failed to add IPv4 default route")
						}
					} else {
						m.log.Info("Adding direct IPv4 default route")
						netcfg.AddDefaultRouteDirect(ifname, false)
					}
				} else {
					m.log.Info("Skipping default route (--no-route)")
				}

				if !m.cfg.NoDNS {
					dns1 := ""
					dns2 := ""
					if settings.IPv4DNS1 != nil {
						dns1 = settings.IPv4DNS1.String()
					}
					if settings.IPv4DNS2 != nil {
						dns2 = settings.IPv4DNS2.String()
					}
					if dns1 != "" {
						m.log.Infof("Configuring DNS: %s, %s", dns1, dns2)
						netcfg.UpdateResolvConf(dns1, dns2)
					}
				} else {
					m.log.Info("Skipping DNS configuration (--no-dns)")
				}

				// Set MTU
				if settings.MTU > 0 {
					m.log.Infof("Setting MTU: %d", settings.MTU)
					netcfg.SetMTU(ifname, int(settings.MTU))
				}
			}
		}
	}

	// 2. IPv6 Configuration / 2. IPv6配置
	if m.wdsV6 != nil {
		m.log.Debug("Querying IPv6 runtime settings...")
		settingsV6, err := m.wdsV6.GetRuntimeSettings(context.Background(), qmi.IpFamilyV6)
		if err != nil {
			m.log.WithError(err).Warn("Failed to get IPv6 settings")
		} else {
			if settingsV6.IPv6Address != nil {
				m.log.Infof("Configuring IPv6: %s/%d", settingsV6.IPv6Address, settingsV6.IPv6Prefix)
				if err := netcfg.SetIPv6Address(ifname, settingsV6.IPv6Address, int(settingsV6.IPv6Prefix)); err != nil {
					m.log.WithError(err).Error("Failed to set IPv6 address")
				}
				if !m.cfg.NoRoute {
					if settingsV6.IPv6Gateway != nil {
						m.log.Infof("Adding IPv6 route via %s", settingsV6.IPv6Gateway)
						netcfg.AddDefaultRoute(ifname, settingsV6.IPv6Gateway)
					} else {
						m.log.Info("Adding direct IPv6 default route")
						netcfg.AddDefaultRouteDirect(ifname, true)
					}
				}
			}
		}
	}

	// Final check: ensure up / 最后检查: 确保接口已启动
	netcfg.BringUp(ifname)
	m.log.Info("Network configuration completed")
	return nil
}

func (m *Manager) doDisconnect() {
	m.log.Info("Disconnecting...")

	if m.handleV4 != 0 && m.wds != nil {
		m.wds.StopNetworkInterface(context.Background(), m.handleV4)
		m.handleV4 = 0
	}
	if m.handleV6 != 0 && m.wdsV6 != nil {
		m.wdsV6.StopNetworkInterface(context.Background(), m.handleV6)
		m.handleV6 = 0

	}

	netcfg.FlushAddresses(m.cfg.Device.NetInterface)
	netcfg.FlushRoutes(m.cfg.Device.NetInterface)
	netcfg.BringDown(m.cfg.Device.NetInterface)

	m.setState(StateDisconnected)

	// Emit disconnected event / 发送断开连接事件
	if m.events != nil {
		m.events.Emit(Event{Type: EventDisconnected, State: StateDisconnected})
	}
}

func (m *Manager) doStatusCheck() {
	m.mu.RLock()
	if m.isRotating {
		m.mu.RUnlock()
		return // Skip check during rotation / 轮换期间跳过检查
	}
	currentState := m.state
	m.mu.RUnlock()

	if currentState == StateStopping || currentState == StateDisconnected {
		return
	}

	if m.client == nil {
		return
	}

	// 1. Log Signal Strength & Registration / 1. 记录信号强度和注册状态
	if m.nas != nil {
		sig, err := m.nas.GetSignalStrength(context.Background())
		if err == nil {
			m.log.Infof("Signal: RSSI=%d, RSRP=%d, RSRQ=%d", sig.RSSI, sig.RSRP, sig.RSRQ)
		}
		ss, err := m.nas.GetServingSystem(context.Background())
		if err == nil {
			m.log.Infof("Network: %s (MCC:%d MNC:%d) Tech:%d", ss.RegistrationState, ss.MCC, ss.MNC, ss.RadioInterface)
		}
	}

	// 2. Query connection status / 2. 查询连接状态
	if m.wds == nil {
		return
	}

	status, err := m.wds.GetPacketServiceStatus(context.Background())
	if err != nil {
		m.log.WithError(err).Debug("Status query failed")
		return
	}

	if status == qmi.StatusConnected {
		if currentState != StateConnected {
			m.log.Info("Connection restored")
			m.configureNetwork()
			m.setState(StateConnected)
			m.retryCount = 0
		} else {
			// Smart Check: Verify IP consistency (match C version) / 智能检查: 验证IP一致性 (匹配C版本逻辑)
			if err := m.verifyIPConsistency(); err != nil {
				m.log.WithError(err).Warn("IP consistency check failed - triggering redial")
				m.doDisconnect()
				m.mu.RLock()
				isStopping := m.state == StateStopping
				m.mu.RUnlock()
				if m.cfg.AutoReconnect && !isStopping {
					m.eventCh <- eventStart
				}
			}
		}
	} else if status == qmi.StatusDisconnected {
		if currentState == StateConnected {
			m.log.Warn("Connection lost!")
			m.handleV4 = 0
			netcfg.FlushAddresses(m.cfg.Device.NetInterface)
			m.setState(StateDisconnected)

			// Trigger reconnect
			m.mu.RLock()
			isStopping := m.state == StateStopping
			m.mu.RUnlock()
			if m.cfg.AutoReconnect && !isStopping {
				m.eventCh <- eventStart
			}
		}
	}
}

func (m *Manager) verifyIPConsistency() error {
	if m.wds == nil || m.settings == nil {
		return nil
	}

	// Get fresh settings from modem / 从 modem获取最新设置
	newSettings, err := m.wds.GetRuntimeSettings(context.Background(), qmi.IpFamilyV4)
	if err != nil {
		return err
	}

	// Compare with recorded IP / 与记录的IP进行比较
	if !newSettings.IPv4Address.Equal(m.settings.IPv4Address) {
		return fmt.Errorf("local IP %s != modem IP %s", m.settings.IPv4Address, newSettings.IPv4Address)
	}

	return nil
}

func (m *Manager) handleDialFailure() {
	m.setState(StateDisconnected)

	if !m.cfg.AutoReconnect {
		return
	}

	delay := m.getRetryDelay()
	m.retryCount++
	if m.retryCount == 3 {
		go m.RadioReset()
	}
	m.log.Infof("Will retry in %v (%d/%d)", delay, m.retryCount, len(m.retryDelays))

	time.AfterFunc(delay, func() {
		m.eventCh <- eventStart
	})
}

func (m *Manager) getRetryDelay() time.Duration {
	if m.retryCount < len(m.retryDelays) {
		return m.retryDelays[m.retryCount]
	}
	return m.retryDelays[len(m.retryDelays)-1]
}

// ============================================================================
// Indication Handler
// ============================================================================

func (m *Manager) indicationHandler() {
	defer m.wg.Done()

	for {
		if m.ctx.Err() != nil {
			return
		}

		m.mu.RLock()
		client := m.client
		m.mu.RUnlock()

		if client == nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		eventsCh := client.Events()
	readEvents:
		for {
			select {
			case <-m.ctx.Done():
				return
			case evt, ok := <-eventsCh:
				if !ok {
					time.Sleep(200 * time.Millisecond)
					break readEvents
				}
				m.handleIndication(evt)
			}
		}
	}
}

func (m *Manager) handleIndication(evt qmi.Event) {
	m.log.Debugf("Indication: type=%d service=0x%02x msg=0x%04x", evt.Type, evt.ServiceID, evt.MessageID)

	switch evt.Type {
	case qmi.EventPacketServiceStatusChanged:
		m.eventCh <- eventPacketStatusChanged

	case qmi.EventServingSystemChanged:
		// Decode registration state if possible / 如果可能，解码注册状态
		if tlv := qmi.FindTLV(evt.Packet.TLVs, 0x01); tlv != nil && len(tlv.Value) >= 1 {
			state := qmi.RegistrationState(tlv.Value[0])
			m.mu.Lock()
			notify := m.regNotify
			m.mu.Unlock()
			if state == qmi.RegStateRegistered && notify != nil {
				select {
				case notify <- true:
				default:
				}
			}
		}
		m.eventCh <- eventServingSystemChanged

	case qmi.EventModemReset:
		m.eventCh <- eventModemReset

	case qmi.EventNewMessage:
		m.log.Info("New SMS Indication received")
		// TLV 0x10 usually has index and storage (GW)
		if tlv := qmi.FindTLV(evt.Packet.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 5 {
			index := binary.LittleEndian.Uint32(tlv.Value[1:5])
			if m.events != nil {
				m.events.Emit(Event{Type: EventNewSMS, SMSIndex: index})
			}
		} else if tlv := qmi.FindTLV(evt.Packet.TLVs, 0x11); tlv != nil && len(tlv.Value) >= 5 {
			// Fallback to TLV 0x11
			index := binary.LittleEndian.Uint32(tlv.Value[1:5])
			if m.events != nil {
				m.events.Emit(Event{Type: EventNewSMS, SMSIndex: index})
			}
		} else {
			// Just emit event without index if TLV missing
			if m.events != nil {
				m.events.Emit(Event{Type: EventNewSMS, SMSIndex: 0xFFFFFFFF})
			}
		}
	}
}

// ============================================================================
// Radio Reset Recovery
// ============================================================================

// RadioReset performs a radio power cycle to recover from stuck states / 射频重置: 执行射频电源循环以从卡死状态恢复
func (m *Manager) RadioReset() error {
	if m.dms == nil {
		return fmt.Errorf("DMS service not available")
	}

	m.log.Info("Performing radio reset...")

	// Turn radio off / 关闭射频
	if err := m.dms.RadioPower(context.Background(), false); err != nil {
		return fmt.Errorf("failed to turn radio off: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Turn radio on / 打开射频
	if err := m.dms.RadioPower(context.Background(), true); err != nil {
		return fmt.Errorf("failed to turn radio on: %w", err)
	}

	m.log.Info("Radio reset completed")
	return nil
}

// ============================================================================
// SMS Methods / 短信方法
// ============================================================================

// ListSMS lists SMS messages from the specified storage (0=UIM, 1=NV) / ListSMS 从指定的存储中列出短信 (0=UIM, 1=NV)
func (m *Manager) ListSMS(storageType uint8, tag qmi.MessageTagType) ([]struct {
	Index uint32
	Tag   qmi.MessageTagType
}, error) {
	if m.wms == nil {
		return nil, fmt.Errorf("WMS service not initialized")
	}
	return m.wms.ListMessages(context.Background(), storageType, tag)
}

// ReadRawSMS reads a raw SMS message PDU / ReadRawSMS 读取原始短信 PDU
func (m *Manager) ReadRawSMS(storageType uint8, index uint32) ([]byte, error) {
	if m.wms == nil {
		return nil, fmt.Errorf("WMS service not initialized")
	}
	return m.wms.RawReadMessage(context.Background(), storageType, index)
}

// DecodedSMS represents a decoded SMS message / DecodedSMS 代表解码后的短信
type DecodedSMS struct {
	Index     uint32
	Storage   uint8
	Sender    string
	Message   string
	Timestamp time.Time

	// Concat info
	IsConcat    bool
	ConcatRef   int
	ConcatTotal int
	ConcatSeq   int
}

// ReadSMS reads and decodes an SMS message / ReadSMS 读取并解码短信
func (m *Manager) ReadSMS(storageType uint8, index uint32) (*DecodedSMS, error) {
	raw, err := m.ReadRawSMS(storageType, index)
	if err != nil {
		return nil, err
	}

	// QMI usually returns [SMSC_Len(1)] + [SMSC(N)] + [TPDU(M)]
	if len(raw) < 1 {
		return nil, fmt.Errorf("PDU too short")
	}
	smscLen := int(raw[0])
	tpduOffset := 1 + smscLen
	if tpduOffset > len(raw) {
		return nil, fmt.Errorf("invalid PDU: SMSC length mismatch")
	}

	pd := &tpdu.TPDU{}
	if err := pd.UnmarshalBinary(raw[tpduOffset:]); err != nil {
		return nil, fmt.Errorf("PDU unmarshal failed: %w", err)
	}

	// Decode message text (handles GSM7, UCS2 etc.)
	// Decode takes a slice of *tpdu.TPDU to handle reassembly of multi-part messages
	textBytes, err := sms.Decode([]*tpdu.TPDU{pd})
	if err != nil {
		return nil, fmt.Errorf("PDU text decode failed: %w", err)
	}

	resp := &DecodedSMS{
		Index:     index,
		Storage:   storageType,
		Sender:    pd.OA.Number(),
		Message:   string(textBytes),
		Timestamp: pd.SCTS.Time,
	}

	// Parse UDH for concat info
	for _, ie := range pd.UDH {
		// 0x00: Concat 8-bit, 0x08: Concat 16-bit
		if ie.ID == 0x00 && len(ie.Data) >= 3 {
			resp.IsConcat = true
			resp.ConcatRef = int(ie.Data[0])
			resp.ConcatTotal = int(ie.Data[1])
			resp.ConcatSeq = int(ie.Data[2])
			break
		} else if ie.ID == 0x08 && len(ie.Data) >= 4 {
			resp.IsConcat = true
			resp.ConcatRef = int(ie.Data[0])<<8 | int(ie.Data[1])
			resp.ConcatTotal = int(ie.Data[2])
			resp.ConcatSeq = int(ie.Data[3])
			break
		}
	}

	return resp, nil
}

// SendRawSMS sends a raw SMS PDU / SendRawSMS 发送原始短信 PDU
func (m *Manager) SendRawSMS(format uint8, pdu []byte) error {
	if m.wms == nil {
		return fmt.Errorf("WMS service not initialized")
	}
	return m.wms.SendRawMessage(context.Background(), format, pdu)
}

// SendSMS sends a text message / SendSMS 发送文本短信
func (m *Manager) SendSMS(number, text string) error {
	if m.wms == nil {
		return fmt.Errorf("WMS service not initialized")
	}

	pdu, err := m.encodeSMS(number, text)
	if err != nil {
		return err
	}

	return m.wms.SendRawMessage(context.Background(), 0x06, pdu)
}

// encodeSMS encodes a text message into a 7-bit PDU format using warthog618/sms / encodeSMS 使用 warthog618/sms 将文本消息编码为 7-bit PDU 格式
func (m *Manager) encodeSMS(number, text string) ([]byte, error) {
	// Destination number should be in international format for better compatibility
	options := []sms.EncoderOption{sms.AsSubmit, sms.To(number)}

	pdus, err := sms.Encode([]byte(text), options...)
	if err != nil {
		return nil, err
	}
	if len(pdus) == 0 {
		return nil, fmt.Errorf("no PDUs generated")
	}

	// Marshal the first PDU segment back to binary for QMI
	binaryTPDU, err := pdus[0].MarshalBinary()
	if err != nil {
		return nil, err
	}

	// QMI WMSRawSend expects: [SMSC_Len(1)] + [TPDU]
	// 0x00 means use the default SMSC stored in the SIM/modem
	pduWithSMSC := append([]byte{0x00}, binaryTPDU...)
	return pduWithSMSC, nil
}
