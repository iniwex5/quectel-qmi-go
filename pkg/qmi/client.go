package qmi

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ============================================================================
// Event types for indication handling / 指示处理的事件类型
// ============================================================================

type EventType int

const (
	EventUnknown                               EventType = iota
	EventPacketServiceStatusChanged                      // WDS connection status changed / WDS连接状态改变
	EventServingSystemChanged                            // NAS registration state changed / NAS注册状态改变
	EventNASOperatorNameChanged                          // NAS operator name changed / NAS 运营商名称变化
	EventNASNetworkTimeChanged                           // NAS network time changed / NAS 网络时间变化
	EventNASSignalInfoChanged                            // NAS signal info changed / NAS 信号信息变化
	EventNASNetworkReject                                // NAS network reject / NAS 驻网拒绝
	EventNASIncrementalNetworkScan                       // NAS incremental network scan / NAS 增量搜网
	EventModemReset                                      // CTL revoke client ID (modem reset) / CTL撤销客户端ID (modem重置)
	EventNewMessage                                      // WMS new message / WMS新消息
	EventWMSSMSCAddress                                  // WMS SMSC address indication / WMS 短信中心地址指示
	EventWMSTransportNetworkRegistrationStatus           // WMS transport network registration status indication / WMS 传输网络注册状态指示
	EventIMSRegistrationStatus                           // IMSA registration status changed / IMSA 注册状态变化
	EventIMSServicesStatus                               // IMSA services status changed / IMSA 业务状态变化
	EventIMSSettingsChanged                              // IMS settings changed / IMS 配置状态变化
	EventVoiceCallStatus                                 // Voice all call status indication / VOICE 通话状态指示
	EventVoiceSupplementaryService                       // Voice supplementary service indication / VOICE 补充业务指示
	EventUSSD                                            // Voice USSD indication / Voice USSD指示
	EventVoiceUSSDReleased                               // Voice USSD released indication / VOICE USSD 释放指示
	EventVoiceUSSDNoWaitResult                           // Voice originate USSD no wait indication / VOICE 异步 USSD 指示
	EventSimStatusChanged                                // UIM SIM status changed / UIM SIM状态改变
	EventUIMSessionClosed                                // UIM session closed indication / UIM 会话关闭指示
	EventUIMRefresh                                      // UIM refresh indication / UIM 刷新指示
	EventUIMSlotStatus                                   // UIM slot status indication / UIM 卡槽状态指示
)

// Event represents an asynchronous indication from the modem / Event代表来自modem的异步指示
type Event struct {
	Type      EventType
	ServiceID uint8
	MessageID uint16
	Packet    *Packet
}

// ClientOptions controls runtime behavior for the low-level QMI client.
type ClientOptions struct {
	SyncOnOpen            bool
	ReadDeadline          time.Duration
	DefaultRequestTimeout time.Duration
	TxQueueSize           int
	IndicationQueueSize   int
}

// DefaultClientOptions returns the production defaults used by NewClientWithOptions.
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		SyncOnOpen:            true,
		ReadDeadline:          100 * time.Millisecond,
		DefaultRequestTimeout: 30 * time.Second,
		TxQueueSize:           128,
		IndicationQueueSize:   256,
	}
}

// ClientStats summarizes key runtime behaviors without exposing payloads.
type ClientStats struct {
	UnmatchedResponses     uint64
	ParseErrors            uint64
	CoalescedIndications   uint64
	DroppedEdgeIndications uint64
}

type writeRequest struct {
	data   []byte
	result chan error
}

type coalescedEventStore struct {
	events map[string]Event
	order  []string
}

// ============================================================================
// Client - QMI communication client / 客户端 - QMI通信客户端
// ============================================================================

