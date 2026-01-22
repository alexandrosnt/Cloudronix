//go:build darwin

package sysinfo

import (
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// getGPUInfo returns GPU information on macOS
func getGPUInfo() string {
	cmd := exec.Command("system_profiler", "SPDisplaysDataType")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Chipset Model:") {
			return strings.TrimPrefix(line, "Chipset Model:")
		}
		if strings.HasPrefix(line, "Chip:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Chip:"))
		}
	}

	return ""
}

// getLocalIP returns the local IP address
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				// Skip link-local addresses (169.254.x.x)
				if ip4[0] == 169 && ip4[1] == 254 {
					continue
				}
				return ip4.String()
			}
		}
	}

	return ""
}

// getPhysicalRAM returns total physical RAM in bytes using sysctl
func getPhysicalRAM() uint64 {
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	memsize, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0
	}

	return memsize
}

// getCPUTemperature returns CPU temperature on macOS
func getCPUTemperature() *float64 {
	// macOS doesn't expose temperature via standard APIs
	// Requires SMC access or third-party tools like osx-cpu-temp
	cmd := exec.Command("osx-cpu-temp", "-C")
	output, err := cmd.Output()
	if err == nil {
		line := strings.TrimSpace(string(output))
		line = strings.TrimSuffix(line, "°C")
		if temp, err := strconv.ParseFloat(line, 64); err == nil && temp > 0 && temp < 150 {
			return &temp
		}
	}

	// Temperature not available on macOS without special tools
	return nil
}
