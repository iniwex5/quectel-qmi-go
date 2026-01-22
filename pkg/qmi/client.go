package qmi

import (
	"context"
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
	EventUnknown                    EventType = iota
	EventPacketServiceStatusChanged           // WDS connection status changed / WDS连接状态改变
	EventServingSystemChanged                 // NAS registration state changed / NAS注册状态改变
	EventModemReset                           // CTL revoke client ID (modem reset) / CTL撤销客户端ID (modem重置)
	EventNewMessage                           // WMS new message / WMS新消息
	EventUSSD                                 // Voice USSD indication / Voice USSD指示
)

// Event represents an asynchronous indication from the modem / Event代表来自modem的异步指示
type Event struct {
	Type      EventType
	ServiceID uint8
	MessageID uint16
	Packet    *Packet
}

// ============================================================================
// Client - QMI communication client / 客户端 - QMI通信客户端
// ============================================================================

type Client struct {
	file *os.File
	path string

	// Transaction management / 事务管理
	mu           sync.Mutex
	transactions map[uint32]chan *Packet
	lastTxID     uint32 // atomic counter / 原子计数器
	ctlTxID      uint32 // separate counter for CTL (1 byte) / CTL的独立计数器 (1字节)

	// Client ID cache / 客户端ID缓存
	clientIDs map[uint8]uint8 // service -> clientID / 服务 -> 客户端ID

	// Event handling / 事件处理
	eventCh chan Event
	closeCh chan struct{}
	wg      sync.WaitGroup
}

// NewClient creates a new QMI client connected to the given device path / NewClient创建一个连接到指定设备路径的新QMI客户端
func NewClient(path string) (*Client, error) {
	// Open like C version: O_RDWR | O_NONBLOCK | O_NOCTTY / 像C版本一样打开: O_RDWR | O_NONBLOCK | O_NOCTTY
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open QMI device %s: %w", path, err)
	}

	c := &Client{
		file:         f,
		path:         path,
		transactions: make(map[uint32]chan *Packet),
		clientIDs:    make(map[uint8]uint8),
		eventCh:      make(chan Event, 32),
		closeCh:      make(chan struct{}),
	}

	// Start read loop / 启动读取循环
	c.wg.Add(1)
	go c.readLoop()

	// Initial sync (non-fatal, helps clear modem state) / 初始同步 (非致命，有助于清除modem状态)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Sync(ctx); err != nil {
		log.Printf("QMI: initial sync failed (non-fatal): %v", err)
	}

	return c, nil
}

// Close shuts down the client / Close关闭客户端
func (c *Client) Close() error {
	close(c.closeCh)
	c.wg.Wait()
	return c.file.Close()
}

// Events returns a channel for receiving asynchronous indications / Events返回用于接收异步指示的通道
func (c *Client) Events() <-chan Event {
	return c.eventCh
}

// ============================================================================
// Read Loop - handles responses and indications / 读取循环 - 处理响应和指示
// ============================================================================

func (c *Client) readLoop() {
	defer c.wg.Done()
	buf := make([]byte, 4096)

	for {
		select {
		case <-c.closeCh:
			return
		default:
		}

		// Set read deadline to allow periodic checking of closeCh / 设置读取截止时间以允许定期检查closeCh
		c.file.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		n, err := c.file.Read(buf)
		if err != nil {
			if os.IsTimeout(err) {
				continue
			}
			return
		}

		if n < QmuxHeaderSize {
			continue
		}

		packet, err := UnmarshalPacket(buf[:n])
		if err != nil {
			log.Printf("QMI: failed to parse packet (%d bytes): %v", n, err)
			continue
		}

		// Check if this is a response to a pending request / 检查这是否是对挂起请求的响应
		c.mu.Lock()
		key := uint32(packet.ServiceType)<<16 | uint32(packet.TransactionID)
		ch, ok := c.transactions[key]
		c.mu.Unlock()

		if ok && !packet.IsIndication {
			select {
			case ch <- packet:
			default:
			}
		} else {
			if !packet.IsIndication && packet.ServiceType != ServiceControl {
				log.Printf("QMI: received response with unmatched key 0x%08x (MsgID 0x%04x, Service 0x%02x, TxID %d)",
					key, packet.MessageID, packet.ServiceType, packet.TransactionID)
			}
			c.dispatchIndication(packet)
		}
	}
}