type Client struct {
	file *os.File
	path string
	opts ClientOptions

	// Transaction management / 事务管理
	mu           sync.Mutex
	transactions map[uint32]chan *Packet
	lastTxID     uint32 // atomic counter / 原子计数器
	ctlTxID      uint32 // separate counter for CTL (1 byte) / CTL的独立计数器 (1字节)

	// Client ID cache / 客户端ID缓存
	clientIDs map[uint8]uint8 // service -> clientID / 服务 -> 客户端ID

	// Event handling / 事件处理
	eventCh           chan Event
	indicationInCh    chan Event
	coalescedSignalCh chan struct{}
	writeCh           chan writeRequest
	closeCh           chan struct{}
	closeOnce         sync.Once
	wg                sync.WaitGroup

	coalescedMu sync.Mutex
	coalesced   coalescedEventStore

	unmatchedResponses     atomic.Uint64
	parseErrors            atomic.Uint64
	coalescedIndications   atomic.Uint64
	droppedEdgeIndications atomic.Uint64
}

func normalizeClientOptions(opts ClientOptions) ClientOptions {
	defaults := DefaultClientOptions()
	if opts.ReadDeadline <= 0 {
		opts.ReadDeadline = defaults.ReadDeadline
	}
	if opts.DefaultRequestTimeout <= 0 {
		opts.DefaultRequestTimeout = defaults.DefaultRequestTimeout
	}
	if opts.TxQueueSize <= 0 {
		opts.TxQueueSize = defaults.TxQueueSize
	}
	if opts.IndicationQueueSize <= 0 {
		opts.IndicationQueueSize = defaults.IndicationQueueSize
	}
	// Preserve backwards-compatible zero-value construction while still allowing explicit false
	// when at least one other option is set.
	if !opts.SyncOnOpen &&
		opts.ReadDeadline == defaults.ReadDeadline &&
		opts.DefaultRequestTimeout == defaults.DefaultRequestTimeout &&
		opts.TxQueueSize == defaults.TxQueueSize &&
		opts.IndicationQueueSize == defaults.IndicationQueueSize {
		opts.SyncOnOpen = defaults.SyncOnOpen
	}
	return opts
}

// NewClientWithOptions creates a new QMI client connected to the given device path.
func NewClientWithOptions(ctx context.Context, path string, opts ClientOptions) (*Client, error) {
	opts = normalizeClientOptions(opts)

	// Open like C version: O_RDWR | O_NONBLOCK | O_NOCTTY / 像C版本一样打开: O_RDWR | O_NONBLOCK | O_NOCTTY
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open QMI device %s: %w", path, err)
	}

	c := &Client{
		file:              f,
		path:              path,
		opts:              opts,
		transactions:      make(map[uint32]chan *Packet),
		clientIDs:         make(map[uint8]uint8),
		eventCh:           make(chan Event, opts.IndicationQueueSize),
		indicationInCh:    make(chan Event, opts.IndicationQueueSize),
		coalescedSignalCh: make(chan struct{}, 1),
		writeCh:           make(chan writeRequest, opts.TxQueueSize),
		closeCh:           make(chan struct{}),
		coalesced: coalescedEventStore{
			events: make(map[string]Event),
		},
	}

	// Start runtime loops / 启动运行时循环
	c.wg.Add(3)
	go c.readLoop()
	go c.writerLoop()
	go c.indicationLoop()

	// Initial sync (non-fatal, helps clear modem state) / 初始同步 (非致命，有助于清除modem状态)
	if opts.SyncOnOpen {
		syncCtx := ctx
		if syncCtx == nil {
			syncCtx = context.Background()
		}
		if _, hasDeadline := syncCtx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			syncCtx, cancel = context.WithTimeout(syncCtx, 5*time.Second)
			defer cancel()
		}
		if err := c.Sync(syncCtx); err != nil {
			log.Printf("QMI: initial sync failed (non-fatal): %v", err)
		}
	}

	return c, nil
}

// Close shuts down the client / Close关闭客户端
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closeCh)
		err = c.file.Close()
		c.wg.Wait()
		c.failPendingTransactions(fmt.Errorf("client closed"))
		close(c.eventCh)
	})
	return err
}

// Events returns a channel for receiving asynchronous indications / Events返回用于接收异步指示的通道
func (c *Client) Events() <-chan Event {
	return c.eventCh
}

// Stats returns a point-in-time snapshot of client runtime metrics.
func (c *Client) Stats() ClientStats {
	if c == nil {
		return ClientStats{}
	}
	return ClientStats{
		UnmatchedResponses:     c.unmatchedResponses.Load(),
		ParseErrors:            c.parseErrors.Load(),
		CoalescedIndications:   c.coalescedIndications.Load(),
		DroppedEdgeIndications: c.droppedEdgeIndications.Load(),
	}
}

