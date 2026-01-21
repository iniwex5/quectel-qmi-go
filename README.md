# Quectel-CM-Go

**纯 Go 语言实现的移远 4G 模组连接管理器**

A pure Go implementation of Quectel Connection Manager for Linux.

---

## 功能特性

- 🚀 **原生 Go 实现** - 无需依赖外部 C 二进制
- ⚡ **毫秒级断网感知** - 异步 Indication 监听机制
- 🔄 **自动重连** - 指数退避 (5s→10s→20s→40s→60s)
- 📡 **多模组支持** - ModemPool 负载均衡 + 热插拔
- 🌐 **IPv4/IPv6 双栈** - 独立 WDS 客户端
- 🔧 **零依赖配置** - 使用 netlink 直接操作内核
- 📊 **健康监控** - 定期检查信号和连接状态
- 🎯 **状态回调** - OnConnect/OnDisconnect 事件

---

## 快速开始

### 编译

```bash
cd /root/ec20/quectel-CM/quectel-go
go build -o quectel-cm-go ./cmd/cm
```

### 运行

```bash
# 基础拨号
sudo ./quectel-cm-go -s 你的APN

# 指定接口
sudo ./quectel-cm-go -i wwan0 -s internet

# 带认证
sudo ./quectel-cm-go -s myapn -u 用户名 -p 密码 -a 1

# 仅 IPv4
sudo ./quectel-cm-go -s internet -4
```

### 参数说明

| 参数 | 说明 |
|------|------|
| `-s` | APN 名称 |
| `-u` | 用户名 |
| `-p` | 密码 |
| `-a` | 认证类型: 0=无, 1=PAP, 2=CHAP |
| `-i` | 网络接口名 (如 wwan0) |
| `-d` | 控制设备路径 (如 /dev/cdc-wdm0) |
| `-4` | 仅 IPv4 |
| `-6` | 仅 IPv6 |
| `-pin` | SIM PIN 码 |
| `-v` | 详细日志 |

---

## 作为库使用

### 单模组

```go
import (
    "quectel-go/pkg/device"
    "quectel-go/pkg/manager"
    "quectel-go/pkg/qmi"
)

func main() {
    modems, _ := device.Discover()

    cfg := manager.Config{
        Device:        modems[0],
        APN:           "internet",
        EnableIPv4:    true,
        AutoReconnect: true,
    }

    mgr := manager.New(cfg, nil)
    
    // 状态回调
    mgr.OnConnect(func(s *qmi.RuntimeSettings) {
        fmt.Printf("已连接! IP: %s\n", s.IPv4Address)
    })
    mgr.OnDisconnect(func() {
        fmt.Println("连接断开!")
    })

    mgr.Start()
    defer mgr.Stop()

    select {}
}
```

### 多模组 (负载均衡)

```go
pool := manager.NewPool()

// 自动发现并添加
pool.DiscoverAndAdd(baseCfg, logger)

// 设置选择策略
pool.SetSelector(&manager.SignalStrengthSelector{})

// 启动所有
pool.StartAll()

// 健康监控
pool.StartHealthMonitor(30 * time.Second)

// 热插拔检测
pool.WatchHotPlug(baseCfg, logger, 5*time.Second)

// 获取模组
mgr := pool.Get()          // 按策略选择
mgr := pool.GetHealthy()   // 信号最强

// 检查健康状态
for name, status := range pool.Health() {
    fmt.Printf("%s: RSSI=%d connected=%v\n", 
        name, status.SignalRSSI, status.Connected)
}
```

### 选择策略

| 策略 | 说明 |
|------|------|
| `RoundRobinSelector` | 轮询，均衡使用 |
| `RandomSelector` | 随机选择 |
| `SignalStrengthSelector` | 优先信号最强 |
| `LeastUsedSelector` | 最少使用优先 |

---

## 项目结构

```
quectel-go/
├── cmd/cm/main.go          # 命令行工具入口
├── pkg/
│   ├── qmi/                # QMI 协议栈
│   │   ├── frame.go        # 帧编解码
│   │   ├── client.go       # 异步客户端
│   │   ├── wds.go          # 数据服务 (拨号/IP)
│   │   ├── nas.go          # 网络服务 (注册/信号)
│   │   ├── dms.go          # 设备服务 (SIM/Radio)
│   │   ├── uim.go          # SIM 卡服务
│   │   ├── wda.go          # 数据管理服务
│   │   ├── errors.go       # 自定义错误类型
│   │   └── pool.go         # 缓冲池优化
│   ├── device/             # 设备发现
│   ├── netcfg/             # 网络配置 (netlink)
│   └── manager/            # 连接管理器
│       ├── manager.go      # 核心管理器
│       ├── pool.go         # 多模组池
│       ├── callbacks.go    # 状态回调
│       └── logger.go       # 日志接口
└── go.mod
```

