package device

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

// ModemDevice 代表发现的调制解调器设备
type ModemDevice struct {
	// 设备路径
	ControlPath  string // 例如: /dev/cdc-wdm0
	NetInterface string // 例如: wwan0

	// USB 标识
	USBPath   string // 例如: /sys/bus/usb/devices/1-1.2
	VendorID  uint16
	ProductID uint16

	// 驱动信息
	DriverName string // 例如: qmi_wwan, GobiNet

	// 辅助端口
	ATPorts      []string // 例如: /dev/ttyUSB2, /dev/ttyUSB3
	ATPort       string   // 探测到的主 AT 命令端口
	ATPortBackup string   // 探测到的备用 AT 命令端口
}

// Discover 查找所有连接到系统的 Quectel 调制解调器
func Discover() ([]ModemDevice, error) {
	var devices []ModemDevice

	usbDevices, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		return nil, fmt.Errorf("读取 USB 设备失败: %w", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, entry := range usbDevices {
		if strings.HasPrefix(entry.Name(), "usb") {
			continue
		}

		wg.Add(1)
		go func(e os.DirEntry) {
			defer wg.Done()

			path := filepath.Join("/sys/bus/usb/devices", e.Name())

			// 包装 discoverFromSysFS，增加 5秒 超时保护
			// 这里的超时主要防止 discoverFromSysFS 内部可能有较慢的文件操作
			// 虽然 sysfs 通常很快，但为了绝对的防卡死，加上双重保险（内部的 probeATPort 已经有自己的超时）
			type result struct {
				val *ModemDevice
				err error
			}
			done := make(chan result, 1)

			go func() {
				md, err := discoverFromSysFS(path)
				done <- result{md, err}
			}()

			select {
			case res := <-done:
				if res.err == nil && res.val != nil {
					mu.Lock()
					devices = append(devices, *res.val)
					mu.Unlock()
				}
			case <-time.After(5 * time.Second): // 单个设备最大扫描时间 5s
				// 超时忽略
				// fmt.Printf("扫描设备 %s 超时\n", path)
			}
		}(entry)
	}

	wg.Wait()

	if len(devices) == 0 {
		return nil, fmt.Errorf("未发现调制解调器")
	}

	return devices, nil
}

// discoverFromSysFS 检查单个 USB 设备路径
func discoverFromSysFS(usbPath string) (*ModemDevice, error) {
	// 1. 检查厂商 ID
	vid := readHexFile(filepath.Join(usbPath, "idVendor"))
	pid := readHexFile(filepath.Join(usbPath, "idProduct"))
	// fmt.Printf("Device %s: VID=%04x PID=%04x\n", usbPath, vid, pid)

	if vid != 0x2c7c && vid != 0x05c6 { // Quectel & Qualcomm
		return nil, fmt.Errorf("不是 Quectel 设备")
	}

	// device.c 逻辑: 查找网络接口
	// 它扫描interfaces 0 到 bNumInterfaces+8
	bNumIfaces := readIntFile(filepath.Join(usbPath, "bNumInterfaces"))
	// fmt.Printf("Num interfaces: %d\n", bNumIfaces)

	var netInterface string
	var foundIfaceIndex int

	// 扫描网络接口
	for i := 0; i < bNumIfaces+8; i++ {
		// 尝试路径: usbPath/usbName:1.i/net
		// 上面循环中的 entry.Name() 是 usbName (例如: 1-1)
		// 接口路径: 1-1:1.i
		usbName := filepath.Base(usbPath)
		ifPath := filepath.Join(usbPath, fmt.Sprintf("%s:1.%d", usbName, i))

		netDir := filepath.Join(ifPath, "net")
		entries, err := os.ReadDir(netDir)
		if err == nil && len(entries) > 0 {
			netInterface = entries[0].Name()
			foundIfaceIndex = i
			break
		}
	}

	if netInterface == "" {
		return nil, fmt.Errorf("未找到网络接口")
	}

	md := &ModemDevice{
		USBPath:      usbPath,
		VendorID:     vid,
		ProductID:    pid,
		NetInterface: netInterface,
	}

	// device.c 根据接口类/子类确定驱动类型
	// qmidevice_detect 循环查询 usb_interface_info
	ifPath := filepath.Join(usbPath, fmt.Sprintf("%s:1.%d", filepath.Base(usbPath), foundIfaceIndex))
	md.DriverName = determineDriver(ifPath)

	// 确定控制路径 (cdc-wdm)
	// device.c: detect_path_cdc_wdm_or_qcqmi
	md.ControlPath = findCDCWDM(ifPath)
	if md.ControlPath == "" {
		// 回退到更广泛的搜索
		md.ControlPath = findCDCWDMInUSB(usbPath)
	}
	// device.c 针对 ECM/RNDIS/NCM 的逻辑 (但也适用于 QMI 的 AT 命令)
	atIntf := -1
	if vid == 0x2c7c {
		switch pid {
		case 0x0901, 0x0902, 0x8101: // EC200U, EC200D, RG801H
			atIntf = 2
		case 0x0900: // RG500U
			atIntf = 4
		case 0x6026, 0x6005, 0x6002, 0x6001: // EC200T, EC200A, EC200S, EC100Y
			atIntf = 3
		case 0x6007: // EG915Q/EG800Q
			// if RDNIS_MODEL == 1 { atIntf = 5 } else { atIntf = 3 }
			atIntf = 3 // 暂时假设默认值
		default:
			// 对于 EC20 (pid 0x0125) 和其他型号，典型默认值为 2
			atIntf = 2
		}
	} else if vid == 0x05c6 {
		// 高通默认值
		atIntf = 2
	}

	// 收集所有可用的 ttyUSB 端口以防万一
	md.ATPorts = findATPorts(usbPath)

	// 新逻辑: 使用 ATI 探测所有发现的端口
	var validATPorts []string
	for _, port := range md.ATPorts {
		// 使用带超时的安全探测，防止串口打开卡死
		done := make(chan bool, 1)
		go func(p string) {
			done <- probeATPort(p)
		}(port)

		var success bool
		select {
		case success = <-done:
		case <-time.After(2 * time.Second): // 单个端口最大探测时间 2s
			fmt.Printf("设备 %s: 探测端口 %s 超时\n", usbPath, port)
			success = false
		}

		if success {
			validATPorts = append(validATPorts, port)
			fmt.Printf("设备 %s: 通过探测发现有效 AT 端口: %s\n", usbPath, port)
		}
	}

	if len(validATPorts) > 0 {
		// 策略: 选择第一个作为主端口，第二个作为备用端口
		md.ATPort = validATPorts[0]
		if len(validATPorts) > 1 {
			md.ATPortBackup = validATPorts[1]
		}
	} else {
		// 如果探测失败(或未找到端口)，回退到启发式规则
		if atIntf != -1 {
			// 在该特定接口中查找 tty
			atIfPath := filepath.Join(usbPath, fmt.Sprintf("%s:1.%d", filepath.Base(usbPath), atIntf))
			primary, err := findTTYInInterface(atIfPath)
			if err == nil && primary != "" {
				md.ATPort = primary
			}
		}
	}

	if md.ControlPath == "" {
		// 如果未找到 QMI cdc-wdm，可能是在 ECM 模式下，此时 AT 端口也是控制通道？
		// 但我们的项目是 qmi-cm，所以我们主要严格要求 QMI (cdc-wdm)。
		// 但是，返回我们发现的内容更好。
		// 警告: 如果没有控制路径，功能将受到限制。
	}

	return md, nil
}

// probeATPort 尝试打开端口并发送 ATI，查看是否像 modem 一样响应
func probeATPort(port string) bool {
	mode := &serial.Mode{
		BaudRate: 115200,
	}
	p, err := serial.Open(port, mode)
	if err != nil {
		return false
	}
	defer p.Close()

	// 设置一个短超时
	p.SetReadTimeout(300 * time.Millisecond)

	// 清空缓冲区
	// 简单的读取排空
	buf := make([]byte, 1024)
	p.Read(buf)

	// 发送 ATI
	_, err = p.Write([]byte("ATI\r\n"))
	if err != nil {
		return false
	}

	// 读取响应
	// 我们期望 "Quectel", "Revision:", "OK" 等
	// 允许读取多次以收集响应
	response := ""
	for i := 0; i < 5; i++ {
		n, err := p.Read(buf)
		if err != nil {
			break
		}
		if n > 0 {
			response += string(buf[:n])
			if strings.Contains(response, "OK") || strings.Contains(response, "ERROR") {
				break
			}
		} else {
			// 没有数据，稍作等待
			break
		}
	}

	// 检查关键字
	// ATI 通常返回:
	// Quectel
	// EC20F
	// Revision: ...
	// OK

	// 仅检查 "Quectel" 或 "Revision" 就足够了。
	// 同时检查 "OK" 以确保它是命令处理器。
	if strings.Contains(response, "Quectel") || strings.Contains(response, "Revision") || strings.Contains(response, "Model") {
		return true
	}

	//后备: 如果它只是说 OK，可能是它，但有风险。
	// 目前坚持使用特定关键字。

	return false
}

func findCDCWDM(devicePath string) string {
	// 查找 usbmisc 或 usb 子目录
	for _, subDir := range []string{"usbmisc", "usb"} {
		miscPath := filepath.Join(devicePath, subDir)
		entries, err := os.ReadDir(miscPath)
		if err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "cdc-wdm") {
					return filepath.Join("/dev", e.Name())
				}
			}
		}
	}
	return ""
}