// ============================================================================
// Read Loop - handles responses and indications / 读取循环 - 处理响应和指示
// ============================================================================

func (c *Client) readLoop() {
	defer c.wg.Done()
	buf := make([]byte, 16384)
	var pending []byte

	for {
		select {
		case <-c.closeCh:
			return
		default:
		}

		// Set read deadline to allow periodic checking of closeCh / 设置读取截止时间以允许定期检查closeCh
		_ = c.file.SetReadDeadline(time.Now().Add(c.opts.ReadDeadline))

		n, err := c.file.Read(buf)
		if err != nil {
			if os.IsTimeout(err) {
				continue
			}
			select {
			case <-c.closeCh:
				return
			default:
			}
			log.Printf("QMI: read failed: %v", err)
			c.failPendingTransactions(err)
			return
		}

		if n <= 0 {
			continue
		}

		pending = append(pending, buf[:n]...)

		for {
			if len(pending) < 3 {
				break
			}
			if pending[0] != 0x01 {
				i := 0
				for i < len(pending) && pending[i] != 0x01 {
					i++
				}
				if i == len(pending) {
					pending = pending[:0]
					break
				}
				pending = pending[i:]
				continue
			}
			if len(pending) < QmuxHeaderSize {
				break
			}

			frameLen := 1 + int(binary.LittleEndian.Uint16(pending[1:3]))
			if frameLen < QmuxHeaderSize {
				pending = pending[1:]
				continue
			}
			if len(pending) < frameLen {
				break
			}

			frame := make([]byte, frameLen)
			copy(frame, pending[:frameLen])
			pending = pending[frameLen:]

			packet, err := UnmarshalPacket(frame)
			if err != nil {
				c.parseErrors.Add(1)
				log.Printf("QMI: failed to parse packet (%d bytes): %v", frameLen, err)
				continue
			}

			c.mu.Lock()
			key := uint32(packet.ServiceType)<<16 | uint32(packet.TransactionID)
			ch, ok := c.transactions[key]
			c.mu.Unlock()

			if ok && !packet.IsIndication {
				select {
				case ch <- packet:
				default:
				}
				continue
			}

			if !packet.IsIndication && packet.ServiceType != ServiceControl {
				c.unmatchedResponses.Add(1)
				log.Printf("QMI: received response with unmatched key 0x%08x (MsgID 0x%04x, Service 0x%02x, TxID %d)",
					key, packet.MessageID, packet.ServiceType, packet.TransactionID)
			}
			c.dispatchIndication(packet)
		}
	}
}

func (c *Client) writerLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.closeCh:
			return
		case req := <-c.writeCh:
			err := c.writeAll(req.data)
			select {
			case req.result <- err:
			default:
			}
		}
	}
}

func (c *Client) indicationLoop() {
	defer c.wg.Done()

	for {
		if evt, ok := c.popCoalescedEvent(); ok {
			if !c.deliverEvent(evt) {
				return
			}
			continue
		}

		select {
		case <-c.closeCh:
			return
		case evt := <-c.indicationInCh:
			if !c.deliverEvent(evt) {
				return
			}
		case <-c.coalescedSignalCh:
		}
	}
}

func (c *Client) deliverEvent(evt Event) bool {
	select {
	case c.eventCh <- evt:
		return true
	case <-c.closeCh:
		return false
	}
}

func (c *Client) writeAll(data []byte) error {
	written := 0
	for written < len(data) {
		n, err := c.file.Write(data[written:])
		if err != nil {
			return fmt.Errorf("write failed: %w", err)
		}
		written += n
	}
	return nil
}

func (c *Client) failPendingTransactions(cause error) {
	c.mu.Lock()
	for key, ch := range c.transactions {
		delete(c.transactions, key)
		close(ch)
	}
	c.mu.Unlock()
}

