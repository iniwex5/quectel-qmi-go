package manager

import (
	"sync"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

// ============================================================================
// State Callbacks / 状态回调
// 允许调用方监听连接状态变化
// ============================================================================

// EventType represents the type of connection event / EventType 表示连接事件类型
type EventType int

const (
	EventConnected                             EventType = iota // Connection established / 连接已建立
	EventDisconnected                                           // Connection lost / 连接丢失
	EventIPChanged                                              // IP address changed / IP 地址变化
	EventSignalUpdate                                           // Signal strength updated / 信号强度更新
	EventDialFailed                                             // Dial attempt failed / 拨号失败
	EventReconnecting                                           // Starting reconnection / 开始重连
	EventNewSMS                                                 // New SMS received (storage index) / 收到新短信（存储索引）
	EventNewSMSRaw                                              // New SMS received raw / 收到新的原始短消息 (直投)
	EventIMSRegistrationStatus                                  // IMS registration status changed / IMS 注册状态变化
	EventIMSServicesStatus                                      // IMS services status changed / IMS 业务状态变化
	EventIMSSettingsChanged                                     // IMS settings changed / IMS 配置状态变化
	EventVoiceCallStatus                                        // Voice call status indication / 语音通话状态指示
	EventVoiceUSSD                                              // Voice USSD indication / 语音 USSD 指示
	EventVoiceUSSDReleased                                      // Voice USSD released indication / 语音 USSD 释放指示
	EventVoiceSupplementaryService                              // Voice supplementary service indication / 语音补充业务指示
	EventVoiceUSSDNoWaitResult                                  // Voice originate USSD no wait result / 语音异步 USSD 结果
	EventWMSSMSCAddress                                         // WMS SMSC address indication / WMS 短信中心地址指示
	EventWMSTransportNetworkRegistrationStatus                  // WMS transport network registration status indication / WMS 传输网络注册状态指示
	EventPacketServiceStatusChanged                             // Packet service status changed indication / 数据服务状态改变指示
	EventServingSystemChanged                                   // Serving system changed indication / 服务系统改变指示
	EventModemReset                                             // Modem reset indication / Modem 重置指示
	EventSimStatusChanged                                       // SIM status changed indication / SIM 状态改变指示
	EventUIMSessionClosed                                       // UIM session closed indication / UIM 会话关闭指示
	EventUIMRefresh                                             // UIM refresh indication / UIM 刷新指示
	EventUIMSlotStatus                                          // UIM slot status indication / UIM 卡槽状态指示
	EventUnknownIndication                                      // Unknown indication / 未知指示
)

func (e EventType) String() string {
	switch e {
	case EventConnected:
		return "Connected"
	case EventDisconnected:
		return "Disconnected"
	case EventIPChanged:
		return "IPChanged"
	case EventSignalUpdate:
		return "SignalUpdate"
	case EventDialFailed:
		return "DialFailed"
	case EventReconnecting:
		return "Reconnecting"
	case EventNewSMS:
		return "NewSMS"
	case EventNewSMSRaw:
		return "NewSMSRaw"
	case EventIMSRegistrationStatus:
		return "IMSRegistrationStatus"
	case EventIMSServicesStatus:
		return "IMSServicesStatus"
	case EventIMSSettingsChanged:
		return "IMSSettingsChanged"
	case EventVoiceCallStatus:
		return "VoiceCallStatus"
	case EventVoiceUSSD:
		return "VoiceUSSD"
	case EventVoiceUSSDReleased:
		return "VoiceUSSDReleased"
	case EventVoiceSupplementaryService:
		return "VoiceSupplementaryService"
	case EventVoiceUSSDNoWaitResult:
		return "VoiceUSSDNoWaitResult"
	case EventWMSSMSCAddress:
		return "WMSSMSCAddress"
	case EventWMSTransportNetworkRegistrationStatus:
		return "WMSTransportNetworkRegistrationStatus"
	case EventPacketServiceStatusChanged:
		return "PacketServiceStatusChanged"
	case EventServingSystemChanged:
		return "ServingSystemChanged"
	case EventModemReset:
		return "ModemReset"
	case EventSimStatusChanged:
		return "SimStatusChanged"
	case EventUIMSessionClosed:
		return "UIMSessionClosed"
	case EventUIMRefresh:
		return "UIMRefresh"
	case EventUIMSlotStatus:
		return "UIMSlotStatus"
	case EventUnknownIndication:
		return "UnknownIndication"
	default:
		return "Unknown"
	}
}

// Event represents a connection event / Event 表示连接事件
type Event struct {
	Type                     EventType                                // Event type / 事件类型
	State                    State                                    // Current state / 当前状态
	Settings                 *qmi.RuntimeSettings                     // IP settings (for Connected/IPChanged) / IP 设置
	Error                    error                                    // Error (for DialFailed) / 错误信息
	Signal                   *qmi.SignalStrength                      // Signal info (for SignalUpdate) / 信号信息
	SMSIndex                 uint32                                   // SMS index (for NewSMS) / 短信索引
	StorageType              uint8                                    // SMS storage type (for NewSMS) / 短信存储类型
	Pdu                      []byte                                   // SMS Raw Data PDU (for EventNewSMSRaw) / 短信原始 PDU 数据
	IMSRegistration          *qmi.IMSARegistrationStatus              // IMS registration status / IMS 注册状态
	IMSServices              *qmi.IMSAServicesStatus                  // IMS services status / IMS 业务状态
	IMSSettings              *qmi.IMSServicesEnabledSettings          // IMS enabled settings / IMS 配置状态
	VoiceCalls               *qmi.VoiceAllCallInfo                    // Voice call status / 语音通话状态
	VoiceUSSD                *qmi.VoiceUSSDIndication                 // Voice USSD / 语音 USSD
	VoiceSupplementary       *qmi.VoiceSupplementaryServiceIndication // Voice supplementary service / 语音补充业务
	VoiceUSSDNoWait          *qmi.VoiceUSSDNoWaitIndication           // Voice async USSD result / 异步 USSD 结果
	ServingSystem            *qmi.ServingSystem                       // NAS serving system / NAS 服务系统
	PacketServiceStatus      qmi.ConnectionStatus                     // WDS packet service status / WDS 数据服务状态
	UIMRefresh               *qmi.UIMRefreshIndication                // UIM refresh indication payload / UIM 刷新指示载荷
	UIMSlotStatus            *qmi.UIMSlotStatus                       // UIM slot status indication payload / UIM 卡槽状态指示载荷
	WMSSMSCAddress           *qmi.WMSSMSCAddress                      // WMS SMSC address / WMS 短信中心地址
	WMSTransportRegistration qmi.WMSTransportNetworkRegistration      // WMS transport registration / WMS 传输网络注册状态
	TLVMeta                  []qmi.TLVMeta                            // TLV metadata for diagnostics / TLV 元数据（诊断用）
	RawQMIType               qmi.EventType                            // Raw QMI event type / 原始 QMI 事件类型
	ServiceID                uint8                                    // QMI service id / QMI 服务 ID
	MessageID                uint16                                   // QMI message id / QMI 消息 ID
}

// EventHandler is a callback function for connection events / EventHandler 是连接事件的回调函数
type EventHandler func(event Event)

// EventEmitter manages event handlers / EventEmitter 管理事件处理器
type EventEmitter struct {
	mu       sync.RWMutex
	handlers []EventHandler
	queue    chan Event
}

// NewEventEmitter creates a new event emitter / NewEventEmitter 创建新的事件发射器
func NewEventEmitter() *EventEmitter {
	return NewEventEmitterWithQueueSize(128)
}

func NewEventEmitterWithQueueSize(size int) *EventEmitter {
	if size <= 0 {
		size = 128
	}
	e := &EventEmitter{
		handlers: make([]EventHandler, 0),
		queue:    make(chan Event, size),
	}
	go e.loop()
	return e
}

func (e *EventEmitter) loop() {
	for event := range e.queue {
		e.mu.RLock()
		handlers := make([]EventHandler, len(e.handlers))
		copy(handlers, e.handlers)
		e.mu.RUnlock()

		for _, handler := range handlers {
			func(h EventHandler) {
				defer func() {
					if recover() != nil {
						// Keep the emitter alive even if a callback panics.
					}
				}()
				h(event)
			}(handler)
		}
	}
}

// On registers an event handler / On 注册事件处理器
func (e *EventEmitter) On(handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = append(e.handlers, handler)
}

// Emit sends an event to all handlers / Emit 向所有处理器发送事件
func (e *EventEmitter) Emit(event Event) {
	if e == nil {
		return
	}
	e.queue <- event
}

// Clear removes all handlers / Clear 移除所有处理器
func (e *EventEmitter) Clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = e.handlers[:0]
}

