package manager

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/netcfg"
	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
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
	Device          ModemDevice // Modem device info / Modem 设备信息
	APN             string      // APN (Access Point Name) / APN（接入点名称）
	Username        string      // Authentication username / 认证用户名
	Password        string      // Authentication password / 认证密码
	AuthType        uint8       // 0=none, 1=PAP, 2=CHAP, 3=PAP|CHAP / 认证类型
	EnableIPv4      bool        // Enable IPv4 / 启用 IPv4
	EnableIPv6      bool        // Enable IPv6 / 启用 IPv6
	PINCode         string      // SIM PIN code / SIM 卡 PIN 码
	AutoReconnect   bool        // Automatically reconnect on disconnect / 断开后自动重连
	NoRoute         bool        // Don't add default route (useful for debugging) / 不添加默认路由 (用于调试)
	NoDNS           bool        // Don't configure DNS (useful for debugging) / 不配置DNS (用于调试)
	DisableWMSInd   bool        // Disable WMS indications (Event Report) / 禁用 WMS 指示 (事件报告)
	DisableIMSAInd  bool        // Disable IMSA indications / 禁用 IMSA 指示
	DisableVOICEInd bool        // Disable VOICE indications / 禁用 VOICE 指示

	ProfileIndex uint8 // PDN Profile 索引 (对应 -n 参数, 默认 0 表示使用模组默认 Profile)
	MuxID        uint8 // QMAP Mux ID (对应 -m 参数, 默认 0 表示不启用多路复用)
	NoDial       bool  // Only open QMI services, don't perform WDS dialing / 仅打开 QMI 服务, 不进行 WDS 拨号

	Timeouts      TimeoutConfig
	RetryPolicy   RetryPolicy
	HealthPolicy  HealthPolicy
	EventPolicy   EventPolicy
	ClientOptions qmi.ClientOptions
}

type TimeoutConfig struct {
	Init               time.Duration
	Dial               time.Duration
	SIMCheck           time.Duration
	StatusCheck        time.Duration
	Stop               time.Duration
	IndicationRegister time.Duration
}

type RetryPolicy struct {
	ReconnectDelays []time.Duration
	ReinitDelays    []time.Duration
	RadioResetAfter int
}

type HealthPolicy struct {
	FullCheckInterval     time.Duration
	IndicationDebounce    time.Duration
	IPConsistencyInterval time.Duration
}

type EventPolicy struct {
	CallbackQueueSize int
}

type ManagerStats struct {
	StatusChecks       uint64
	DebouncedChecks    uint64
	ReconnectScheduled uint64
	StaleTimerIgnored  uint64
	ResetEvents        uint64
	ResetCoalesced     uint64
	RecoverAttempts    uint64
	RecoverSuccess     uint64
	RecoverBackoffMs   uint64
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
	ims    *qmi.IMSService
	imsa   *qmi.IMSAService
	imsp   *qmi.IMSPService
	voice  *qmi.VOICEService

	// Connection handles / 连接句柄
	handleV4 uint32
	handleV6 uint32

	// State
	mu                sync.RWMutex
	state             State
	settings          *qmi.RuntimeSettings
	coreReady         bool
	desiredConnection bool

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
	reinitDelays []time.Duration
	isRotating   bool // Flag to suppress status checks during IP rotation / 标志位: IP轮换期间抑制状态检查
	recoverCount int
	lastIPCheck  time.Time

	// Internal notification / 内部通知
	regNotify chan bool // For fast registration detection / 用于快速注册检测

	// 多路拨号 (QMAP) / Multi-PDN
	muxIface string // QMAP 绑定后的虚拟网卡名 (如 qmimux0)

	timerMu                sync.Mutex
	scheduledTimers        map[*time.Timer]struct{}
	targetedCheckScheduled bool
	modemResetMu           sync.Mutex
	modemResetRecovering   bool
	modemResetPending      bool
	modemResetEnqueuedAt   time.Time
	modemResetDedupWindow  time.Duration
	modemResetDeferred     bool
	uimRecoveryMu          sync.Mutex
	dmsRecoveryMu          sync.Mutex
	nasRecoveryMu          sync.Mutex
	wmsRecoveryMu          sync.Mutex
	wmsReplayMu            sync.Mutex
	voiceRecoveryMu        sync.Mutex
	uimLastRecoverSignal   time.Time
	uimRecoverCooldown     time.Duration
	wmsReplayInProgress    bool

	// SMS recovery state / 短信恢复状态
	lastKnownGoodRoutes        *qmi.WMSRouteConfig
	wmsTransportStatus         qmi.WMSTransportNetworkRegistration
	wmsTransportKnown          bool
	wmsTransportUnsupported    bool
	wmsTransportQueryError     string
	wmsLastTransportWarn       string
	wmsLastTransportWarnAt     time.Time
	wmsSMSCValue               string
	wmsSMSCAvailable           bool
	wmsSMSCKnown               bool
	wmsSMSCStale               bool
	wmsSMSCUpdatedAt           time.Time
	wmsSMSCLastCheckAt         time.Time
	wmsSMSCRefreshPending      bool
	wmsRoutesKnown             bool
	wmsLastNASRegistered       bool
	wmsLastNASRegisteredKnown  bool
	wmsReadinessRefreshPending bool

	// Test hooks / 测试注入点
	querySignalStrength     func(ctx context.Context) (*qmi.SignalStrength, error)
	queryServingSystem      func(ctx context.Context) (*qmi.ServingSystem, error)
	queryPacketServiceState func(ctx context.Context) (qmi.ConnectionStatus, error)
	registerWMSEventReport  func(ctx context.Context) error
	registerWMSIndications  func(ctx context.Context, reportTransportNetworkRegistration bool) error
	queryWMSTransportState  func(ctx context.Context) (qmi.WMSTransportNetworkRegistration, error)
	queryWMSRoutes          func(ctx context.Context) (*qmi.WMSRouteConfig, error)
	setWMSRoutes            func(ctx context.Context, routes []qmi.WMSRoute, transferStatusReportToClient bool) error
	querySMSC               func(ctx context.Context) (string, error)
	queryNASRegistered      func(ctx context.Context) (bool, error)
	afterFunc               func(time.Duration, func()) *time.Timer
	ensureUIMServiceHook    func() (*qmi.UIMService, error)
	rebindUIMServiceHook    func(reason string) (*qmi.UIMService, error)
	ensureDMSServiceHook    func() (*qmi.DMSService, error)
	rebindDMSServiceHook    func(reason string) (*qmi.DMSService, error)
	ensureNASServiceHook    func() (*qmi.NASService, error)
	rebindNASServiceHook    func(reason string) (*qmi.NASService, error)
	ensureWMSServiceHook    func() (*qmi.WMSService, error)
	rebindWMSServiceHook    func(reason string) (*qmi.WMSService, error)
	ensureVOICEServiceHook  func() (*qmi.VOICEService, error)
	rebindVOICEServiceHook  func(reason string) (*qmi.VOICEService, error)
	onWMSRebindReplayHook   func(reason string)

	statusChecks       atomic.Uint64
	debouncedChecks    atomic.Uint64
	reconnectScheduled atomic.Uint64
	staleTimerIgnored  atomic.Uint64
	resetEvents        atomic.Uint64
	resetCoalesced     atomic.Uint64
	recoverAttempts    atomic.Uint64
	recoverSuccess     atomic.Uint64
	recoverBackoffMs   atomic.Uint64

	// 设备状态快照（由 NAS Indication 事件驱动，供上层零 IPC 读取）
	snapshot DeviceSnapshot
}

// internalEvent represents an internal event for the manager's event loop. / internalEvent 表示管理器事件循环的内部事件。
type internalEvent int