func (c *Client) enqueueIndication(event Event) {
	if c.indicationInCh == nil {
		select {
		case c.eventCh <- event:
		default:
		}
		return
	}

	if key, ok := c.coalescingKey(event); ok {
		select {
		case c.indicationInCh <- event:
			return
		default:
			c.storeCoalescedEvent(key, event)
			c.coalescedIndications.Add(1)
			select {
			case c.coalescedSignalCh <- struct{}{}:
			default:
			}
			return
		}
	}

	timer := time.NewTimer(c.opts.ReadDeadline)
	defer timer.Stop()

	select {
	case c.indicationInCh <- event:
	case <-c.closeCh:
	case <-timer.C:
		c.droppedEdgeIndications.Add(1)
		log.Printf("QMI: dropping edge indication type=%d service=0x%02x msg=0x%04x because indication queue is full",
			event.Type, event.ServiceID, event.MessageID)
	}
}

func (c *Client) storeCoalescedEvent(key string, event Event) {
	c.coalescedMu.Lock()
	defer c.coalescedMu.Unlock()

	if _, exists := c.coalesced.events[key]; !exists {
		c.coalesced.order = append(c.coalesced.order, key)
	}
	c.coalesced.events[key] = event
}

func (c *Client) popCoalescedEvent() (Event, bool) {
	c.coalescedMu.Lock()
	defer c.coalescedMu.Unlock()

	for len(c.coalesced.order) > 0 {
		key := c.coalesced.order[0]
		c.coalesced.order = c.coalesced.order[1:]
		event, ok := c.coalesced.events[key]
		delete(c.coalesced.events, key)
		if ok {
			return event, true
		}
	}
	return Event{}, false
}

func (c *Client) coalescingKey(event Event) (string, bool) {
	switch event.Type {
	case EventPacketServiceStatusChanged:
		return fmt.Sprintf("packet-status:%d:%d", event.ServiceID, event.MessageID), true
	case EventServingSystemChanged:
		return fmt.Sprintf("serving-system:%d:%d", event.ServiceID, event.MessageID), true
	case EventNASOperatorNameChanged:
		return fmt.Sprintf("nas-operator-name:%d:%d", event.ServiceID, event.MessageID), true
	case EventNASNetworkTimeChanged:
		return fmt.Sprintf("nas-network-time:%d:%d", event.ServiceID, event.MessageID), true
	case EventNASSignalInfoChanged:
		return fmt.Sprintf("nas-signal-info:%d:%d", event.ServiceID, event.MessageID), true
	case EventNASNetworkReject:
		return fmt.Sprintf("nas-network-reject:%d:%d", event.ServiceID, event.MessageID), true
	case EventNASIncrementalNetworkScan:
		return fmt.Sprintf("nas-incremental-scan:%d:%d", event.ServiceID, event.MessageID), true
	case EventWMSTransportNetworkRegistrationStatus:
		return fmt.Sprintf("wms-transport:%d:%d", event.ServiceID, event.MessageID), true
	case EventModemReset:
		return fmt.Sprintf("critical-modem-reset:%d:%d", event.ServiceID, event.MessageID), true
	case EventUIMSessionClosed:
		return fmt.Sprintf("critical-uim-session-closed:%d:%d", event.ServiceID, event.MessageID), true
	default:
		return "", false
	}
}