func findCDCWDMInUSB(usbPath string) string {
	var result string

	filepath.Walk(usbPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if info.Name() == "usbmisc" || info.Name() == "usb" {
			entries, err := os.ReadDir(path)
			if err == nil {
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "cdc-wdm") {
						result = filepath.Join("/dev", e.Name())
						return filepath.SkipAll
					}
				}
			}
		}
		return nil
	})

	return result
}

// findATPorts 查找所有与该 USB 设备关联的 ttyUSB 端口
func findATPorts(usbPath string) []string {
	var ports []string

	// usbPath 类似于 /sys/devices/.../usb1/1-1/1-1.2
	// 我们想查找 /sys/devices/.../usb1/1-1/1-1.2/1-1.2:1.*/ttyUSB*

	pattern := filepath.Join(usbPath, "*", "ttyUSB*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}

	for _, match := range matches {
		ttyName := filepath.Base(match)
		ports = append(ports, filepath.Join("/dev", ttyName))
	}

	return ports
}

func determineDriver(devicePath string) string {
	driverLink := filepath.Join(devicePath, "driver")
	target, err := os.Readlink(driverLink)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func readHexFile(path string) uint16 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, err := strconv.ParseUint(strings.TrimSpace(string(data)), 16, 16)
	if err != nil {
		return 0
	}
	return uint16(val)
}

func readIntFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return val
}

