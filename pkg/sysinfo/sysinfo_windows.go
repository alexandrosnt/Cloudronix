//go:build windows

package sysinfo

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/host"
)

// getGPUInfo returns GPU information on Windows
func getGPUInfo() string {
	// Use PowerShell to get GPU info (more reliable than WMIC)
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"(Get-CimInstance -ClassName Win32_VideoController).Name")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}

	return ""
}

// getPhysicalRAM returns total physical RAM in bytes using PowerShell
func getPhysicalRAM() uint64 {
	// Use PowerShell to get total physical memory from memory chips
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"(Get-CimInstance -ClassName Win32_PhysicalMemory | Measure-Object -Property Capacity -Sum).Sum")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	line := strings.TrimSpace(string(output))
	if capacity, err := strconv.ParseUint(line, 10, 64); err == nil {
		return capacity
	}

	return 0
}

// getLocalIP returns the local IP address (skips loopback and link-local)
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

// Temperature detection logging - only log once
var tempLoggedOnce bool

// getCPUTemperature returns CPU temperature on Windows
func getCPUTemperature() *float64 {
	shouldLog := !tempLoggedOnce
	if shouldLog {
		tempLoggedOnce = true
	}

	// Try gopsutil sensors first
	temps, err := host.SensorsTemperatures()
	if err == nil && len(temps) > 0 {
		if shouldLog {
			fmt.Printf("[Temp] gopsutil found %d sensors\n", len(temps))
		}
		for _, temp := range temps {
			// Look for CPU-related sensors
			sensorKey := strings.ToLower(temp.SensorKey)
			if shouldLog {
				fmt.Printf("[Temp]   Sensor: %s = %.1f°C\n", temp.SensorKey, temp.Temperature)
			}
			if strings.Contains(sensorKey, "cpu") ||
				strings.Contains(sensorKey, "core") ||
				strings.Contains(sensorKey, "package") {
				if temp.Temperature > 0 && temp.Temperature < 150 {
					return &temp.Temperature
				}
			}
		}
		// Return first valid temp if no CPU-specific one found
		for _, temp := range temps {
			if temp.Temperature > 0 && temp.Temperature < 150 {
				return &temp.Temperature
			}
		}
	} else if err != nil && shouldLog {
		fmt.Printf("[Temp] gopsutil sensors error: %v\n", err)
	} else if shouldLog {
		fmt.Printf("[Temp] gopsutil found 0 sensors\n")
	}

	// Try WMI MSAcpi_ThermalZoneTemperature (requires admin, reports in tenths of Kelvin)
	if shouldLog {
		fmt.Println("[Temp] Trying WMI MSAcpi_ThermalZoneTemperature...")
	}
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-CimInstance -Namespace "root/WMI" -ClassName MSAcpi_ThermalZoneTemperature -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty CurrentTemperature`)
	output, err := cmd.Output()
	if err == nil {
		line := strings.TrimSpace(string(output))
		if shouldLog {
			fmt.Printf("[Temp] WMI MSAcpi raw output: %q\n", line)
		}
		if kelvinTenths, err := strconv.ParseFloat(line, 64); err == nil && kelvinTenths > 0 {
			// Convert from tenths of Kelvin to Celsius
			tempC := (kelvinTenths / 10.0) - 273.15
			if shouldLog {
				fmt.Printf("[Temp] WMI MSAcpi: %.1f°C (from %.0f tenths of Kelvin)\n", tempC, kelvinTenths)
			}
			if tempC > 0 && tempC < 150 {
				return &tempC
			}
		}
	} else if shouldLog {
		fmt.Printf("[Temp] WMI MSAcpi error: %v\n", err)
	}

	// Fallback: Try OpenHardwareMonitor WMI if installed
	if shouldLog {
		fmt.Println("[Temp] Trying OpenHardwareMonitor WMI...")
	}
	cmd = exec.Command("powershell", "-NoProfile", "-Command",
		`Get-CimInstance -Namespace "root/OpenHardwareMonitor" -ClassName Sensor -ErrorAction SilentlyContinue | Where-Object { $_.SensorType -eq 'Temperature' -and $_.Name -like '*CPU*' } | Select-Object -First 1 -ExpandProperty Value`)
	output, err = cmd.Output()
	if err == nil {
		line := strings.TrimSpace(string(output))
		if shouldLog {
			fmt.Printf("[Temp] OpenHardwareMonitor raw output: %q\n", line)
		}
		if temp, err := strconv.ParseFloat(line, 64); err == nil && temp > 0 && temp < 150 {
			if shouldLog {
				fmt.Printf("[Temp] OpenHardwareMonitor: %.1f°C\n", temp)
			}
			return &temp
		}
	} else if shouldLog {
		fmt.Printf("[Temp] OpenHardwareMonitor error: %v\n", err)
	}

	// Last resort: Try LibreHardwareMonitor WMI if installed
	if shouldLog {
		fmt.Println("[Temp] Trying LibreHardwareMonitor WMI...")
	}
	cmd = exec.Command("powershell", "-NoProfile", "-Command",
		`Get-CimInstance -Namespace "root/LibreHardwareMonitor" -ClassName Sensor -ErrorAction SilentlyContinue | Where-Object { $_.SensorType -eq 'Temperature' -and $_.Name -like '*CPU*' } | Select-Object -First 1 -ExpandProperty Value`)
	output, err = cmd.Output()
	if err == nil {
		line := strings.TrimSpace(string(output))
		if shouldLog {
			fmt.Printf("[Temp] LibreHardwareMonitor raw output: %q\n", line)
		}
		if temp, err := strconv.ParseFloat(line, 64); err == nil && temp > 0 && temp < 150 {
			if shouldLog {
				fmt.Printf("[Temp] LibreHardwareMonitor: %.1f°C\n", temp)
			}
			return &temp
		}
	} else if shouldLog {
		fmt.Printf("[Temp] LibreHardwareMonitor error: %v\n", err)
	}

	if shouldLog {
		fmt.Println("[Temp] No temperature source available")
	}
	return nil
}
