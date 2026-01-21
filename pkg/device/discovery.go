package device

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ModemDevice represents a discovered modem / ModemDevice代表发现的modem
type ModemDevice struct {
	// Device paths / 设备路径
	ControlPath  string // e.g., /dev/cdc-wdm0
	NetInterface string // e.g., wwan0

	// USB identification / USB标识
	USBPath   string // e.g., /sys/bus/usb/devices/1-1.2
	VendorID  uint16
	ProductID uint16

	// Driver info / 驱动信息
	DriverName string // e.g., qmi_wwan, GobiNet

	// Auxiliary ports / 辅助端口
	ATPorts []string // e.g., /dev/ttyUSB2, /dev/ttyUSB3
	ATPort  string   // Best guess for AT command port / AT命令端口的最佳猜测
}

// Discover finds all Quectel modems connected to the system / Discover发现所有连接到系统的Quectel modem
func Discover() ([]ModemDevice, error) {
	var devices []ModemDevice

	// Scan /sys/bus/usb/devices (matches device.c qmidevice_detect logic)
	// 扫描 /sys/bus/usb/devices (匹配 device.c qmidevice_detect 逻辑)
	usbDevices, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		return nil, fmt.Errorf("failed to read usb devices: %w", err)
	}

	for _, entry := range usbDevices {
		// device.c skips entry->d_name[0] == 'u' (usbX root hubs?), we check prefix
		// device.c 跳过 entry->d_name[0] == 'u' (usbX root hubs?), 我们检查前缀
		if strings.HasPrefix(entry.Name(), "usb") {
			continue
		}

		path := filepath.Join("/sys/bus/usb/devices", entry.Name())
		md, err := discoverFromSysFS(path)
		if err == nil && md != nil {
			devices = append(devices, *md)
		}
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("no modems found")
	}

	return devices, nil
}

// discoverFromSysFS inspects a single USB device path / discoverFromSysFS 检查单个USB设备路径
func discoverFromSysFS(usbPath string) (*ModemDevice, error) {
	// 1. Check VendorID / 检查厂商ID
	vid := readHexFile(filepath.Join(usbPath, "idVendor"))
	pid := readHexFile(filepath.Join(usbPath, "idProduct"))
	// fmt.Printf("Device %s: VID=%04x PID=%04x\n", usbPath, vid, pid)

	if vid != 0x2c7c && vid != 0x05c6 { // Quectel & Qualcomm
		return nil, fmt.Errorf("not a Quectel device")
	}

	// device.c logic: find network interface
	// device.c 逻辑: 查找网络接口
	// It scans interfaces 0 to bNumInterfaces+8
	// 它扫描接口 0 到 bNumInterfaces+8
	bNumIfaces := readIntFile(filepath.Join(usbPath, "bNumInterfaces"))
	// fmt.Printf("Num interfaces: %d\n", bNumIfaces)

	var netInterface string
	var foundIfaceIndex int

	// Scan interfaces for network card / 扫描网络接口
	for i := 0; i < bNumIfaces+8; i++ {
		// Try path: usbPath/usbName:1.i/net
		// entry.Name() in loop above is the usbName (e.g. 1-1)
		// Interface path: 1-1:1.i
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
		return nil, fmt.Errorf("no network interface found")
	}

	md := &ModemDevice{
		USBPath:      usbPath,
		VendorID:     vid,
		ProductID:    pid,
		NetInterface: netInterface,
	}

	// device.c determines driver type from interface class/subclass
	// qmidevice_detect loop queries usb_interface_info
	ifPath := filepath.Join(usbPath, fmt.Sprintf("%s:1.%d", filepath.Base(usbPath), foundIfaceIndex))
	md.DriverName = determineDriver(ifPath)

	// Determine Control Path (cdc-wdm)
	// device.c: detect_path_cdc_wdm_or_qcqmi
	md.ControlPath = findCDCWDM(ifPath)
	if md.ControlPath == "" {
		// Fallback to broader search
		md.ControlPath = findCDCWDMInUSB(usbPath)
	}

	// Find AT ports based on Quectel heuristics (device.c logic)
	// device.c logic for ECM/RNDIS/NCM (but useful for QMI too for AT commands)
	// 基于 Quectel 启发式规则查找 AT 端口 (device.c 逻辑)
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
			atIntf = 3 // Assume default for now / 暂时假设默认值
		default:
			// For EC20 (pid 0x0125) and others, typical default is 2
			// 对于 EC20 (pid 0x0125) 和其他型号，典型默认值为 2
			atIntf = 2
		}
	} else if vid == 0x05c6 {
		// Qualcomm defaults / 高通默认值
		atIntf = 2
	}

	if atIntf != -1 {
		// Look for tty in that specific interface
		atIfPath := filepath.Join(usbPath, fmt.Sprintf("%s:1.%d", filepath.Base(usbPath), atIntf))
		primary, err := findTTYInInterface(atIfPath)
		if err == nil && primary != "" {
			md.ATPort = primary
		}
	}

	// Collect all available ttyUSB ports just in case
	md.ATPorts = findATPorts(usbPath)

	if md.ControlPath == "" {
		// If QMI cdc-wdm not found, maybe it's in ECM mode where AT port IS the control channel?
		// But our project is qmi-cm, so we strictly require QMI (cdc-wdm) mostly.
		// However, returning what we found is better.
		// Warning: functionality will be limited if no control path.
	}

	return md, nil
}

func findCDCWDM(devicePath string) string {
	// Look for usbmisc or usb subdirectory / 查找usbmisc或usb子目录
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

// findATPorts finds all ttyUSB ports associated with the USB device
// findATPorts 查找所有与该USB设备关联的ttyUSB端口
func findATPorts(usbPath string) []string {
	var ports []string

	// usbPath is like /sys/devices/.../usb1/1-1/1-1.2
	// We want to look into /sys/devices/.../usb1/1-1/1-1.2/1-1.2:1.*/ttyUSB*

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

// findTTYInInterface looks for tty directory in the interface path / findTTYInInterface 在接口路径中查找tty目录
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

	// Some systems might have ttyUSBx directly or in other subdirs?
	// The C code seems to look into 'tty' subdir.
	// Try standard Glob if direct read failed
	matches, _ := filepath.Glob(filepath.Join(ifPath, "ttyUSB*"))
	if len(matches) > 0 {
		return filepath.Join("/dev", filepath.Base(matches[0])), nil
	}

	matches2, _ := filepath.Glob(filepath.Join(ifPath, "tty", "ttyUSB*"))
	if len(matches2) > 0 {
		return filepath.Join("/dev", filepath.Base(matches2[0])), nil
	}

	return "", fmt.Errorf("no tty found")
}

// String returns a human-readable description / String返回可读的描述
func (m ModemDevice) String() string {
	return fmt.Sprintf("%s (%s) [%04x:%04x] driver=%s AT=%s",
		m.ControlPath, m.NetInterface, m.VendorID, m.ProductID, m.DriverName, m.ATPort)
}