const (
	eventStart                internalEvent = iota // Start connection / 开始连接
	eventStop                                      // Stop connection / 停止连接
	eventCheckFull                                 // Periodic full status check / 周期性完整状态检查
	eventCheckTargeted                             // Debounced targeted status check / 去抖后的定向状态检查
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

var defaultReinitDelays = []time.Duration{
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	32 * time.Second,
}

var defaultTimeouts = TimeoutConfig{
	Init:               10 * time.Second,
	Dial:               30 * time.Second,
	SIMCheck:           10 * time.Second,
	StatusCheck:        5 * time.Second,
	Stop:               3 * time.Second,
	IndicationRegister: 5 * time.Second,
}

var defaultHealthPolicy = HealthPolicy{
	FullCheckInterval:     15 * time.Second,
	IndicationDebounce:    500 * time.Millisecond,
	IPConsistencyInterval: 60 * time.Second,
}

var defaultEventPolicy = EventPolicy{
	CallbackQueueSize: 128,
}

const defaultUIMRecoverCooldown = 10 * time.Second
const defaultModemResetDedupWindow = 1 * time.Second
const recoverBackoffBase = 500 * time.Millisecond
const recoverBackoffMax = 15 * time.Second
const recoverBackoffJitterRatio = 0.2

var (
	smsReadyWaitTimeout   = 8 * time.Second
	smsReadyPollInterval  = 500 * time.Millisecond
	smscRefreshCooldown   = 30 * time.Second
	transportWarnCooldown = 30 * time.Second
)

type wmsRefreshOptions struct {
	includeRoutes    bool
	includeTransport bool
	includeSMSC      bool
	forceSMSC        bool
	allowRouteReplay bool
	emitSummary      bool
	quiet            bool
	reason           string
}

func normalizeConfig(cfg Config) Config {
	if cfg.Timeouts.Init <= 0 {
		cfg.Timeouts.Init = defaultTimeouts.Init
	}
	if cfg.Timeouts.Dial <= 0 {
		cfg.Timeouts.Dial = defaultTimeouts.Dial
	}
	if cfg.Timeouts.SIMCheck <= 0 {
		cfg.Timeouts.SIMCheck = defaultTimeouts.SIMCheck
	}
	if cfg.Timeouts.StatusCheck <= 0 {
		cfg.Timeouts.StatusCheck = defaultTimeouts.StatusCheck
	}
	if cfg.Timeouts.Stop <= 0 {
		cfg.Timeouts.Stop = defaultTimeouts.Stop
	}
	if cfg.Timeouts.IndicationRegister <= 0 {
		cfg.Timeouts.IndicationRegister = defaultTimeouts.IndicationRegister
	}

	if len(cfg.RetryPolicy.ReconnectDelays) == 0 {
		cfg.RetryPolicy.ReconnectDelays = append([]time.Duration(nil), defaultRetryDelays...)
	}
	if len(cfg.RetryPolicy.ReinitDelays) == 0 {
		cfg.RetryPolicy.ReinitDelays = append([]time.Duration(nil), defaultReinitDelays...)
	}
	if cfg.RetryPolicy.RadioResetAfter <= 0 {
		cfg.RetryPolicy.RadioResetAfter = 3
	}

	if cfg.HealthPolicy.FullCheckInterval <= 0 {
		cfg.HealthPolicy.FullCheckInterval = defaultHealthPolicy.FullCheckInterval
	}
	if cfg.HealthPolicy.IndicationDebounce <= 0 {
		cfg.HealthPolicy.IndicationDebounce = defaultHealthPolicy.IndicationDebounce
	}
	if cfg.HealthPolicy.IPConsistencyInterval <= 0 {
		cfg.HealthPolicy.IPConsistencyInterval = defaultHealthPolicy.IPConsistencyInterval
	}

	if cfg.EventPolicy.CallbackQueueSize <= 0 {
		cfg.EventPolicy.CallbackQueueSize = defaultEventPolicy.CallbackQueueSize
	}
	defaultClientOpts := qmi.DefaultClientOptions()
	if cfg.ClientOptions.ReadDeadline <= 0 {
		cfg.ClientOptions.ReadDeadline = defaultClientOpts.ReadDeadline
	}
	if cfg.ClientOptions.DefaultRequestTimeout <= 0 {
		cfg.ClientOptions.DefaultRequestTimeout = defaultClientOpts.DefaultRequestTimeout
	}
	if cfg.ClientOptions.TxQueueSize <= 0 {
		cfg.ClientOptions.TxQueueSize = defaultClientOpts.TxQueueSize
	}
	if cfg.ClientOptions.IndicationQueueSize <= 0 {
		cfg.ClientOptions.IndicationQueueSize = defaultClientOpts.IndicationQueueSize
	}
	if !cfg.ClientOptions.SyncOnOpen &&
		cfg.ClientOptions.ReadDeadline == defaultClientOpts.ReadDeadline &&
		cfg.ClientOptions.DefaultRequestTimeout == defaultClientOpts.DefaultRequestTimeout &&
		cfg.ClientOptions.TxQueueSize == defaultClientOpts.TxQueueSize &&
		cfg.ClientOptions.IndicationQueueSize == defaultClientOpts.IndicationQueueSize {
		cfg.ClientOptions.SyncOnOpen = defaultClientOpts.SyncOnOpen
	}
	return cfg
}

// New creates a new connection manager / New 创建新的连接管理器
// logger is optional, if nil a default logger will be used / logger 是可选的，如果为 nil 则使用默认日志器
func New(cfg Config, logger Logger) *Manager {
	cfg = normalizeConfig(cfg)
	if logger == nil {
		logger = NewNopLogger()
	}

	return &Manager{
		cfg:                   cfg,
		log:                   logger.WithField("iface", cfg.Device.NetInterface),
		retryDelays:           append([]time.Duration(nil), cfg.RetryPolicy.ReconnectDelays...),
		reinitDelays:          append([]time.Duration(nil), cfg.RetryPolicy.ReinitDelays...),
		eventCh:               make(chan internalEvent, 16),
		events:                NewEventEmitterWithQueueSize(cfg.EventPolicy.CallbackQueueSize),
		scheduledTimers:       make(map[*time.Timer]struct{}),
		modemResetDedupWindow: defaultModemResetDedupWindow,
		uimRecoverCooldown:    defaultUIMRecoverCooldown,
	}
}

// Start initializes and starts the connection manager / Start 初始化并启动连接管理器
func (m *Manager) Start() error {
	if err := m.StartCore(); err != nil {
		return err
	}

	if !m.cfg.NoDial {
		m.mu.Lock()
		m.desiredConnection = true
		m.mu.Unlock()
		m.eventCh <- eventStart
	} else {
		m.log.Info("NoDial mode enabled, core started without initial connection")
	}

	return nil
}

// StartCore initializes the QMI core services without starting a data call.
func (m *Manager) StartCore() error {
	m.mu.Lock()
	if m.coreReady || m.state != StateDisconnected {
		m.mu.Unlock()
		return fmt.Errorf("manager already started")
	}
	m.state = StateConnecting
	m.desiredConnection = false
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
	m.stopScheduledTimers()
	m.wg.Add(2)
	go m.eventLoop()
	go m.indicationHandler()

	m.mu.Lock()
	m.coreReady = true
	m.mu.Unlock()
	m.setState(StateDisconnected)
	m.log.Info("QMI core started")

	// 在底层 QMI 就绪后立即在后台异步全量加载设备标识。
	m.PreWarmIdentities(true)

	return nil
}

// Stop gracefully stops the connection manager / Stop 优雅停止连接管理器
func (m *Manager) Stop() error {
	m.mu.Lock()
	if (!m.coreReady && m.state == StateDisconnected) || m.state == StateStopping {
		m.mu.Unlock()
		return nil
	}
	m.desiredConnection = false
	m.state = StateStopping
	m.mu.Unlock()

	m.log.Info("Stopping connection manager...")
	m.stopScheduledTimers()
	m.eventCh <- eventStop

	// Wait for loops to finish / 等待循环结束
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	m.cleanup()
	m.setState(StateDisconnected)
	m.log.Info("Connection manager stopped")
	return nil
}

// Connect establishes a data call on top of an already-started QMI core.
func (m *Manager) Connect() error {
	m.mu.Lock()
	if !m.coreReady {
		m.mu.Unlock()
		return fmt.Errorf("manager core not started")
	}
	if m.state == StateStopping {
		m.mu.Unlock()
		return fmt.Errorf("manager is stopping")
	}
	if m.state == StateConnected {
		m.desiredConnection = true
		m.mu.Unlock()
		return nil
	}
	if m.state == StateConnecting && (m.handleV4 != 0 || m.handleV6 != 0) {
		m.desiredConnection = true
		m.mu.Unlock()
		return fmt.Errorf("connection already in progress")
	}
	m.desiredConnection = true
	m.mu.Unlock()

	return m.doConnect()
}

// Disconnect tears down the current data call but keeps the QMI core available.
func (m *Manager) Disconnect() error {
	m.mu.Lock()
	if !m.coreReady {
		m.mu.Unlock()
		return fmt.Errorf("manager core not started")
	}
	if m.state == StateStopping {
		m.mu.Unlock()
		return fmt.Errorf("manager is stopping")
	}
	m.desiredConnection = false
	hasData := m.handleV4 != 0 || m.handleV6 != 0 || m.state == StateConnected || m.state == StateConnecting
	m.mu.Unlock()

	if !hasData {
		return nil
	}

	m.doDisconnect()
	return nil
}

// State returns the current connection state / State 返回当前的连接状态
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// IsCoreReady reports whether the QMI core services are initialized.
func (m *Manager) IsCoreReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.coreReady
}

// WaitCoreReady 阻塞等待直到 QMI core 服务恢复就绪（coreReady == true）。
// 用于 modem reset 后的调用方门控，避免在 QMI 服务恢复窗口期内发起请求。
// 如果 core 已就绪，立即返回 nil。
func (m *Manager) WaitCoreReady(ctx context.Context) error {
	m.mu.RLock()
	if m.coreReady {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 QMI core 就绪超时: %w", ctx.Err())
		case <-ticker.C:
			m.mu.RLock()
			ready := m.coreReady
			m.mu.RUnlock()
			if ready {
				return nil
			}
		}
	}
}

// IsConnected reports whether the data plane is currently connected.
func (m *Manager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state == StateConnected
}

// Settings returns the current IP settings / Settings 返回当前的 IP 设置
func (m *Manager) Settings() *qmi.RuntimeSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

func (m *Manager) Stats() ManagerStats {
	if m == nil {
		return ManagerStats{}
	}
	return ManagerStats{
		StatusChecks:       m.statusChecks.Load(),
		DebouncedChecks:    m.debouncedChecks.Load(),
		ReconnectScheduled: m.reconnectScheduled.Load(),
		StaleTimerIgnored:  m.staleTimerIgnored.Load(),
		ResetEvents:        m.resetEvents.Load(),
		ResetCoalesced:     m.resetCoalesced.Load(),
		RecoverAttempts:    m.recoverAttempts.Load(),
		RecoverSuccess:     m.recoverSuccess.Load(),
		RecoverBackoffMs:   m.recoverBackoffMs.Load(),
	}
}

func (m *Manager) ClientStats() qmi.ClientStats {
	if m == nil {
		return qmi.ClientStats{}
	}
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return qmi.ClientStats{}
	}
	return client.Stats()
}

func (m *Manager) emitEvent(event Event) {
	if m == nil || m.events == nil {
		return
	}
	m.events.Emit(event)
}

func (m *Manager) qmiIndicationEvent(eventType EventType, evt qmi.Event) Event {
	return Event{
		Type:       eventType,
		State:      m.State(),
		RawQMIType: evt.Type,
		ServiceID:  evt.ServiceID,
		MessageID:  evt.MessageID,
	}
}

func (m *Manager) emitQMIIndicationEvent(eventType EventType, evt qmi.Event) {
	m.emitEvent(m.qmiIndicationEvent(eventType, evt))
}

func (m *Manager) emitSignalUpdate(sig *qmi.SignalStrength) {
	if sig == nil {
		return
	}
	// 原地更新快照，供上层零 IPC 读取信号强度
	m.snapshot.updateSignal(sig)
	m.emitEvent(Event{
		Type:   EventSignalUpdate,
		State:  m.State(),
		Signal: sig,
	})
}

func packetTLVMeta(packet *qmi.Packet) []qmi.TLVMeta {
	if packet == nil || len(packet.TLVs) == 0 {
		return nil
	}
	meta := make([]qmi.TLVMeta, 0, len(packet.TLVs))
	for _, tlv := range packet.TLVs {
		meta = append(meta, qmi.TLVMeta{
			Type:   tlv.Type,
			Length: len(tlv.Value),
		})
	}
	return meta
}