// dispatchIndication sends an indication to the event channel / dispatchIndication将指示发送到事件通道
func (c *Client) dispatchIndication(p *Packet) {
	var eventType EventType

	switch {
	case p.ServiceType == ServiceControl && p.MessageID == CTLRevokeClientIDInd:
		c.handleClientIDRevoke(p)
		eventType = EventModemReset
	case (p.ServiceType == ServiceWDS || p.ServiceType == ServiceWDSIPv6) && p.MessageID == WDSGetPktSrvcStatusInd:
		eventType = EventPacketServiceStatusChanged
	case p.ServiceType == ServiceNAS && (p.MessageID == NASServingSystemInd || p.MessageID == NASSysInfoInd || p.MessageID == NASEventReportInd):
		eventType = EventServingSystemChanged
	case p.ServiceType == ServiceNAS && p.MessageID == NASOperatorNameInd:
		eventType = EventNASOperatorNameChanged
	case p.ServiceType == ServiceNAS && p.MessageID == NASNetworkTimeInd:
		eventType = EventNASNetworkTimeChanged
	case p.ServiceType == ServiceNAS && p.MessageID == NASSignalInfoInd:
		eventType = EventNASSignalInfoChanged
	case p.ServiceType == ServiceNAS && p.MessageID == NASNetworkRejectInd:
		eventType = EventNASNetworkReject
	case p.ServiceType == ServiceNAS && p.MessageID == NASIncrementalNetworkScanInd:
		eventType = EventNASIncrementalNetworkScan
	case p.ServiceType == ServiceWMS && p.MessageID == WMSEventReportInd:
		eventType = EventNewMessage
	case p.ServiceType == ServiceWMS && p.MessageID == WMSSMSCAddressInd:
		eventType = EventWMSSMSCAddress
	case p.ServiceType == ServiceWMS && p.MessageID == WMSTransportNetworkRegistrationStatusInd:
		eventType = EventWMSTransportNetworkRegistrationStatus
	case p.ServiceType == ServiceIMSA && p.MessageID == IMSARegistrationStatusChanged:
		eventType = EventIMSRegistrationStatus
	case p.ServiceType == ServiceIMSA && p.MessageID == IMSAServicesStatusChanged:
		eventType = EventIMSServicesStatus
	case p.ServiceType == ServiceIMS && p.MessageID == IMSSettingsChangedInd:
		eventType = EventIMSSettingsChanged
	case p.ServiceType == ServiceVOICE && p.MessageID == VOICEAllCallStatusInd:
		eventType = EventVoiceCallStatus
	case p.ServiceType == ServiceVOICE && p.MessageID == VOICESupplementaryServiceInd:
		eventType = EventVoiceSupplementaryService
	case p.ServiceType == ServiceVOICE && p.MessageID == VOICEUSSDInd:
		eventType = EventUSSD
	case p.ServiceType == ServiceVOICE && p.MessageID == VOICEReleaseUSSDInd:
		eventType = EventVoiceUSSDReleased
	case p.ServiceType == ServiceVOICE && p.MessageID == VOICEOriginateUSSDNoWait:
		eventType = EventVoiceUSSDNoWaitResult
	case p.ServiceType == ServiceUIM && p.MessageID == UIMStatusChangeInd:
		eventType = EventSimStatusChanged
	case p.ServiceType == ServiceUIM && p.MessageID == UIMSessionClosedInd:
		eventType = EventUIMSessionClosed
	case p.ServiceType == ServiceUIM && p.MessageID == UIMRefreshInd:
		eventType = EventUIMRefresh
	case p.ServiceType == ServiceUIM && p.MessageID == UIMSlotStatusInd:
		eventType = EventUIMSlotStatus
	default:
		eventType = EventUnknown
	}

	event := Event{
		Type:      eventType,
		ServiceID: p.ServiceType,
		MessageID: p.MessageID,
		Packet:    p,
	}
	c.enqueueIndication(event)
}

func (c *Client) handleClientIDRevoke(p *Packet) {
	if p.ServiceType != ServiceControl || p.MessageID != CTLRevokeClientIDInd {
		return
	}
	tlv := FindTLV(p.TLVs, 0x01)
	if tlv == nil || len(tlv.Value) < 2 {
		return
	}
	service := tlv.Value[0]
	clientID := tlv.Value[1]

	c.mu.Lock()
	if cached, ok := c.clientIDs[service]; ok && cached == clientID {
		delete(c.clientIDs, service)
	}
	c.mu.Unlock()
}

// ============================================================================
// Request/Response handling / 请求/响应处理
// ============================================================================

