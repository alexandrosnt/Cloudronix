//go:build android

package sysinfo

import (
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// getGPUInfo returns GPU information on Android (limited)
func getGPUInfo() string {
	// Android GPU info is typically not accessible without root
	cmd := exec.Command("getprop", "ro.hardware.gpu")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
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

// getPhysicalRAM returns 0 on Android (falls back to virtual memory detection)
func getPhysicalRAM() uint64 {
	// Android typically requires root for accurate hardware info
	// Return 0 to use fallback virtual memory detection
	return 0
}

// getCPUTemperature returns CPU temperature on Android
func getCPUTemperature() *float64 {
	// Android temperature sensors require root access
	// Try reading from common thermal zone paths
	cmd := exec.Command("cat", "/sys/class/thermal/thermal_zone0/temp")
	output, err := cmd.Output()
	if err == nil {
		line := strings.TrimSpace(string(output))
		if tempMilliC, err := strconv.ParseFloat(line, 64); err == nil {
			temp := tempMilliC / 1000.0
			if temp > 0 && temp < 150 {
				return &temp
			}
		}
	}

	return nil
}