func isUnsupportedOptionalWMSIndicationError(err error) bool {
	if err == nil {
		return false
	}
	var qmiErr *qmi.QMIError
	if !errors.As(err, &qmiErr) || qmiErr == nil {
		return false
	}
	// QMIErrOpDeviceUnsupported (0x0034) 是 EC20 对 WMSGetTransportNetworkRegistrationStatus
	// (msg 0x004A) 的常见回应，含义等同于「设备不支持该操作」
	return qmiErr.ErrorCode == qmi.QMIErrInvalidQmiCmd ||
		qmiErr.ErrorCode == qmi.QMIErrNotSupported ||
		qmiErr.ErrorCode == qmi.QMIErrOpDeviceUnsupported
}

func (m *Manager) getSignalStrength(ctx context.Context) (*qmi.SignalStrength, error) {
	if m.querySignalStrength != nil {
		return m.querySignalStrength(ctx)
	}
	return withNASRecoveryValue(m, "getSignalStrength", func(nas *qmi.NASService) (*qmi.SignalStrength, error) {
		return nas.GetSignalStrength(ctx)
	})
}

func (m *Manager) getServingSystem(ctx context.Context) (*qmi.ServingSystem, error) {
	if m.queryServingSystem != nil {
		return m.queryServingSystem(ctx)
	}
	return withNASRecoveryValue(m, "getServingSystem", func(nas *qmi.NASService) (*qmi.ServingSystem, error) {
		return nas.GetServingSystem(ctx)
	})
}

func (m *Manager) getPacketServiceState(ctx context.Context) (qmi.ConnectionStatus, error) {
	if m.queryPacketServiceState != nil {
		return m.queryPacketServiceState(ctx)
	}
	if m.wds == nil {
		return qmi.StatusUnknown, fmt.Errorf("wds service not available")
	}
	return m.wds.GetPacketServiceStatus(ctx)
}

func (m *Manager) opContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	m.mu.RLock()
	if m.ctx != nil && m.ctx.Err() == nil {
		base = m.ctx
	}
	m.mu.RUnlock()

	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}

func maxDuration(a, b time.Duration) time.Duration {
	if a >= b {
		return a
	}
	return b
}

func contextWithMaxTimeout(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		return context.WithCancel(ctx)
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining <= limit {
			return context.WithCancel(ctx)
		}
	}
	return context.WithTimeout(ctx, limit)
}

func copyWMSRouteConfig(cfg *qmi.WMSRouteConfig) *qmi.WMSRouteConfig {
	if cfg == nil {
		return nil
	}
	dup := &qmi.WMSRouteConfig{
		TransferStatusReportToClient: cfg.TransferStatusReportToClient,
		HasTransferStatusReport:      cfg.HasTransferStatusReport,
	}
	if len(cfg.Routes) > 0 {
		dup.Routes = append([]qmi.WMSRoute(nil), cfg.Routes...)
	}
	return dup
}

func isUsableWMSRouteConfig(cfg *qmi.WMSRouteConfig) bool {
	return cfg != nil && len(cfg.Routes) > 0
}

func boolPtr(v bool) *bool {
	out := v
	return &out
}

func (m *Manager) setWMSTransportState(status qmi.WMSTransportNetworkRegistration, known, unsupported bool) {
	m.mu.Lock()
	m.wmsTransportStatus = status
	m.wmsTransportKnown = known
	m.wmsTransportUnsupported = unsupported
	m.mu.Unlock()
}

func (m *Manager) setWMSTransportQueryError(err error) {
	m.mu.Lock()
	if err == nil {
		m.wmsTransportQueryError = ""
	} else {
		m.wmsTransportQueryError = err.Error()
	}
	m.mu.Unlock()
}

func (m *Manager) setWMSSMSCState(value string, known, available, stale bool, updatedAt, checkedAt time.Time) {
	m.mu.Lock()
	m.wmsSMSCValue = value
	m.wmsSMSCKnown = known
	m.wmsSMSCAvailable = available
	m.wmsSMSCStale = stale
	m.wmsSMSCUpdatedAt = updatedAt
	if checkedAt.IsZero() {
		checkedAt = updatedAt
	}
	m.wmsSMSCLastCheckAt = checkedAt
	m.mu.Unlock()
}

func (m *Manager) beginWMSSMSCRefresh() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.wmsSMSCRefreshPending {
		return false
	}
	m.wmsSMSCRefreshPending = true
	return true
}

func (m *Manager) endWMSSMSCRefresh() {
	m.mu.Lock()
	m.wmsSMSCRefreshPending = false
	m.mu.Unlock()
}

func (m *Manager) setWMSRoutesKnown(known bool) {
	m.mu.Lock()
	m.wmsRoutesKnown = known
	m.mu.Unlock()
}

func (m *Manager) cacheWMSRoutes(cfg *qmi.WMSRouteConfig) {
	m.mu.Lock()
	m.lastKnownGoodRoutes = copyWMSRouteConfig(cfg)
	m.wmsRoutesKnown = isUsableWMSRouteConfig(cfg)
	m.mu.Unlock()
}

func (m *Manager) setWMSLastNASRegistered(registered *bool) {
	m.mu.Lock()
	if registered == nil {
		m.wmsLastNASRegisteredKnown = false
		m.wmsLastNASRegistered = false
	} else {
		m.wmsLastNASRegisteredKnown = true
		m.wmsLastNASRegistered = *registered
	}
	m.mu.Unlock()
}

func (m *Manager) markWMSReadinessStale() {
	m.mu.Lock()
	m.wmsTransportStatus = 0
	m.wmsTransportKnown = false
	m.wmsTransportUnsupported = false
	m.wmsTransportQueryError = ""
	m.wmsLastTransportWarn = ""
	m.wmsLastTransportWarnAt = time.Time{}
	m.wmsLastNASRegisteredKnown = false
	m.wmsLastNASRegistered = false
	if m.wmsSMSCKnown {
		m.wmsSMSCStale = true
	}
	m.mu.Unlock()
}

func (m *Manager) cachedWMSRoutes() *qmi.WMSRouteConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return copyWMSRouteConfig(m.lastKnownGoodRoutes)
}

func (m *Manager) wmsReadinessSnapshot() (status qmi.WMSTransportNetworkRegistration, known, unsupported, smscAvailable, routesKnown bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wmsTransportStatus, m.wmsTransportKnown, m.wmsTransportUnsupported, m.wmsSMSCAvailable, m.wmsRoutesKnown
}

func (m *Manager) wmsSMSCSnapshot() (value string, available, known, stale bool, updatedAt, lastCheckAt time.Time, pending bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wmsSMSCValue, m.wmsSMSCAvailable, m.wmsSMSCKnown, m.wmsSMSCStale, m.wmsSMSCUpdatedAt, m.wmsSMSCLastCheckAt, m.wmsSMSCRefreshPending
}

func (m *Manager) wmsDiagnosticSnapshot() (status qmi.WMSTransportNetworkRegistration, known, unsupported bool, transportQueryError string, smscValue string, smscAvailable, smscKnown, smscStale bool, smscUpdatedAt time.Time, routesKnown bool, nasRegistered *bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.wmsLastNASRegisteredKnown {
		nasRegistered = boolPtr(m.wmsLastNASRegistered)
	}
	return m.wmsTransportStatus, m.wmsTransportKnown, m.wmsTransportUnsupported, m.wmsTransportQueryError, m.wmsSMSCValue, m.wmsSMSCAvailable, m.wmsSMSCKnown, m.wmsSMSCStale, m.wmsSMSCUpdatedAt, m.wmsRoutesKnown, nasRegistered
}

func (m *Manager) wmsTransportStatusString() string {
	status, known, unsupported, _, _, _, _, _, _, _, _ := m.wmsDiagnosticSnapshot()
	if unsupported {
		return "unsupported"
	}
	if !known {
		return "unknown"
	}
	return status.String()
}

func (m *Manager) shouldWarnTransportError(err error) bool {
	if err == nil {
		return false
	}

	now := time.Now()
	message := err.Error()

	m.mu.Lock()
	defer m.mu.Unlock()
	if message != "" && message == m.wmsLastTransportWarn && !m.wmsLastTransportWarnAt.IsZero() && now.Sub(m.wmsLastTransportWarnAt) < transportWarnCooldown {
		return false
	}
	m.wmsLastTransportWarn = message
	m.wmsLastTransportWarnAt = now
	return true
}

func (m *Manager) hasWMSReadyPath() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wms != nil || m.registerWMSEventReport != nil || m.registerWMSIndications != nil || m.queryWMSTransportState != nil || m.queryWMSRoutes != nil
}

func (m *Manager) registerWMSEventReportWithContext(ctx context.Context) error {
	if m.registerWMSEventReport != nil {
		return m.registerWMSEventReport(ctx)
	}
	return m.withWMSRecovery("registerWMSEventReportWithContext", func(wms *qmi.WMSService) error {
		return wms.RegisterEventReport(ctx)
	})
}

func (m *Manager) registerWMSIndicationsWithContext(ctx context.Context, reportTransportNetworkRegistration bool) error {
	if m.registerWMSIndications != nil {
		return m.registerWMSIndications(ctx, reportTransportNetworkRegistration)
	}
	return m.withWMSRecovery("registerWMSIndicationsWithContext", func(wms *qmi.WMSService) error {
		return wms.IndicationRegister(ctx, reportTransportNetworkRegistration)
	})
}

func (m *Manager) queryWMSTransportStateWithContext(ctx context.Context) (qmi.WMSTransportNetworkRegistration, error) {
	if m.queryWMSTransportState != nil {
		return m.queryWMSTransportState(ctx)
	}
	return m.WMSGetTransportNetworkRegistrationStatus(ctx)
}

func (m *Manager) queryWMSRoutesWithContext(ctx context.Context) (*qmi.WMSRouteConfig, error) {
	if m.queryWMSRoutes != nil {
		return m.queryWMSRoutes(ctx)
	}
	return m.WMSGetRoutes(ctx)
}

func (m *Manager) setWMSRoutesWithContext(ctx context.Context, routes []qmi.WMSRoute, transferStatusReportToClient bool) error {
	if m.setWMSRoutes != nil {
		return m.setWMSRoutes(ctx, routes, transferStatusReportToClient)
	}
	return m.WMSSetRoutes(ctx, routes, transferStatusReportToClient)
}

