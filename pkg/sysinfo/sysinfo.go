package sysinfo

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// SystemInfo contains system information
type SystemInfo struct {
	OSName       string          `json:"os_name"`
	OSVersion    string          `json:"os_version"`
	Hostname     string          `json:"hostname"`
	Architecture string          `json:"architecture"`
	Specs        *Specs          `json:"specs,omitempty"`
	LocalIP      string          `json:"local_ip,omitempty"`
	AgentVersion string          `json:"agent_version,omitempty"`
	Security     *SecurityStatus `json:"security,omitempty"`
}

// Specs contains hardware specifications
type Specs struct {
	CPU  string `json:"cpu,omitempty"`
	RAM  string `json:"ram,omitempty"`
	GPU  string `json:"gpu,omitempty"`
	Disk string `json:"disk,omitempty"`
}

// Collect gathers system information
func Collect() *SystemInfo {
	info := &SystemInfo{
		Architecture: runtime.GOARCH,
	}

	// Get hostname
	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	}

	// Get host info
	if hostInfo, err := host.Info(); err == nil {
		info.OSName = hostInfo.Platform
		info.OSVersion = hostInfo.PlatformVersion
		if info.OSName == "" {
			info.OSName = hostInfo.OS
		}
	} else {
		info.OSName = runtime.GOOS
	}

	// Collect hardware specs
	info.Specs = collectSpecs()

	// Get local IP
	info.LocalIP = getLocalIP()

	// Collect security status
	info.Security = CollectSecurityStatus()

	return info
}

// collectSpecs gathers hardware specifications
func collectSpecs() *Specs {
	specs := &Specs{}

	// CPU info
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		specs.CPU = cpuInfo[0].ModelName
	}

	// Memory info - try physical RAM first, fall back to virtual memory
	if physicalRAM := getPhysicalRAM(); physicalRAM > 0 {
		totalGB := float64(physicalRAM) / (1024 * 1024 * 1024)
		specs.RAM = formatMemory(totalGB)
	} else if memInfo, err := mem.VirtualMemory(); err == nil {
		totalGB := float64(memInfo.Total) / (1024 * 1024 * 1024)
		specs.RAM = formatMemory(totalGB)
	}

	// GPU info (platform-specific, implemented in platform files)
	specs.GPU = getGPUInfo()

	return specs
}

// formatMemory formats memory size in human-readable format
func formatMemory(gb float64) string {
	if gb >= 1024 {
		return fmt.Sprintf("%.1f TB", gb/1024)
	}
	return fmt.Sprintf("%.0f GB", gb)
}

// ============================================================================
// Real-time Metrics Collection
// ============================================================================

// Metrics contains real-time system metrics
type Metrics struct {
	Timestamp    time.Time      `json:"timestamp"`
	CPU          CPUMetrics     `json:"cpu"`
	Memory       MemoryMetrics  `json:"memory"`
	Disk         DiskMetrics    `json:"disk"`
	Network      NetworkMetrics `json:"network"`
	Temperature  *float64       `json:"temperature,omitempty"`
	Uptime       uint64         `json:"uptime"`
	TopProcesses []ProcessInfo  `json:"top_processes"`
}

// CPUMetrics contains CPU usage information
type CPUMetrics struct {
	UsagePercent float64   `json:"usage_percent"`
	CoreCount    int       `json:"core_count"`
	PerCore      []float64 `json:"per_core,omitempty"`
}

// MemoryMetrics contains memory usage information
type MemoryMetrics struct {
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Available    uint64  `json:"available"`
	UsagePercent float64 `json:"usage_percent"`
}

// DiskMetrics contains disk usage information
type DiskMetrics struct {
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Free         uint64  `json:"free"`
	UsagePercent float64 `json:"usage_percent"`
	Path         string  `json:"path"`
}

// NetworkMetrics contains network I/O information
type NetworkMetrics struct {
	BytesSent     uint64 `json:"bytes_sent"`
	BytesRecv     uint64 `json:"bytes_recv"`
	BytesSentRate uint64 `json:"bytes_sent_rate"` // bytes per second
	BytesRecvRate uint64 `json:"bytes_recv_rate"` // bytes per second
}