// SendRequest sends a QMI request and waits for response / SendRequest发送QMI请求并等待响应
func (c *Client) SendRequest(ctx context.Context, service uint8, clientID uint8, msgID uint16, tlvs []TLV) (*Packet, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok && c.opts.DefaultRequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.DefaultRequestTimeout)
		defer cancel()
	}

	// Allocate transaction ID / 分配事务ID
	var txID uint16
	if service == ServiceControl {
		txID = uint16(atomic.AddUint32(&c.ctlTxID, 1) & 0xFF)
		if txID == 0 {
			txID = uint16(atomic.AddUint32(&c.ctlTxID, 1) & 0xFF)
		}
	} else {
		txID = uint16(atomic.AddUint32(&c.lastTxID, 1) & 0xFFFF)
		if txID == 0 {
			txID = uint16(atomic.AddUint32(&c.lastTxID, 1) & 0xFFFF)
		}
	}

	// Create response channel / 创建响应通道
	respCh := make(chan *Packet, 1)
	key := uint32(service)<<16 | uint32(txID)
	c.mu.Lock()
	c.transactions[key] = respCh
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.transactions, key)
		c.mu.Unlock()
	}()

	// Build packet / 构建数据包
	p := Packet{
		ServiceType:   service,
		ClientID:      clientID,
		TransactionID: txID,
		MessageID:     msgID,
		TLVs:          tlvs,
	}

	writeReq := writeRequest{
		data:   p.Marshal(),
		result: make(chan error, 1),
	}

	select {
	case c.writeCh <- writeReq:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closeCh:
		return nil, fmt.Errorf("connection closed")
	}

	select {
	case err := <-writeReq.result:
		if err != nil {
			return nil, err
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closeCh:
		return nil, fmt.Errorf("connection closed")
	}

	// Wait for response / 等待响应
	select {
	case resp, ok := <-respCh:
		if !ok || resp == nil {
			return nil, fmt.Errorf("connection closed")
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closeCh:
		return nil, fmt.Errorf("connection closed")
	}
}

// Sync sends a QMI CTL sync request / Sync发送QMI CTL同步请求
func (c *Client) Sync(ctx context.Context) error {
	_, err := c.SendRequest(ctx, ServiceControl, 0, 0x0027, nil) // 0x0027 = QMICTL_SYNC_REQ
	return err
}

// AllocateClientID requests a client ID for the given service / AllocateClientID为给定服务请求客户端ID
func (c *Client) AllocateClientID(service uint8) (uint8, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return c.AllocateClientIDWithContext(ctx, service)
}

func (c *Client) AllocateClientIDWithContext(ctx context.Context, service uint8) (uint8, error) {
	var lastErr error
	for retry := 0; retry < 3; retry++ {
		attemptCtx, attemptCancel := context.WithTimeout(ctx, 20*time.Second)

		// Build request: TLV 0x01 = service type / 构建请求: TLV 0x01 = 服务类型
		tlvs := []TLV{NewTLVUint8(0x01, service)}

		resp, err := c.SendRequest(attemptCtx, ServiceControl, 0, CTLGetClientID, tlvs)
		attemptCancel()
		if err == nil {
			if err := resp.CheckResult(); err != nil {
				return 0, err
			}

			// Parse response TLV 0x01: {service, clientID} / 解析响应 TLV 0x01: {服务, clientID}
			tlv := FindTLV(resp.TLVs, 0x01)
			if tlv == nil || len(tlv.Value) < 2 {
				return 0, fmt.Errorf("invalid response TLV")
			}

			clientID := tlv.Value[1]
			c.mu.Lock()
			c.clientIDs[service] = clientID
			c.mu.Unlock()

			return clientID, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	return 0, fmt.Errorf("allocate client ID request failed after retries: %w", lastErr)
}

// ReleaseClientID releases a client ID for the given service / ReleaseClientID释放给定服务的客户端ID
func (c *Client) ReleaseClientID(service uint8, clientID uint8) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.ReleaseClientIDWithContext(ctx, service, clientID)
}

func (c *Client) ReleaseClientIDWithContext(ctx context.Context, service uint8, clientID uint8) error {
	// Build request: TLV 0x01 = {service, clientID} / 构建请求: TLV 0x01 = {服务, clientID}
	tlvs := []TLV{{Type: 0x01, Value: []byte{service, clientID}}}

	resp, err := c.SendRequest(ctx, ServiceControl, 0, CTLReleaseClientID, tlvs)
	if err != nil {
		return fmt.Errorf("release client ID request failed: %w", err)
	}

	if err := resp.CheckResult(); err != nil {
		return err
	}

	c.mu.Lock()
	delete(c.clientIDs, service)
	c.mu.Unlock()

	return nil
}

// GetClientID returns the cached client ID for a service, or 0 if not allocated / GetClientID返回服务的缓存客户端ID，如果未分配则返回0
func (c *Client) GetClientID(service uint8) uint8 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clientIDs[service]
}