// findTTYInInterface 在接口路径中查找 tty 目录
func findTTYInInterface(ifPath string) (string, error) {
	// device.c: dir_get_child(pl->path, devname, sizeof(devname), "tty");
	// path/tty/ttyUSBx

	ttyDir := filepath.Join(ifPath, "tty")
	entries, err := os.ReadDir(ttyDir)
	if err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "tty") {
				return filepath.Join("/dev", e.Name()), nil
			}
		}
	}

	// 如果直接读取失败，尝试标准的 Glob
	matches, _ := filepath.Glob(filepath.Join(ifPath, "ttyUSB*"))
	if len(matches) > 0 {
		return filepath.Join("/dev", filepath.Base(matches[0])), nil
	}

	matches2, _ := filepath.Glob(filepath.Join(ifPath, "tty", "ttyUSB*"))
	if len(matches2) > 0 {
		return filepath.Join("/dev", filepath.Base(matches2[0])), nil
	}

	return "", fmt.Errorf("未找到 tty")
}

// String 返回可读的描述
func (m ModemDevice) String() string {
	return fmt.Sprintf("%s (%s) [%04x:%04x] driver=%s AT=%s Backup=%s",
		m.ControlPath, m.NetInterface, m.VendorID, m.ProductID, m.DriverName, m.ATPort, m.ATPortBackup)
}
