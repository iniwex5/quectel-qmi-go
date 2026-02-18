//go:build linux
// +build linux

package netcfg

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/iniwex5/netlink"
)

// LinuxConfigurator implements NetworkConfigurator for Linux using netlink
// LinuxConfigurator 使用 netlink 实现 Linux 的 NetworkConfigurator
type LinuxConfigurator struct{}

func NewLinuxConfigurator() *LinuxConfigurator {
	return &LinuxConfigurator{}
}

func (l *LinuxConfigurator) SetIPAddress(ifname string, ip net.IP, prefixLen int) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	ipNet := &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(prefixLen, 32),
	}

	addr := &netlink.Addr{IPNet: ipNet}

	if err := netlink.AddrAdd(link, addr); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("failed to add address: %w", err)
		}
	}
	return nil
}

func (l *LinuxConfigurator) SetIPv6Address(ifname string, ip net.IP, prefixLen int) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	ipNet := &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(prefixLen, 128),
	}

	addr := &netlink.Addr{IPNet: ipNet}

	if err := netlink.AddrAdd(link, addr); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("failed to add IPv6 address: %w", err)
		}
	}
	return nil
}

func (l *LinuxConfigurator) FlushAddresses(ifname string) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return nil // Interface gone
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil
	}

	for _, addr := range addrs {
		// Ignore errors during cleanup
		_ = netlink.AddrDel(link, &addr)
	}
	return nil
}

func (l *LinuxConfigurator) AddDefaultRoute(ifname string, gateway net.IP) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	var dst *net.IPNet
	if gateway.To4() != nil {
		dst = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	} else {
		dst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Gw:        gateway,
		Priority:  512, // High metric to avoid overriding system default route / 高跃点数避免覆盖系统默认路由
	}

	if err := netlink.RouteAdd(route); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("failed to add default route: %w", err)
		}
	}
	return nil
}

func (l *LinuxConfigurator) AddDefaultRouteDirect(ifname string, ipv6 bool) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	var dst *net.IPNet
	if ipv6 {
		dst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	} else {
		dst = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Priority:  512, // High metric / 高跃点数
	}

	if err := netlink.RouteAdd(route); err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("failed to add default route: %w", err)
		}
	}
	return nil
}

func (l *LinuxConfigurator) FlushRoutes(ifname string) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	routes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list routes: %w", err)
	}

	for _, route := range routes {
		_ = netlink.RouteDel(&route)
	}
	return nil
}

func (l *LinuxConfigurator) BringUp(ifname string) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}
	return netlink.LinkSetUp(link)
}

func (l *LinuxConfigurator) BringDown(ifname string) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return nil // Interface gone
	}
	return netlink.LinkSetDown(link)
}

func (l *LinuxConfigurator) SetMTU(ifname string, mtu int) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifname, err)
	}
	return netlink.LinkSetMTU(link, mtu)
}

func (l *LinuxConfigurator) GetCurrentIP(ifname string) (net.IP, error) {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return nil, fmt.Errorf("interface %s not found: %w", ifname, err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to list addresses: %w", err)
	}

	if len(addrs) == 0 {
		return nil, nil
	}
	return addrs[0].IP, nil
}

func (l *LinuxConfigurator) IsUp(ifname string) (bool, error) {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		return false, fmt.Errorf("interface %s not found: %w", ifname, err)
	}
	return link.Attrs().Flags&net.FlagUp != 0, nil
}

const resolvConfPath = "/etc/resolv.conf"

func (l *LinuxConfigurator) UpdateDNS(dns1, dns2 string) error {
	var lines []string
	if data, err := os.ReadFile(resolvConfPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "nameserver") {
				lines = append(lines, line)
			}
		}
	}

	if dns1 != "" {
		lines = append(lines, "nameserver "+dns1)
	}
	if dns2 != "" && dns2 != dns1 {
		lines = append(lines, "nameserver "+dns2)
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(resolvConfPath, []byte(content), 0644)
}

func (l *LinuxConfigurator) RestoreDNS() error {
	var lines []string
	if data, err := os.ReadFile(resolvConfPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "nameserver") {
				lines = append(lines, line)
			}
		}
	}
	content := strings.Join(lines, "\n")
	return os.WriteFile(resolvConfPath, []byte(content), 0644)
}