---

## API 参考

### manager.Config

| 字段 | 类型 | 说明 |
|------|------|------|
| Device | ModemDevice | 设备信息 |
| APN | string | 接入点名称 |
| Username | string | PPP 用户名 |
| Password | string | PPP 密码 |
| AuthType | uint8 | 0=无, 1=PAP, 2=CHAP |
| EnableIPv4 | bool | 启用 IPv4 |
| EnableIPv6 | bool | 启用 IPv6 |
| AutoReconnect | bool | 自动重连 |

### manager.ModemPool

| 方法 | 说明 |
|------|------|
| `Add(name, cfg, logger)` | 添加模组 |
| `Remove(name)` | 移除模组 |
| `Get()` | 获取模组 (按策略) |
| `GetHealthy()` | 获取健康模组 |
| `StartAll()` / `StopAll()` | 批量操作 |
| `SetSelector(s)` | 设置选择策略 |
| `StartHealthMonitor(d)` | 启动健康监控 |
| `WatchHotPlug(cfg, log, d)` | 监控热插拔 |
| `Health()` | 获取健康状态 |

---

## 与 C 版本对比

| 特性 | C 版本 | Go 版本 |
|------|--------|---------|
| 代码行数 | ~15000 | ~3000 |
| 依赖 | libc, udhcpc | 纯 Go |
| 可嵌入性 | 需 exec | 直接 import |
| 健康监控 | ❌ | ✅ |
| 热插拔 | ❌ | ✅ |
| IP 轮换 | 慢 (AT指令) | ⚡ 极致 (Indication驱动) |

---

## 高级功能：IP 轮换

本项目实现了经过深度优化的 IP 轮换机制，支持**秒级**切换 IP。

### 核心优化技术
1.  **Indication 驱动**：不再轮询，模组搜网成功后毫秒级响应。
2.  **智能 IP 检测**：拨号后立即比对 IP，若运营商分配了相同 IP，自动触发射频重置。
3.  **零延迟策略**：移除了所有非必要的 `sleep`，压榨硬件性能极限。

### 代码示例

```go
// 触发 IP 轮换
go func() {
    for {
        time.Sleep(1 * time.Minute)
        
        fmt.Println("正在更换 IP...")
        if err := mgr.RotateIP(); err != nil {
            fmt.Printf("换 IP 失败: %v\n", err)
        } else {
            fmt.Println("换 IP 成功!")
        }
    }
}()
```


---

## 兼容性

- **支持驱动**: qmi_wwan, GobiNet
- **支持模组**: EC20, EC25, EG25, EM05, RM500Q 等 Quectel 4G/5G 模组
- **系统要求**:
    - **Linux**: Kernel 3.10+ (需 `qmi_wwan` / `GobiNet` 驱动)
    - **Windows**: Windows 10/11 (需安装 Quectel USB 驱动)
    - **macOS**: macOS 12+ (Apple Silicon / Intel)
    - **OpenWrt**: 19.07+ (预装 `kmod-usb-net-qmi-wwan`)

---

## 跨平台支持 (Cross-Platform)

### Windows
```powershell
# 编译
$Env:GOOS = "windows"; $Env:GOARCH = "amd64"; go build -o quectel-cm.exe ./cmd/cm
# 运行 (需管理员权限)
.\quectel-cm.exe -s internet
```

### macOS
```bash
# 编译
GOOS=darwin GOARCH=arm64 go build -o quectel-cm-mac ./cmd/cm
# 运行 (需 sudo)
sudo ./quectel-cm-mac -s internet
```

### OpenWrt / 路由器
```bash
# 交叉编译 (MIPSLE 示例)
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -ldflags="-s -w" -o quectel-cm-mipsle ./cmd/cm
# 上传并运行
scp quectel-cm-mipsle root@192.168.1.1:/tmp/
ssh root@192.168.1.1 "/tmp/quectel-cm-mipsle -s internet"
```

---

## 许可证

MIT License
