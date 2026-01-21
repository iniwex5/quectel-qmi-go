package qmi

import (
	"sync"
)

// ============================================================================
// Buffer Pool / 缓冲池
// 使用 sync.Pool 复用缓冲区以减少 GC 压力
// ============================================================================

// bufferPool provides reusable byte slices for TLV and packet marshaling
// bufferPool 提供可复用的字节切片，用于 TLV 和数据包序列化
var bufferPool = sync.Pool{
	New: func() interface{} {
		// Allocate 256 bytes by default, sufficient for most TLVs
		// 默认分配 256 字节，足以满足大多数 TLV
		buf := make([]byte, 256)
		return &buf
	},
}

// smallBufferPool for small fixed-size buffers (4-8 bytes)
// smallBufferPool 用于小型固定大小缓冲区 (4-8 字节)
var smallBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 8)
		return &buf
	},
}

// GetBuffer retrieves a buffer from the pool / GetBuffer 从池中获取缓冲区
func GetBuffer() *[]byte {
	return bufferPool.Get().(*[]byte)
}

// PutBuffer returns a buffer to the pool / PutBuffer 将缓冲区返回池中
func PutBuffer(b *[]byte) {
	if b == nil || cap(*b) < 256 {
		return
	}
	*b = (*b)[:0] // Reset length / 重置长度
	bufferPool.Put(b)
}

// GetSmallBuffer retrieves a small buffer (8 bytes) / GetSmallBuffer 获取小缓冲区 (8字节)
func GetSmallBuffer() *[]byte {
	return smallBufferPool.Get().(*[]byte)
}

// PutSmallBuffer returns a small buffer to the pool / PutSmallBuffer 将小缓冲区返回池中
func PutSmallBuffer(b *[]byte) {
	if b == nil || cap(*b) < 8 {
		return
	}
	*b = (*b)[:0]
	smallBufferPool.Put(b)
}

// packetBufferPool for full packet buffers (larger, ~4KB)
// packetBufferPool 用于完整数据包缓冲区 (较大，约 4KB)
var packetBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 4096)
		return &buf
	},
}

// GetPacketBuffer retrieves a packet buffer / GetPacketBuffer 获取数据包缓冲区
func GetPacketBuffer() *[]byte {
	return packetBufferPool.Get().(*[]byte)
}

// PutPacketBuffer returns a packet buffer / PutPacketBuffer 返回数据包缓冲区
func PutPacketBuffer(b *[]byte) {
	if b == nil || cap(*b) < 4096 {
		return
	}
	*b = (*b)[:0]
	packetBufferPool.Put(b)
}