func (m *Manager) querySMSCWithContext(ctx context.Context) (string, error) {
	if m.querySMSC != nil {
		return m.querySMSC(ctx)
	}
	return m.querySMSCFromDevice(ctx)
}

func (m *Manager) queryNASRegisteredWithContext(ctx context.Context) (bool, error) {
	if m.queryNASRegistered != nil {
		return m.queryNASRegistered(ctx)
	}
	return withNASRecoveryValue(m, "queryNASRegisteredWithContext", func(nas *qmi.NASService) (bool, error) {
		return nas.IsRegistered(ctx)
	})
}

func (m *Manager) replayCachedWMSRoutes(ctx context.Context) {
	cfg := m.cachedWMSRoutes()
	if !isUsableWMSRouteConfig(cfg) {
		m.setWMSRoutesKnown(false)
		m.log.Debug("WMS routes unavailable and no cached routes to replay")
		return
	}

	if err := m.setWMSRoutesWithContext(ctx, cfg.Routes, cfg.TransferStatusReportToClient); err != nil {
		m.setWMSRoutesKnown(false)
		m.log.WithError(err).Warn("Failed to replay cached WMS routes")
		return
	}

	m.setWMSRoutesKnown(true)
	m.log.WithField("route_count", len(cfg.Routes)).Info("WMS routes replayed from cache")
}

func (m *Manager) refreshWMSRouteState(ctx context.Context, allowRouteReplay bool, quiet bool) {
	cfg, err := m.queryWMSRoutesWithContext(ctx)
	if err == nil && isUsableWMSRouteConfig(cfg) {
		m.cacheWMSRoutes(cfg)
		if !quiet {
			m.log.WithField("route_count", len(cfg.Routes)).Debug("WMS routes cached")
		}
		return
	}

	m.setWMSRoutesKnown(false)
	if err != nil {
		if !quiet {
			m.log.WithError(err).Warn("Failed to query WMS routes")
		}
	} else if !quiet {
		m.log.Debug("WMS routes unavailable or empty")
	}
	if allowRouteReplay {
		m.replayCachedWMSRoutes(ctx)
	}
}

func (m *Manager) refreshWMSTransportState(ctx context.Context, quiet bool) {
	status, err := m.queryWMSTransportStateWithContext(ctx)
	switch {
	case err == nil:
		m.setWMSTransportState(status, true, false)
		m.setWMSTransportQueryError(nil)
	case isUnsupportedOptionalWMSIndicationError(err):
		m.setWMSTransportState(0, false, true)
		m.setWMSTransportQueryError(err)
		if !quiet {
			m.log.WithError(err).Debug("WMS transport registration query not supported by modem")
		}
	default:
		m.setWMSTransportState(0, false, false)
		m.setWMSTransportQueryError(err)
		if !quiet {
			entry := m.log.WithError(err)
			if m.shouldWarnTransportError(err) {
				entry.Warn("Failed to query WMS transport registration status")
			} else {
				entry.Debug("Failed to query WMS transport registration status")
			}
		}
	}
}

func (m *Manager) refreshWMSSMSCState(ctx context.Context, force bool, quiet bool) {
	_, _, _, _, _, cachedLastCheckAt, pending := m.wmsSMSCSnapshot()
	if pending {
		return
	}
	if !force && !cachedLastCheckAt.IsZero() && time.Since(cachedLastCheckAt) < smscRefreshCooldown {
		return
	}
	if !m.beginWMSSMSCRefresh() {
		return
	}
	defer m.endWMSSMSCRefresh()

	checkedAt := time.Now()
	smsc, err := m.querySMSCWithContext(ctx)
	if err != nil {
		cachedValue, cachedAvailable, cachedKnown, _, cachedUpdatedAt, _, _ := m.wmsSMSCSnapshot()
		if cachedKnown {
			m.setWMSSMSCState(cachedValue, true, cachedAvailable, true, cachedUpdatedAt, checkedAt)
			if !quiet && force {
				m.log.WithError(err).Debug("WMS SMSC refresh failed, keeping cached SMSC")
			}
			return
		}
		m.setWMSSMSCState("", false, false, false, time.Time{}, checkedAt)
		if !quiet {
			m.log.WithError(err).Debug("WMS SMSC unavailable")
		}
		return
	}

	trimmed := strings.TrimSpace(smsc)
	known := trimmed != ""
	m.setWMSSMSCState(trimmed, known, known, false, checkedAt, checkedAt)
	if !known && !quiet {
		m.log.Debug("WMS SMSC unavailable: empty result")
	}
}

func (m *Manager) logWMSRecoveryState(message string) {
	_, known, unsupported, transportQueryError, _, smscAvailable, smscKnown, smscStale, smscUpdatedAt, routesKnown, nasRegistered := m.wmsDiagnosticSnapshot()
	entry := m.log.
		WithField("transport_status", m.wmsTransportStatusString()).
		WithField("transport_known", known).
		WithField("transport_unsupported", unsupported).
		WithField("smsc_available", smscAvailable).
		WithField("smsc_known", smscKnown).
		WithField("smsc_stale", smscStale).
		WithField("routes_known", routesKnown)
	if transportQueryError != "" {
		entry = entry.WithField("transport_query_error", transportQueryError)
	}
	if !smscUpdatedAt.IsZero() {
		entry = entry.WithField("smsc_last_updated_at", smscUpdatedAt)
	}
	if nasRegistered != nil {
		entry = entry.WithField("nas_registered", *nasRegistered)
	}
	entry.Debug(message)
}

func (m *Manager) refreshWMSState(ctx context.Context, opts wmsRefreshOptions) {
	if opts.includeRoutes {
		m.refreshWMSRouteState(ctx, opts.allowRouteReplay, opts.quiet)
	}
	if opts.includeTransport {
		m.refreshWMSTransportState(ctx, opts.quiet)
	}
	if opts.includeSMSC {
		m.refreshWMSSMSCState(ctx, opts.forceSMSC, opts.quiet)
	}

	if opts.emitSummary {
		_, known, unsupported, smscAvailable, routesKnown := m.wmsReadinessSnapshot()
		switch {
		case routesKnown && smscAvailable && ((known && m.wmsTransportStatusString() == qmi.WMSTransportNetworkRegistrationFullService.String()) || unsupported):
			m.logWMSRecoveryState("WMS recovery state ready")
		default:
			m.logWMSRecoveryState("WMS recovery state degraded")
		}
	}
}

func (m *Manager) recoverWMSState() {
	if !m.hasWMSReadyPath() {
		return
	}

	if !m.cfg.DisableWMSInd {
		ctx, cancel := m.opContext(m.cfg.Timeouts.IndicationRegister)
		if err := m.registerWMSEventReportWithContext(ctx); err != nil {
			m.log.WithError(err).Warn("Failed to register SMS indications")
		}
		cancel()

		ctx, cancel = m.opContext(m.cfg.Timeouts.IndicationRegister)
		if err := m.registerWMSIndicationsWithContext(ctx, true); err != nil {
			if isUnsupportedOptionalWMSIndicationError(err) {
				m.log.WithError(err).Debug("WMS transport registration indications not supported by modem")
			} else {
				m.log.WithError(err).Warn("Failed to register WMS transport registration indications")
			}
		}
		cancel()
	} else {
		m.log.Debug("WMS indications disabled by config")
	}

	ctx, cancel := m.opContext(maxDuration(m.cfg.Timeouts.IndicationRegister, m.cfg.Timeouts.StatusCheck))
	defer cancel()
	m.refreshWMSState(ctx, wmsRefreshOptions{
		includeRoutes:    true,
		includeTransport: true,
		includeSMSC:      true,
		forceSMSC:        true,
		allowRouteReplay: true,
		emitSummary:      true,
		reason:           "recover",
	})
}

func (m *Manager) maybeRefreshWMSReadiness(reason string) {
	if !m.hasWMSReadyPath() {
		return
	}

	m.mu.Lock()
	if m.wmsReadinessRefreshPending {
		m.mu.Unlock()
		return
	}
	m.wmsReadinessRefreshPending = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			m.wmsReadinessRefreshPending = false
			m.mu.Unlock()
		}()

		ctx, cancel := m.opContext(m.cfg.Timeouts.StatusCheck)
		defer cancel()
		m.refreshWMSState(ctx, wmsRefreshOptions{
			includeRoutes:    true,
			includeTransport: true,
			includeSMSC:      false,
			allowRouteReplay: false,
			emitSummary:      true,
			reason:           reason,
		})
		m.log.WithField("reason", reason).Debug("WMS readiness refresh completed")
	}()
}

func (m *Manager) smsReadyWithContext(ctx context.Context) (bool, bool, error) {
	if !m.hasWMSReadyPath() {
		return false, false, ErrServiceNotReady("WMS")
	}

	_, _, smscKnown, _, _, _, _ := m.wmsSMSCSnapshot()
	m.refreshWMSState(ctx, wmsRefreshOptions{
		includeRoutes:    true,
		includeTransport: true,
		includeSMSC:      !smscKnown,
		allowRouteReplay: false,
		emitSummary:      false,
		quiet:            true,
		reason:           "sms-ready-check",
	})

	status, known, unsupported, smscAvailable, routesKnown := m.wmsReadinessSnapshot()
	transportStatus := m.wmsTransportStatusString()
	if !smscAvailable {
		m.setWMSLastNASRegistered(nil)
		return false, false, nil
	}
	if known && transportStatus == qmi.WMSTransportNetworkRegistrationFullService.String() {
		m.setWMSLastNASRegistered(nil)
		return true, false, nil
	}
	if known {
		switch status {
		case qmi.WMSTransportNetworkRegistrationNoService,
			qmi.WMSTransportNetworkRegistrationInProcess,
			qmi.WMSTransportNetworkRegistrationFailure,
			qmi.WMSTransportNetworkRegistrationLimitedService:
			m.setWMSLastNASRegistered(nil)
			return false, false, nil
		}
	}
	if unsupported || transportStatus == "unknown" {
		if !routesKnown {
			m.setWMSLastNASRegistered(nil)
			return false, false, nil
		}
		registered, err := m.queryNASRegisteredWithContext(ctx)
		if err != nil {
			m.setWMSLastNASRegistered(nil)
			return false, false, err
		}
		m.setWMSLastNASRegistered(boolPtr(registered))
		return registered, true, nil
	}
	m.setWMSLastNASRegistered(nil)
	return false, false, nil
}