// ============================================================================
// Convenience Methods on Manager / Manager 便捷方法
// ============================================================================

// OnEvent registers a callback for all events / OnEvent 为所有事件注册回调
func (m *Manager) OnEvent(handler EventHandler) {
	if m.events == nil {
		m.events = NewEventEmitter()
	}
	m.events.On(handler)
}

// OnConnect registers a callback for connect events / OnConnect 为连接事件注册回调
func (m *Manager) OnConnect(handler func(settings *qmi.RuntimeSettings)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventConnected && handler != nil {
			handler(e.Settings)
		}
	})
}

// OnDisconnect registers a callback for disconnect events / OnDisconnect 为断开连接事件注册回调
func (m *Manager) OnDisconnect(handler func()) {
	m.OnEvent(func(e Event) {
		if e.Type == EventDisconnected && handler != nil {
			handler()
		}
	})
}

// OnIPChange registers a callback for IP change events / OnIPChange 为 IP 变化事件注册回调
func (m *Manager) OnIPChange(handler func(settings *qmi.RuntimeSettings)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventIPChanged && handler != nil {
			handler(e.Settings)
		}
	})
}

// OnError registers a callback for error events / OnError 为错误事件注册回调
func (m *Manager) OnError(handler func(err error)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventDialFailed && handler != nil {
			handler(e.Error)
		}
	})
}

