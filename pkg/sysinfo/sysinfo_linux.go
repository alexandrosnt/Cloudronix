//go:build linux

package sysinfo

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/host"
)

// getGPUInfo returns GPU information on Linux
func getGPUInfo() string {
	// Try lspci first
	cmd := exec.Command("lspci")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), "vga") ||
				strings.Contains(strings.ToLower(line), "3d") ||
				strings.Contains(strings.ToLower(line), "display") {
				// Extract GPU name after the colon
				parts := strings.SplitN(line, ": ", 2)
				if len(parts) >= 2 {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}

	// Try reading from /sys
	matches, _ := filepath.Glob("/sys/class/drm/card*/device/vendor")
	if len(matches) > 0 {
		return "GPU detected"
	}

	return ""
}

// getLocalIP returns the local IP address
func getLocalIP() string {
	// Try to get the IP from the default route interface
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

// getPhysicalRAM returns total physical RAM in bytes from /proc/meminfo
func getPhysicalRAM() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				// MemTotal is in kB
				if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					return kb * 1024
				}
			}
		}
	}

	return 0
}

// getCPUTemperature returns CPU temperature on Linux
func getCPUTemperature() *float64 {
	// Try gopsutil sensors first
	temps, err := host.SensorsTemperatures()
	if err == nil {
		for _, temp := range temps {
			sensorKey := strings.ToLower(temp.SensorKey)
			if strings.Contains(sensorKey, "coretemp") ||
				strings.Contains(sensorKey, "cpu") ||
				strings.Contains(sensorKey, "k10temp") ||
				strings.Contains(sensorKey, "acpitz") {
				if temp.Temperature > 0 && temp.Temperature < 150 {
					return &temp.Temperature
				}
			}
		}
	}

	// Fallback: Read from /sys/class/thermal
	matches, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if tempMilliC, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			temp := tempMilliC / 1000.0
			if temp > 0 && temp < 150 {
				return &temp
			}
		}
	}

	return nil
}