// EnsureSMSReady performs a minimal compatibility check for SMS sending.
func (m *Manager) EnsureSMSReady(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !m.hasWMSReadyPath() {
		return ErrServiceNotReady("WMS")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}

func (m *Manager) currentSMSReadinessDetails() SMSNotReadyError {
	status, known, unsupported, transportQueryError, _, smscAvailable, _, _, _, routesKnown, nasRegistered := m.wmsDiagnosticSnapshot()
	transportStatus := "unknown"
	switch {
	case unsupported:
		transportStatus = "unsupported"
	case known:
		transportStatus = status.String()
	}
	return SMSNotReadyError{
		TransportStatus:      transportStatus,
		TransportKnown:       known,
		TransportUnsupported: unsupported,
		TransportQueryError:  transportQueryError,
		SMSCAvailable:        smscAvailable,
		RoutesKnown:          routesKnown,
		NASRegistered:        nasRegistered,
	}
}

func (m *Manager) currentSMSNotReadyError() *SMSNotReadyError {
	details := m.currentSMSReadinessDetails()
	return &details
}

func (m *Manager) scheduleAfter(delay time.Duration, fn func()) {
	if m.afterFunc != nil {
		m.afterFunc(delay, fn)
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		m.timerMu.Lock()
		if timer != nil {
			delete(m.scheduledTimers, timer)
		}
		m.timerMu.Unlock()

		m.mu.RLock()
		ctx := m.ctx
		m.mu.RUnlock()
		if ctx != nil && ctx.Err() != nil {
			m.staleTimerIgnored.Add(1)
			return
		}
		fn()
	})

	m.timerMu.Lock()
	m.scheduledTimers[timer] = struct{}{}
	m.timerMu.Unlock()
}

func (m *Manager) stopScheduledTimers() {
	m.timerMu.Lock()
	defer m.timerMu.Unlock()
	for timer := range m.scheduledTimers {
		timer.Stop()
		delete(m.scheduledTimers, timer)
	}
	m.targetedCheckScheduled = false
}

func (m *Manager) scheduleTargetedCheck() {
	m.timerMu.Lock()
	if m.targetedCheckScheduled {
		m.timerMu.Unlock()
		m.debouncedChecks.Add(1)
		return
	}
	m.targetedCheckScheduled = true
	m.timerMu.Unlock()

	m.scheduleAfter(m.cfg.HealthPolicy.IndicationDebounce, func() {
		m.timerMu.Lock()
		m.targetedCheckScheduled = false
		m.timerMu.Unlock()

		select {
		case m.eventCh <- eventCheckTargeted:
		default:
			m.debouncedChecks.Add(1)
		}
	})
}

// OpenLogicalChannel opens a UIM logical channel using a fixed 10s timeout.
func (m *Manager) OpenLogicalChannel(slot uint8, aid []byte) (byte, error) {
	ctx, cancel := m.opContext(m.cfg.Timeouts.Stop)
	defer cancel()
	return withUIMRecoveryValue(m, "OpenLogicalChannel", func(uim *qmi.UIMService) (byte, error) {
		return uim.OpenLogicalChannel(ctx, slot, aid)
	})
}

// CloseLogicalChannel closes a UIM logical channel using a fixed 10s timeout.
func (m *Manager) CloseLogicalChannel(slot uint8, channel uint8) error {
	ctx, cancel := m.opContext(m.cfg.Timeouts.Stop)
	defer cancel()
	return m.withUIMRecovery("CloseLogicalChannel", func(uim *qmi.UIMService) error {
		return uim.CloseLogicalChannel(ctx, slot, channel)
	})
}

// SendAPDU transmits a raw APDU using a fixed 10s timeout.
func (m *Manager) SendAPDU(slot uint8, channel uint8, command []byte) ([]byte, error) {
	ctx, cancel := m.opContext(m.cfg.Timeouts.Stop)
	defer cancel()
	return withUIMRecoveryValue(m, "SendAPDU", func(uim *qmi.UIMService) ([]byte, error) {
		return uim.SendAPDU(ctx, slot, channel, command)
	})
}

// GetNativeMCCMNC 获取原生归属地 MCC 和 MNC
func (m *Manager) GetNativeMCCMNC(ctx context.Context) (mcc, mnc string, err error) {
	type nativeLocation struct {
		mcc string
		mnc string
	}
	location, err := withUIMRecoveryValue(m, "GetNativeMCCMNC", func(uim *qmi.UIMService) (nativeLocation, error) {
		localMCC, localMNC, callErr := uim.GetNativeMCCMNC(ctx)
		return nativeLocation{
			mcc: localMCC,
			mnc: localMNC,
		}, callErr
	})
	if err != nil {
		return "", "", err
	}
	return location.mcc, location.mnc, nil
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

	ctx, cancel := m.opContext(m.cfg.Timeouts.Dial)
	defer cancel()

	// 1. Disconnect data call / 1. 断开数据呼叫
	if m.handleV4 != 0 && m.wds != nil {
		_ = m.wds.StopNetworkInterface(ctx, m.handleV4)
		m.handleV4 = 0
	}

	// Flush old addresses to avoid duplicates / 清除旧地址以避免重复
	netcfg.FlushAddresses(m.cfg.Device.NetInterface)

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
	ctx, cancel := m.opContext(m.cfg.Timeouts.Dial)
	defer cancel()

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
	if err := m.withDMSRecovery("rotateViaRadioReset.RadioPowerCycle", func(dms *qmi.DMSService) error {
		m.log.Info("Turning radio off...")
		if powerErr := dms.RadioPower(ctx, false); powerErr != nil {
			return powerErr
		}
		// 3. Radio on / 4. 打开射频
		m.log.Info("Turning radio on...")
		if powerErr := dms.RadioPower(ctx, true); powerErr != nil {
			return powerErr
		}
		// No fixed sleep here, start polling immediately / 此处无固定睡眠，立即开始轮询
		return nil
	}); err != nil {
		m.log.WithError(err).Warn("Radio power cycle failed during rotateViaRadioReset")
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
	if registered, _ := withNASRecoveryValue(m, "rotateViaRadioReset.IsRegistered", func(nas *qmi.NASService) (bool, error) {
		return nas.IsRegistered(ctx)
	}); registered {
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
	}

	// NAS
	m.log.Debug("Allocating NAS client...")
	m.nas, err = qmi.NewNASService(m.client)
	if err != nil {
		m.log.WithError(err).Warn("Failed to allocate NAS client")
	} else {
		m.log.Debug("Allocated NAS client")
	}

	// DMS
	m.log.Debug("Allocating DMS client...")
	m.dms, err = qmi.NewDMSService(m.client)
	if err != nil {
		m.log.WithError(err).Warn("Failed to allocate DMS client")
	} else {
		m.log.Debug("Allocated DMS client")
	}

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
		m.recoverWMSState()
	}

	// VOICE
	m.log.Debug("Allocating VOICE client...")
	m.voice, err = qmi.NewVOICEService(m.client)
	if err != nil {
		m.log.WithError(err).Warn("Failed to allocate VOICE client")
	} else {
		m.log.Debug("Allocated VOICE client")
		if cfg, ok := m.voiceIndicationRegistration(); ok {
			ctx, cancel := m.opContext(m.cfg.Timeouts.IndicationRegister)
			if err := m.withVOICERecovery("allocateServices.VOICEIndicationRegister", func(voice *qmi.VOICEService) error {
				return voice.IndicationRegister(ctx, cfg)
			}); err != nil {
				m.log.WithError(err).Warn("Failed to register VOICE indications")
			}
			cancel()
		} else {
			m.log.Debug("VOICE indications disabled by config")
		}
	}

	// IMS/IMSA/IMSP
	// 当前设备族在这些服务上经常返回 CTL client-id 分配失败（如 0x001f），
	// 且主链路不依赖它们，默认跳过分配以减少启动噪声和恢复抖动。
	m.ims = nil
	m.imsa = nil
	m.imsp = nil
	m.log.Debug("Skipping IMS/IMSA/IMSP client allocation")

	return nil
}

func (m *Manager) imsaIndicationRegistration() (qmi.IMSAIndicationRegistration, bool) {
	if m.cfg.DisableIMSAInd {
		return qmi.IMSAIndicationRegistration{}, false
	}
	return qmi.IMSAIndicationRegistration{
		RegistrationStatusChanged: true,
		ServicesStatusChanged:     true,
	}, true
}