// OnIMSRegistrationStatus registers a callback for IMSA registration status indications.
func (m *Manager) OnIMSRegistrationStatus(handler func(info *qmi.IMSARegistrationStatus)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventIMSRegistrationStatus && handler != nil {
			handler(e.IMSRegistration)
		}
	})
}

// OnIMSServicesStatus registers a callback for IMSA services status indications.
func (m *Manager) OnIMSServicesStatus(handler func(info *qmi.IMSAServicesStatus)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventIMSServicesStatus && handler != nil {
			handler(e.IMSServices)
		}
	})
}

// OnIMSSettingsChanged registers a callback for IMS settings change indications.
func (m *Manager) OnIMSSettingsChanged(handler func(info *qmi.IMSServicesEnabledSettings)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventIMSSettingsChanged && handler != nil {
			handler(e.IMSSettings)
		}
	})
}

// OnVoiceCallStatus registers a callback for voice call status indications.
func (m *Manager) OnVoiceCallStatus(handler func(info *qmi.VoiceAllCallInfo)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceCallStatus && handler != nil {
			handler(e.VoiceCalls)
		}
	})
}

// OnVoiceUSSD registers a callback for voice USSD indications.
func (m *Manager) OnVoiceUSSD(handler func(info *qmi.VoiceUSSDIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceUSSD && handler != nil {
			handler(e.VoiceUSSD)
		}
	})
}

// OnVoiceUSSDReleased registers a callback for USSD release indications.
func (m *Manager) OnVoiceUSSDReleased(handler func()) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceUSSDReleased && handler != nil {
			handler()
		}
	})
}

// OnVoiceSupplementaryService registers a callback for supplementary service indications.
func (m *Manager) OnVoiceSupplementaryService(handler func(info *qmi.VoiceSupplementaryServiceIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceSupplementaryService && handler != nil {
			handler(e.VoiceSupplementary)
		}
	})
}

// OnVoiceUSSDNoWaitResult registers a callback for async USSD results.
func (m *Manager) OnVoiceUSSDNoWaitResult(handler func(info *qmi.VoiceUSSDNoWaitIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventVoiceUSSDNoWaitResult && handler != nil {
			handler(e.VoiceUSSDNoWait)
		}
	})
}

// OnUIMRefresh registers a callback for UIM refresh indications.
func (m *Manager) OnUIMRefresh(handler func(info *qmi.UIMRefreshIndication)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventUIMRefresh && handler != nil {
			handler(e.UIMRefresh)
		}
	})
}

// OnUIMSlotStatus registers a callback for UIM slot status indications.
func (m *Manager) OnUIMSlotStatus(handler func(info *qmi.UIMSlotStatus)) {
	m.OnEvent(func(e Event) {
		if e.Type == EventUIMSlotStatus && handler != nil {
			handler(e.UIMSlotStatus)
		}
	})
}
