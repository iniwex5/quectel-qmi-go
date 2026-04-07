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
	EventConnected                 EventType = iota // Connection established / 连接已建立
	EventDisconnected                               // Connection lost / 连接丢失
	EventIPChanged                                  // IP address changed / IP 地址变化
	EventSignalUpdate                               // Signal strength updated / 信号强度更新
	EventDialFailed                                 // Dial attempt failed / 拨号失败
	EventReconnecting                               // Starting reconnection / 开始重连
	EventNewSMS                                     // New SMS received (storage index) / 收到新短信（存储索引）
	EventNewSMSRaw                                  // New SMS received raw / 收到新的原始短消息 (直投)
	EventIMSRegistrationStatus                      // IMS registration status changed / IMS 注册状态变化
	EventIMSServicesStatus                          // IMS services status changed / IMS 业务状态变化
	EventIMSSettingsChanged                         // IMS settings changed / IMS 配置状态变化
	EventVoiceCallStatus                            // Voice call status indication / 语音通话状态指示
	EventVoiceUSSD                                  // Voice USSD indication / 语音 USSD 指示
	EventVoiceUSSDReleased                          // Voice USSD released indication / 语音 USSD 释放指示
	EventVoiceSupplementaryService                  // Voice supplementary service indication / 语音补充业务指示
	EventVoiceUSSDNoWaitResult                      // Voice originate USSD no wait result / 语音异步 USSD 结果
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
	default:
		return "Unknown"
	}
}

// Event represents a connection event / Event 表示连接事件
type Event struct {
	Type               EventType                                // Event type / 事件类型
	State              State                                    // Current state / 当前状态
	Settings           *qmi.RuntimeSettings                     // IP settings (for Connected/IPChanged) / IP 设置
	Error              error                                    // Error (for DialFailed) / 错误信息
	Signal             *qmi.SignalStrength                      // Signal info (for SignalUpdate) / 信号信息
	SMSIndex           uint32                                   // SMS index (for NewSMS) / 短信索引
	StorageType        uint8                                    // SMS storage type (for NewSMS) / 短信存储类型
	Pdu                []byte                                   // SMS Raw Data PDU (for EventNewSMSRaw) / 短信原始 PDU 数据
	IMSRegistration    *qmi.IMSARegistrationStatus              // IMS registration status / IMS 注册状态
	IMSServices        *qmi.IMSAServicesStatus                  // IMS services status / IMS 业务状态
	IMSSettings        *qmi.IMSServicesEnabledSettings          // IMS enabled settings / IMS 配置状态
	VoiceCalls         *qmi.VoiceAllCallInfo                    // Voice call status / 语音通话状态
	VoiceUSSD          *qmi.VoiceUSSDIndication                 // Voice USSD / 语音 USSD
	VoiceSupplementary *qmi.VoiceSupplementaryServiceIndication // Voice supplementary service / 语音补充业务
	VoiceUSSDNoWait    *qmi.VoiceUSSDNoWaitIndication           // Voice async USSD result / 异步 USSD 结果
}

// EventHandler is a callback function for connection events / EventHandler 是连接事件的回调函数
type EventHandler func(event Event)

// EventEmitter manages event handlers / EventEmitter 管理事件处理器
type EventEmitter struct {
	mu       sync.RWMutex
	handlers []EventHandler
}

// NewEventEmitter creates a new event emitter / NewEventEmitter 创建新的事件发射器
func NewEventEmitter() *EventEmitter {
	return &EventEmitter{
		handlers: make([]EventHandler, 0),
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
	e.mu.RLock()
	handlers := make([]EventHandler, len(e.handlers))
	copy(handlers, e.handlers)
	e.mu.RUnlock()

	for _, handler := range handlers {
		// Call handlers in goroutine to avoid blocking / 在 goroutine 中调用处理器以避免阻塞
		go handler(event)
	}
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