func (m *Manager) voiceIndicationRegistration() (qmi.VoiceIndicationRegistration, bool) {
	if m.cfg.DisableVOICEInd {
		return qmi.VoiceIndicationRegistration{}, false
	}
	return qmi.VoiceIndicationRegistration{
		CallNotificationEvents:                 true,
		SupplementaryServiceNotificationEvents: true,
		USSDNotificationEvents:                 true,
	}, true
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
	ctx, cancel := m.opContext(m.cfg.Timeouts.StatusCheck)
	defer cancel()
	if currentFormat, err := m.wda.GetDataFormat(ctx); err == nil {
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
	ctx, cancel = m.opContext(m.cfg.Timeouts.StatusCheck)
	defer cancel()
	if err := m.wda.SetDataFormat(ctx, format); err != nil {
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
	ctx, cancel := m.opContext(m.cfg.Timeouts.SIMCheck)
	defer cancel()

	// Try UIM service first (modern modems) / 优先尝试UIM服务 (现代modem)
	if m.uim != nil {
		status, err = m.uim.GetCardStatus(ctx)
		if err == nil {
			m.log.Infof("SIM status (UIM): %s", status)
		}
	}

	// Fallback to DMS if UIM failed or not ready / 如果UIM失败或未就绪，回退到DMS
	if err != nil || status != qmi.SIMReady {
		dmsStatus, dmsErr := withDMSRecoveryValue(m, "checkSIM.GetSIMStatus", func(dms *qmi.DMSService) (qmi.SIMStatus, error) {
			return dms.GetSIMStatus(ctx)
		})
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
		if err := m.withDMSRecovery("checkSIM.VerifyPIN", func(dms *qmi.DMSService) error {
			return dms.VerifyPIN(ctx, m.cfg.PINCode)
		}); err != nil {
			return fmt.Errorf("PIN verification failed: %w", err)
		}
		m.log.Info("PIN verified successfully")
	}

	return nil
}

func (m *Manager) cleanup() {
	// Use timeout context for cleanup operations / 使用超时上下文进行清理操作
	m.stopScheduledTimers()
	cleanupCtx, cancel := m.opContext(m.cfg.Timeouts.Stop)
	defer cancel()

	m.mu.Lock()
	wds := m.wds
	wdsV6 := m.wdsV6
	nas := m.nas
	dms := m.dms
	uim := m.uim
	wda := m.wda
	wms := m.wms
	ims := m.ims
	imsa := m.imsa
	imsp := m.imsp
	voice := m.voice
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
	m.ims = nil
	m.imsa = nil
	m.imsp = nil
	m.voice = nil
	m.client = nil
	m.handleV4 = 0
	m.handleV6 = 0
	m.settings = nil
	m.muxIface = ""
	m.coreReady = false
	m.wmsTransportStatus = 0
	m.wmsTransportKnown = false
	m.wmsTransportUnsupported = false
	m.wmsTransportQueryError = ""
	m.wmsLastTransportWarn = ""
	m.wmsLastTransportWarnAt = time.Time{}
	m.wmsSMSCValue = ""
	m.wmsSMSCAvailable = false
	m.wmsSMSCKnown = false
	m.wmsSMSCStale = false
	m.wmsSMSCUpdatedAt = time.Time{}
	m.wmsSMSCLastCheckAt = time.Time{}
	m.wmsSMSCRefreshPending = false
	m.wmsRoutesKnown = false
	m.wmsLastNASRegistered = false
	m.wmsLastNASRegisteredKnown = false
	m.wmsReadinessRefreshPending = false
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
	if ims != nil {
		ims.Close()
	}
	if imsa != nil {
		imsa.Close()
	}
	if imsp != nil {
		imsp.Close()
	}
	if voice != nil {
		voice.Close()
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

	checkTicker := time.NewTicker(m.cfg.HealthPolicy.FullCheckInterval)
	defer checkTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return

		case evt := <-m.eventCh:
			m.handleEvent(evt)

		case <-checkTicker.C:
			m.eventCh <- eventCheckFull
		}
	}
}

func (m *Manager) handleEvent(evt internalEvent) {
	switch evt {
	case eventStart:
		_ = m.doConnect()

	case eventStop:
		m.doDisconnect()

	case eventCheckFull:
		m.doStatusCheck(true)

	case eventCheckTargeted:
		m.doStatusCheck(false)

	case eventPacketStatusChanged, eventServingSystemChanged:
		m.log.Debug("Received indication - scheduling status check")
		m.scheduleTargetedCheck()

	case eventModemReset:
		m.handleModemResetEvent()
	}
}

func (m *Manager) handleModemResetEvent() {
	if m == nil {
		return
	}

	m.modemResetMu.Lock()
	if m.modemResetRecovering {
		m.modemResetPending = true
		m.resetCoalesced.Add(1)
		m.modemResetMu.Unlock()
		m.log.Debug("Skip duplicated modem reset event while recovery is running")
		return
	}
	m.modemResetRecovering = true
	m.modemResetPending = false
	m.modemResetMu.Unlock()

	m.log.Warn("Modem reset detected!")
	recovered := m.doRecoverFromModemReset()

	m.modemResetMu.Lock()
	m.modemResetRecovering = false
	pending := m.modemResetPending
	m.modemResetPending = false
	m.modemResetMu.Unlock()

	if pending && recovered {
		m.log.Warn("Processing coalesced modem reset event after previous recovery")
		m.enqueueModemResetEvent("pending_after_recovery")
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
		initCtx, cancel := m.opContext(m.cfg.Timeouts.Init)
		client, err := qmi.NewClientWithOptions(initCtx, m.cfg.Device.ControlPath, m.cfg.ClientOptions)
		cancel()
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

func (m *Manager) doRecoverFromModemReset() bool {
	m.mu.RLock()
	desiredConnection := m.desiredConnection
	isStopping := m.state == StateStopping
	m.mu.RUnlock()
	if isStopping {
		return false
	}

	m.recoverAttempts.Add(1)
	m.doDisconnect()
	m.cleanup()

	if err := m.openClientAndAllocateServices(); err != nil {
		m.log.WithError(err).Warn("Failed to reinitialize QMI after modem reset")
		m.setState(StateDisconnected)
		m.recoverCount++
		delay := m.getRecoverDelay()
		m.log.Infof("Will retry reinit with backoff in %v (attempt=%d)", delay, m.recoverCount)
		m.scheduleAfter(delay, func() {
			m.enqueueModemResetEvent("recover_retry")
		})
		return false
	}
	m.recoverCount = 0
	m.recoverBackoffMs.Store(0)
	m.recoverSuccess.Add(1)

	if err := m.checkSIM(); err != nil {
		m.log.WithError(err).Warn("SIM check failed after modem reset")
	}

	m.mu.Lock()
	m.coreReady = true
	m.mu.Unlock()
	m.setState(StateDisconnected)
	if desiredConnection && m.cfg.AutoReconnect {
		m.scheduleAfter(2*time.Second, func() {
			m.eventCh <- eventStart
		})
	} else {
		m.log.Info("QMI core recovered without reconnecting data plane")
	}

	return true
}

func (m *Manager) getRecoverDelay() time.Duration {
	attempt := m.recoverCount
	if attempt <= 0 {
		attempt = 1
	}

	delay := recoverBackoffBase
	for i := 1; i < attempt; i++ {
		if delay >= recoverBackoffMax {
			delay = recoverBackoffMax
			break
		}
		delay *= 2
		if delay > recoverBackoffMax {
			delay = recoverBackoffMax
			break
		}
	}

	// add jitter in range [-20%, +20%]
	jitterRange := time.Duration(float64(delay) * recoverBackoffJitterRatio)
	if jitterRange > 0 {
		jitter := time.Duration(rand.Int63n(int64(jitterRange)*2+1)) - jitterRange
		delay += jitter
	}
	if delay < 100*time.Millisecond {
		delay = 100 * time.Millisecond
	}

	m.recoverBackoffMs.Store(uint64(delay / time.Millisecond))
	return delay
}

func (m *Manager) doConnect() error {
	m.mu.Lock()
	if !m.desiredConnection {
		m.mu.Unlock()
		return nil
	}
	if m.state == StateConnected || m.state == StateStopping {
		m.mu.Unlock()
		return nil
	}
	if m.state == StateConnecting && m.handleV4 != 0 {
		m.mu.Unlock()
		return fmt.Errorf("connection already in progress")
	}
	m.state = StateConnecting
	m.mu.Unlock()

	if m.wds == nil && m.wdsV6 == nil {
		err := fmt.Errorf("wds service not available")
		m.log.Error("WDS service not available")
		m.handleDialFailure(err)
		return err
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
			ctx, cancel := m.opContext(m.cfg.Timeouts.Dial)
			if err := m.wds.BindMuxDataPort(ctx, binding); err != nil {
				m.log.WithError(err).Error("WDS IPv4 BindMuxDataPort 失败")
				// 非致命，继续
			} else {
				m.log.Infof("WDS IPv4 已绑定 MuxID=%d", m.cfg.MuxID)
			}
			cancel()
		}

		// 如果有 IPv6 WDS，也需要绑定
		if m.wdsV6 != nil {
			ctx, cancel := m.opContext(m.cfg.Timeouts.Dial)
			if err := m.wdsV6.BindMuxDataPort(ctx, binding); err != nil {
				m.log.WithError(err).Warn("WDS IPv6 BindMuxDataPort 失败")
			} else {
				m.log.Infof("WDS IPv6 已绑定 MuxID=%d", m.cfg.MuxID)
			}
			cancel()
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

	dialCtx, cancelDial := m.opContext(m.cfg.Timeouts.Dial)
	defer cancelDial()

	// Log current signal and registration for context / 记录当前信号和注册状态以便调试
	if sig, err := m.getSignalStrength(dialCtx); err == nil {
		m.emitSignalUpdate(sig)
		if sig != nil {
			m.log.Infof("Signal: RSSI=%d, RSRP=%d, RSRQ=%d", sig.RSSI, sig.RSRP, sig.RSRQ)
		}
	}
	if ss, err := m.getServingSystem(dialCtx); err == nil {
		if ss != nil {
			m.log.Infof("Network: %s (%d-%d) Tech:%d", ss.RegistrationState, ss.MCC, ss.MNC, ss.RadioInterface)
		}
	}

	// Check registration / 检查注册状态
	if registered, regErr := withNASRecoveryValue(m, "doConnect.IsRegistered", func(nas *qmi.NASService) (bool, error) {
		return nas.IsRegistered(dialCtx)
	}); regErr == nil {
		if !registered {
			m.log.Info("Waiting for network registration...")
			// Don't fail - continue and let the dial fail if not registered / 不报错 - 继续执行，让拨号过程去处理未注册的情况
		}
	} else {
		m.log.WithError(regErr).Debug("Failed to query registration state before dialing")
	}

	// Start IPv4 data call / 启动IPv4数据呼叫
	if m.cfg.EnableIPv4 {
		m.log.Info("Starting IPv4 data call...")
		handle, err := m.wds.StartNetworkInterface(dialCtx,
			m.cfg.APN, m.cfg.Username, m.cfg.Password, m.cfg.AuthType, qmi.IpFamilyV4)
		if err != nil {
			m.log.WithError(err).Error("IPv4 dial failed")
			m.handleDialFailure(err)
			return err
		}
		m.handleV4 = handle
		m.log.Infof("IPv4 connected, handle=0x%08x", handle)
	}

	// Start IPv6 data call / 启动IPv6数据呼叫
	if m.cfg.EnableIPv6 && m.wdsV6 != nil {
		m.log.Info("Starting IPv6 data call...")
		handle, err := m.wdsV6.StartNetworkInterface(dialCtx,
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
		m.handleDialFailure(err)
		return err
	}

	m.setState(StateConnected)
	m.retryCount = 0
	m.log.Info("Connection established successfully!")

	// Emit connected event / 发送连接事件
	m.mu.RLock()
	settings := m.settings
	m.mu.RUnlock()
	m.emitEvent(Event{Type: EventConnected, State: StateConnected, Settings: settings})

	return nil
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
		ctx, cancel := m.opContext(m.cfg.Timeouts.StatusCheck)
		settings, err := m.wds.GetRuntimeSettings(ctx, qmi.IpFamilyV4)
		cancel()
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
		ctx, cancel := m.opContext(m.cfg.Timeouts.StatusCheck)
		settingsV6, err := m.wdsV6.GetRuntimeSettings(ctx, qmi.IpFamilyV6)
		cancel()
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
	ctx, cancel := m.opContext(m.cfg.Timeouts.Stop)
	defer cancel()

	if m.handleV4 != 0 && m.wds != nil {
		_ = m.wds.StopNetworkInterface(ctx, m.handleV4)
		m.handleV4 = 0
	}
	if m.handleV6 != 0 && m.wdsV6 != nil {
		_ = m.wdsV6.StopNetworkInterface(ctx, m.handleV6)
		m.handleV6 = 0

	}

	netcfg.FlushAddresses(m.cfg.Device.NetInterface)
	netcfg.FlushRoutes(m.cfg.Device.NetInterface)
	netcfg.BringDown(m.cfg.Device.NetInterface)

	m.mu.Lock()
	m.settings = nil
	m.mu.Unlock()

	m.setState(StateDisconnected)

	// Emit disconnected event / 发送断开连接事件
	m.emitEvent(Event{Type: EventDisconnected, State: StateDisconnected})
}

func (m *Manager) doStatusCheck(full bool) {
	m.mu.RLock()
	if m.isRotating {
		m.mu.RUnlock()
		return // Skip check during rotation / 轮换期间跳过检查
	}
	currentState := m.state
	desiredConnection := m.desiredConnection
	m.mu.RUnlock()

	if currentState == StateStopping || currentState == StateDisconnected {
		return
	}

	if m.client == nil {
		return
	}

	m.statusChecks.Add(1)
	ctx, cancel := m.opContext(m.cfg.Timeouts.StatusCheck)
	defer cancel()

	// 1. Log Signal Strength & Registration / 1. 记录信号强度和注册状态
	if full {
		sig, err := m.getSignalStrength(ctx)
		if err == nil {
			m.emitSignalUpdate(sig)
			if sig != nil {
				m.log.Infof("Signal: RSSI=%d, RSRP=%d, RSRQ=%d", sig.RSSI, sig.RSRP, sig.RSRQ)
			}
		}
		ss, err := m.getServingSystem(ctx)
		if err == nil {
			if ss != nil {
				m.log.Infof("Network: %s (MCC:%d MNC:%d) Tech:%d", ss.RegistrationState, ss.MCC, ss.MNC, ss.RadioInterface)
			}
		}
	}

	// 2. Query connection status / 2. 查询连接状态
	status, err := m.getPacketServiceState(ctx)
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
			m.mu.RLock()
			settings := m.settings
			m.mu.RUnlock()
			m.emitEvent(Event{Type: EventConnected, State: StateConnected, Settings: settings})
		} else {
			// Smart Check: Verify IP consistency (match C version) / 智能检查: 验证IP一致性 (匹配C版本逻辑)
			shouldVerifyIP := full
			if !shouldVerifyIP && m.cfg.HealthPolicy.IPConsistencyInterval > 0 {
				m.mu.RLock()
				lastIPCheck := m.lastIPCheck
				m.mu.RUnlock()
				shouldVerifyIP = time.Since(lastIPCheck) >= m.cfg.HealthPolicy.IPConsistencyInterval
			}
			if shouldVerifyIP {
				if err := m.verifyIPConsistency(); err != nil {
					m.log.WithError(err).Warn("IP consistency check failed - triggering redial")
					m.doDisconnect()
					m.mu.RLock()
					isStopping := m.state == StateStopping
					m.mu.RUnlock()
					if m.cfg.AutoReconnect && desiredConnection && !isStopping {
						m.emitEvent(Event{Type: EventReconnecting, State: StateDisconnected, Error: err})
						m.eventCh <- eventStart
					}
				} else {
					m.mu.Lock()
					m.lastIPCheck = time.Now()
					m.mu.Unlock()
				}
			}
		}
	} else if status == qmi.StatusDisconnected {
		if currentState == StateConnected {
			m.log.Warn("Connection lost!")
			m.handleV4 = 0
			netcfg.FlushAddresses(m.cfg.Device.NetInterface)
			m.setState(StateDisconnected)
			m.emitEvent(Event{Type: EventDisconnected, State: StateDisconnected})

			// Trigger reconnect
			m.mu.RLock()
			isStopping := m.state == StateStopping
			m.mu.RUnlock()
			if m.cfg.AutoReconnect && desiredConnection && !isStopping {
				m.emitEvent(Event{Type: EventReconnecting, State: StateDisconnected})
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
	ctx, cancel := m.opContext(m.cfg.Timeouts.StatusCheck)
	defer cancel()
	newSettings, err := m.wds.GetRuntimeSettings(ctx, qmi.IpFamilyV4)
	if err != nil {
		return err
	}

	// Compare with recorded IP / 与记录的IP进行比较
	if !newSettings.IPv4Address.Equal(m.settings.IPv4Address) {
		return fmt.Errorf("local IP %s != modem IP %s", m.settings.IPv4Address, newSettings.IPv4Address)
	}

	return nil
}

func (m *Manager) handleDialFailure(err error) {
	m.setState(StateDisconnected)
	m.emitEvent(Event{Type: EventDialFailed, State: StateDisconnected, Error: err})

	m.mu.RLock()
	desiredConnection := m.desiredConnection
	m.mu.RUnlock()
	if !m.cfg.AutoReconnect || !desiredConnection {
		return
	}

	delay := m.getRetryDelay()
	m.retryCount++
	if m.retryCount == m.cfg.RetryPolicy.RadioResetAfter {
		go m.RadioReset()
	}
	m.emitEvent(Event{Type: EventReconnecting, State: StateDisconnected, Error: err})
	m.log.Infof("Will retry in %v (%d/%d)", delay, m.retryCount, len(m.retryDelays))
	m.reconnectScheduled.Add(1)

	m.scheduleAfter(delay, func() {
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
		event := m.qmiIndicationEvent(EventPacketServiceStatusChanged, evt)
		if evt.Packet != nil {
			status, err := qmi.ParsePacketServiceStatusIndication(evt.Packet)
			if err != nil {
				m.log.WithError(err).Warn("Failed to parse WDS packet service status indication")
			} else {
				event.PacketServiceStatus = status
			}
		}
		m.emitEvent(event)
		m.eventCh <- eventPacketStatusChanged

	case qmi.EventServingSystemChanged:
		event := m.qmiIndicationEvent(EventServingSystemChanged, evt)
		if evt.Packet != nil {
			// SysInfoInd 独立拆出补充网络动态快照
			if evt.MessageID == qmi.NASGetSysInfo {
				if sysInfo, err := qmi.ParseSysInfoIndication(evt.Packet); err == nil {
					m.snapshot.updateSysInfo(sysInfo)
				}
			}

			if qmi.FindTLV(evt.Packet.TLVs, 0x01) != nil || qmi.FindTLV(evt.Packet.TLVs, 0x12) != nil {
				info, err := qmi.ParseServingSystemIndication(evt.Packet)
				if err != nil {
					m.log.WithError(err).Warn("Failed to parse NAS serving system indication")
				} else {
					event.ServingSystem = info
					// 原地更新快照，供上层零 IPC 读取运营商/注册状态
					m.snapshot.updateServing(info)
				}
			}
		}
		if event.ServingSystem != nil {
			m.mu.Lock()
			notify := m.regNotify
			m.mu.Unlock()
			if event.ServingSystem.RegistrationState == qmi.RegStateRegistered && notify != nil {
				select {
				case notify <- true:
				default:
				}
			}
			if (event.ServingSystem.RegistrationState == qmi.RegStateRegistered || event.ServingSystem.RegistrationState == qmi.RegStateRoaming) && event.ServingSystem.PSAttached {
				m.maybeRefreshWMSReadiness("serving-system-recovered")
			}
		}
		m.emitEvent(event)
		m.eventCh <- eventServingSystemChanged

	case qmi.EventModemReset:
		m.emitQMIIndicationEvent(EventModemReset, evt)
		m.enqueueModemResetEvent("qmi_indication")

	case qmi.EventNewMessage:
		m.log.Info("New SMS Indication received")
		if tlv := qmi.FindTLV(evt.Packet.TLVs, 0x10); tlv != nil && len(tlv.Value) >= 5 {
			// 0x10 = MT Message: StorageType (1) + MemoryIndex (4) / 0x10 = MT 消息: 存储类型(1) + 内存索引(4)
			storage := tlv.Value[0]
			index := binary.LittleEndian.Uint32(tlv.Value[1:5])
			m.emitEvent(Event{
				Type:        EventNewSMS,
				State:       m.State(),
				SMSIndex:    index,
				StorageType: storage,
				RawQMIType:  evt.Type,
				ServiceID:   evt.ServiceID,
				MessageID:   evt.MessageID,
			})
		} else if tlv := qmi.FindTLV(evt.Packet.TLVs, 0x11); tlv != nil && len(tlv.Value) >= 6 {
			// 0x11 = Transfer Route MT Message: AckIndicator(1) + TransactionID(4) + Format(1) + RawData(...)
			// 0x11 = 传输路由 MT 消息: Ack(1) + 事务ID(4) + 格式(1) + 原始数据(...)
			pdu := tlv.Value[6:]
			// Copy PDU to prevent underlying buffer corruption / 拷贝 PDU 防止底层缓冲区损坏
			pduCopy := make([]byte, len(pdu))
			copy(pduCopy, pdu)
			m.emitEvent(Event{
				Type:       EventNewSMSRaw,
				State:      m.State(),
				Pdu:        pduCopy,
				RawQMIType: evt.Type,
				ServiceID:  evt.ServiceID,
				MessageID:  evt.MessageID,
			})
		} else {
			// Just emit event without index if TLV missing
			m.emitEvent(Event{
				Type:       EventNewSMS,
				State:      m.State(),
				SMSIndex:   0xFFFFFFFF,
				RawQMIType: evt.Type,
				ServiceID:  evt.ServiceID,
				MessageID:  evt.MessageID,
			})
		}

	case qmi.EventIMSRegistrationStatus:
		info, err := qmi.ParseIMSARegistrationStatusChanged(evt.Packet)
		if err != nil {
			m.log.WithError(err).Warn("Failed to parse IMSA registration status indication")
			return
		}
		m.emitEvent(Event{
			Type:            EventIMSRegistrationStatus,
			State:           m.State(),
			IMSRegistration: info,
			RawQMIType:      evt.Type,
			ServiceID:       evt.ServiceID,
			MessageID:       evt.MessageID,
		})

	case qmi.EventIMSServicesStatus:
		info, err := qmi.ParseIMSAServicesStatusChanged(evt.Packet)
		if err != nil {
			m.log.WithError(err).Warn("Failed to parse IMSA services status indication")
			return
		}
		m.emitEvent(Event{
			Type:        EventIMSServicesStatus,
			State:       m.State(),
			IMSServices: info,
			RawQMIType:  evt.Type,
			ServiceID:   evt.ServiceID,
			MessageID:   evt.MessageID,
		})

	case qmi.EventIMSSettingsChanged:
		info, err := qmi.ParseIMSServicesEnabledSetting(evt.Packet)
		if err != nil {
			m.log.WithError(err).Warn("Failed to parse IMS settings changed indication")
			return
		}
		m.emitEvent(Event{
			Type:        EventIMSSettingsChanged,
			State:       m.State(),
			IMSSettings: info,
			RawQMIType:  evt.Type,
			ServiceID:   evt.ServiceID,
			MessageID:   evt.MessageID,
		})

	case qmi.EventVoiceCallStatus:
		info, err := qmi.ParseVoiceAllCallStatus(evt.Packet)
		if err != nil {
			m.log.WithError(err).Warn("Failed to parse VOICE call status indication")
			return
		}
		m.emitEvent(Event{
			Type:       EventVoiceCallStatus,
			State:      m.State(),
			VoiceCalls: info,
			RawQMIType: evt.Type,
			ServiceID:  evt.ServiceID,
			MessageID:  evt.MessageID,
		})

	case qmi.EventVoiceSupplementaryService:
		info, err := qmi.ParseVoiceSupplementaryServiceIndication(evt.Packet)
		if err != nil {
			m.log.WithError(err).Warn("Failed to parse VOICE supplementary service indication")
			return
		}
		m.emitEvent(Event{
			Type:               EventVoiceSupplementaryService,
			State:              m.State(),
			VoiceSupplementary: info,
			RawQMIType:         evt.Type,
			ServiceID:          evt.ServiceID,
			MessageID:          evt.MessageID,
		})

	case qmi.EventUSSD:
		info, err := qmi.ParseVoiceUSSDIndication(evt.Packet)
		if err != nil {
			m.log.WithError(err).Warn("Failed to parse VOICE USSD indication")
			return
		}
		m.emitEvent(Event{
			Type:       EventVoiceUSSD,
			State:      m.State(),
			VoiceUSSD:  info,
			RawQMIType: evt.Type,
			ServiceID:  evt.ServiceID,
			MessageID:  evt.MessageID,
		})

	case qmi.EventVoiceUSSDReleased:
		m.emitEvent(Event{
			Type:       EventVoiceUSSDReleased,
			State:      m.State(),
			RawQMIType: evt.Type,
			ServiceID:  evt.ServiceID,
			MessageID:  evt.MessageID,
		})

	case qmi.EventVoiceUSSDNoWaitResult:
		info, err := qmi.ParseVoiceUSSDNoWaitIndication(evt.Packet)
		if err != nil {
			m.log.WithError(err).Warn("Failed to parse VOICE USSD no-wait indication")
			return
		}
		m.emitEvent(Event{
			Type:            EventVoiceUSSDNoWaitResult,
			State:           m.State(),
			VoiceUSSDNoWait: info,
			RawQMIType:      evt.Type,
			ServiceID:       evt.ServiceID,
			MessageID:       evt.MessageID,
		})

	case qmi.EventSimStatusChanged:
		m.snapshot.ResetIdentities(false)
		m.PreWarmIdentities(false)
		m.emitQMIIndicationEvent(EventSimStatusChanged, evt)

	case qmi.EventUIMSessionClosed:
		m.markWMSReadinessStale()
		event := m.qmiIndicationEvent(EventUIMSessionClosed, evt)
		event.TLVMeta = packetTLVMeta(evt.Packet)
		m.emitEvent(event)

	case qmi.EventUIMRefresh:
		m.snapshot.ResetIdentities(false)
		m.PreWarmIdentities(false)
		event := m.qmiIndicationEvent(EventUIMRefresh, evt)
		event.TLVMeta = packetTLVMeta(evt.Packet)
		if evt.Packet != nil {
			info, err := qmi.ParseUIMRefreshIndication(evt.Packet)
			if err != nil {
				m.log.WithError(err).Warn("Failed to parse UIM refresh indication")
			} else {
				event.UIMRefresh = info
			}
		}
		m.emitEvent(event)

	case qmi.EventUIMSlotStatus:
		event := m.qmiIndicationEvent(EventUIMSlotStatus, evt)
		event.TLVMeta = packetTLVMeta(evt.Packet)
		if evt.Packet != nil {
			info, err := qmi.ParseUIMSlotStatusIndication(evt.Packet)
			if err != nil {
				m.log.WithError(err).Warn("Failed to parse UIM slot status indication")
			} else {
				event.UIMSlotStatus = info
			}
		}
		m.emitEvent(event)

	case qmi.EventWMSSMSCAddress:
		event := m.qmiIndicationEvent(EventWMSSMSCAddress, evt)
		if evt.Packet != nil {
			info, err := qmi.ParseWMSSMSCAddressIndication(evt.Packet)
			if err != nil {
				m.log.WithError(err).Warn("Failed to parse WMS SMSC address indication")
			} else {
				event.WMSSMSCAddress = info
				digits := strings.TrimSpace(info.Digits)
				known := digits != ""
				now := time.Now()
				m.setWMSSMSCState(digits, known, known, false, now, now)
			}
		}
		m.emitEvent(event)

	case qmi.EventWMSTransportNetworkRegistrationStatus:
		event := m.qmiIndicationEvent(EventWMSTransportNetworkRegistrationStatus, evt)
		if evt.Packet != nil {
			status, err := qmi.ParseWMSTransportNetworkRegistrationStatusIndication(evt.Packet)
			if err != nil {
				m.log.WithError(err).Warn("Failed to parse WMS transport registration status indication")
			} else {
				event.WMSTransportRegistration = status
				m.setWMSTransportState(status, true, false)
			}
		}
		m.emitEvent(event)

	default:
		event := m.qmiIndicationEvent(EventUnknownIndication, evt)
		event.TLVMeta = packetTLVMeta(evt.Packet)
		m.emitEvent(event)
	}
}

// ============================================================================
// Radio Reset Recovery
// ============================================================================

// RadioReset performs a radio power cycle to recover from stuck states / 射频重置: 执行射频电源循环以从卡死状态恢复
func (m *Manager) RadioReset() error {
	m.log.Info("Performing radio reset...")
	ctx, cancel := m.opContext(m.cfg.Timeouts.Stop)
	defer cancel()

	if err := m.withDMSRecovery("RadioReset.RadioPowerCycle", func(dms *qmi.DMSService) error {
		// Turn radio off / 关闭射频
		if powerErr := dms.RadioPower(ctx, false); powerErr != nil {
			return fmt.Errorf("failed to turn radio off: %w", powerErr)
		}
		// Turn radio on / 打开射频
		if powerErr := dms.RadioPower(ctx, true); powerErr != nil {
			return fmt.Errorf("failed to turn radio on: %w", powerErr)
		}
		return nil
	}); err != nil {
		return err
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
	ctx, cancel := m.opContext(m.cfg.Timeouts.Dial)
	defer cancel()
	return withWMSRecoveryValue(m, "ListSMS", func(wms *qmi.WMSService) ([]struct {
		Index uint32
		Tag   qmi.MessageTagType
	}, error) {
		return wms.ListMessages(ctx, storageType, tag)
	})
}

// ReadRawSMS reads a raw SMS message PDU / ReadRawSMS 读取原始短信 PDU
func (m *Manager) ReadRawSMS(storageType uint8, index uint32) ([]byte, error) {
	ctx, cancel := m.opContext(m.cfg.Timeouts.Dial)
	defer cancel()
	return withWMSRecoveryValue(m, "ReadRawSMS", func(wms *qmi.WMSService) ([]byte, error) {
		return wms.RawReadMessage(ctx, storageType, index)
	})
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
	ctx, cancel := m.opContext(m.cfg.Timeouts.Dial)
	defer cancel()
	return m.withWMSRecovery("SendRawSMS", func(wms *qmi.WMSService) error {
		return wms.SendRawMessage(ctx, format, pdu)
	})
}

// SendSMS sends a text message / SendSMS 发送文本短信
func (m *Manager) SendSMS(number, text string) error {
	pdu, err := m.encodeSMS(number, text)
	if err != nil {
		return err
	}

	ctx, cancel := m.opContext(m.cfg.Timeouts.Dial)
	defer cancel()
	return m.withWMSRecovery("SendSMS", func(wms *qmi.WMSService) error {
		return wms.SendRawMessage(ctx, 0x06, pdu)
	})
}

// encodeSMS encodes a text message into a 7-bit PDU format using warthog618/sms / encodeSMS 使用 warthog618/sms 将文本消息编码为 7-bit PDU 格式
func (m *Manager) encodeSMS(number, text string) ([]byte, error) {
	normalizedNumber := strings.TrimSpace(number)
	options := []sms.EncoderOption{sms.AsSubmit, sms.To(normalizedNumber)}

	pdus, err := sms.Encode([]byte(text), options...)
	if err != nil {
		return nil, err
	}
	if len(pdus) == 0 {
		return nil, fmt.Errorf("no PDUs generated")
	}
	if isLikelyShortCode(normalizedNumber) {
		da := pdus[0].DA
		da.SetTypeOfNumber(tpdu.TonUnknown)
		da.SetNumberingPlan(tpdu.NpISDN)
		pdus[0].DA = da
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

func isLikelyShortCode(phone string) bool {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return false
	}
	if strings.HasPrefix(phone, "+") {
		return false
	}
	digits := strings.TrimLeft(phone, "0123456789")
	return digits == "" && len(phone) <= 6
}