// ProcessInfo contains information about a running process
type ProcessInfo struct {
	PID        int32   `json:"pid"`
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float32 `json:"mem_percent"`
	Memory     uint64  `json:"memory"`
}

// Previous network stats for rate calculation
var (
	prevNetStats     *net.IOCountersStat
	prevNetStatsTime time.Time
)

// CollectMetrics gathers real-time system metrics
func CollectMetrics() *Metrics {
	metrics := &Metrics{
		Timestamp: time.Now().UTC(),
	}

	// CPU usage (with 500ms sample interval for accuracy)
	if cpuPercent, err := cpu.Percent(500*time.Millisecond, false); err == nil && len(cpuPercent) > 0 {
		metrics.CPU.UsagePercent = cpuPercent[0]
	}

	// Per-core CPU usage
	if perCore, err := cpu.Percent(0, true); err == nil {
		metrics.CPU.PerCore = perCore
		metrics.CPU.CoreCount = len(perCore)
	} else if count, err := cpu.Counts(true); err == nil {
		metrics.CPU.CoreCount = count
	}

	// Memory usage
	if memInfo, err := mem.VirtualMemory(); err == nil {
		metrics.Memory = MemoryMetrics{
			Total:        memInfo.Total,
			Used:         memInfo.Used,
			Available:    memInfo.Available,
			UsagePercent: memInfo.UsedPercent,
		}
	}

	// Disk usage (primary disk)
	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = "C:"
	}
	if diskInfo, err := disk.Usage(diskPath); err == nil {
		metrics.Disk = DiskMetrics{
			Total:        diskInfo.Total,
			Used:         diskInfo.Used,
			Free:         diskInfo.Free,
			UsagePercent: diskInfo.UsedPercent,
			Path:         diskPath,
		}
	}

	// Network I/O with rate calculation
	if netStats, err := net.IOCounters(false); err == nil && len(netStats) > 0 {
		current := &netStats[0]
		metrics.Network.BytesSent = current.BytesSent
		metrics.Network.BytesRecv = current.BytesRecv

		// Calculate rates if we have previous stats
		if prevNetStats != nil {
			elapsed := time.Since(prevNetStatsTime).Seconds()
			if elapsed > 0 {
				metrics.Network.BytesSentRate = uint64(float64(current.BytesSent-prevNetStats.BytesSent) / elapsed)
				metrics.Network.BytesRecvRate = uint64(float64(current.BytesRecv-prevNetStats.BytesRecv) / elapsed)
			}
		}

		// Store for next calculation
		prevNetStats = current
		prevNetStatsTime = time.Now()
	}

	// CPU temperature (platform-specific)
	metrics.Temperature = getCPUTemperature()

	// System uptime
	if hostInfo, err := host.Info(); err == nil {
		metrics.Uptime = hostInfo.Uptime
	}

	// Top processes by CPU usage
	metrics.TopProcesses = getTopProcesses(10)

	return metrics
}

// getTopProcesses returns the top N processes sorted by CPU usage
func getTopProcesses(n int) []ProcessInfo {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}

	var processes []ProcessInfo
	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}

		cpuPercent, _ := p.CPUPercent()
		memPercent, _ := p.MemoryPercent()
		memInfo, _ := p.MemoryInfo()

		var memBytes uint64
		if memInfo != nil {
			memBytes = memInfo.RSS
		}

		// Skip idle/system processes with 0% usage
		if cpuPercent == 0 && memPercent == 0 {
			continue
		}

		processes = append(processes, ProcessInfo{
			PID:        p.Pid,
			Name:       name,
			CPUPercent: cpuPercent,
			MemPercent: memPercent,
			Memory:     memBytes,
		})
	}

	// Sort by CPU usage (descending)
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].CPUPercent > processes[j].CPUPercent
	})

	// Return top N
	if len(processes) > n {
		processes = processes[:n]
	}

	return processes
}