// dispatchIndication sends an indication to the event channel / dispatchIndication将指示发送到事件通道
func (c *Client) dispatchIndication(p *Packet) {
	var eventType EventType

	switch {
	case p.ServiceType == ServiceControl && p.MessageID == CTLRevokeClientIDInd:
		eventType = EventModemReset
	case (p.ServiceType == ServiceWDS || p.ServiceType == ServiceWDSIPv6) && p.MessageID == WDSGetPktSrvcStatusInd:
		eventType = EventPacketServiceStatusChanged
	case p.ServiceType == ServiceNAS && (p.MessageID == NASServingSystemInd || p.MessageID == NASSysInfoInd):
		eventType = EventServingSystemChanged
	case p.ServiceType == ServiceWMS && p.MessageID == WMSEventReportInd:
		eventType = EventNewMessage
	default:
		eventType = EventUnknown
	}

	event := Event{
		Type:      eventType,
		ServiceID: p.ServiceType,
		MessageID: p.MessageID,
		Packet:    p,
	}

	select {
	case c.eventCh <- event:
	default:
		// Channel full - drop event / 通道已满 -以此丢弃事件
	}
}

// ============================================================================
// Request/Response handling / 请求/响应处理
// ============================================================================

// SendRequest sends a QMI request and waits for response / SendRequest发送QMI请求并等待响应
func (c *Client) SendRequest(ctx context.Context, service uint8, clientID uint8, msgID uint16, tlvs []TLV) (*Packet, error) {
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

	// Build and send packet / 构建并发送数据包
	p := Packet{
		ServiceType:   service,
		ClientID:      clientID,
		TransactionID: txID,
		MessageID:     msgID,
		TLVs:          tlvs,
	}

	data := p.Marshal()

	// log.Printf("QMI: TX service=0x%02x msg=0x%04x txID=%d len=%d", service, msgID, txID, len(data))
	_, err := c.file.Write(data)
	if err != nil {
		return nil, fmt.Errorf("write failed: %w", err)
	}

	// Wait for response / 等待响应
	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response to msg 0x%04x", msgID)
	}
}

// Sync sends a QMI CTL sync request / Sync发送QMI CTL同步请求
func (c *Client) Sync(ctx context.Context) error {
	_, err := c.SendRequest(ctx, ServiceControl, 0, 0x0027, nil) // 0x0027 = QMICTL_SYNC_REQ
	return err
}

// AllocateClientID requests a client ID for the given service / AllocateClientID为给定服务请求客户端ID
func (c *Client) AllocateClientID(service uint8) (uint8, error) {
	var lastErr error
	for retry := 0; retry < 3; retry++ {

		// Build request: TLV 0x01 = service type / 构建请求: TLV 0x01 = 服务类型
		tlvs := []TLV{NewTLVUint8(0x01, service)}

		resp, err := c.SendRequest(context.Background(), ServiceControl, 0, CTLGetClientID, tlvs)
		if err == nil {
			if !resp.IsSuccess() {
				_, qmiErr, _ := resp.GetResultCode()
				return 0, fmt.Errorf("allocate client ID failed: QMI error 0x%04x", qmiErr)
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
		time.Sleep(500 * time.Millisecond)
	}

	return 0, fmt.Errorf("allocate client ID request failed after retries: %w", lastErr)
}

// ReleaseClientID releases a client ID for the given service / ReleaseClientID释放给定服务的客户端ID
func (c *Client) ReleaseClientID(service uint8, clientID uint8) error {
	// Build request: TLV 0x01 = {service, clientID} / 构建请求: TLV 0x01 = {服务, clientID}
	tlvs := []TLV{{Type: 0x01, Value: []byte{service, clientID}}}

	resp, err := c.SendRequest(context.Background(), ServiceControl, 0, CTLReleaseClientID, tlvs)
	if err != nil {
		return fmt.Errorf("release client ID request failed: %w", err)
	}

	if !resp.IsSuccess() {
		_, qmiErr, _ := resp.GetResultCode()
		return fmt.Errorf("release client ID failed: QMI error 0x%04x", qmiErr)
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
