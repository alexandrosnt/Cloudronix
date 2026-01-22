//go:build windows

package sysinfo

import (
	"os/exec"
	"strings"
)

func collectPlatformSecurity(status *SecurityStatus) {
	// Check Windows Firewall status
	checkFirewall(status)

	// Check Windows Defender / Antivirus status
	checkAntivirus(status)

	// Check BitLocker status
	checkBitLocker(status)

	// Check Windows Update status
	checkAutoUpdates(status)

	// Check Secure Boot status
	checkSecureBoot(status)

	// Check UAC status
	checkUAC(status)

	// Check Privacy settings
	checkPrivacySettings(status)
}

func checkFirewall(status *SecurityStatus) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-NetFirewallProfile | Select-Object -ExpandProperty Enabled | Where-Object { $_ -eq $true } | Measure-Object | Select-Object -ExpandProperty Count`)
	output, err := cmd.Output()
	if err != nil {
		status.Firewall = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not determine firewall status"}
		return
	}

	count := strings.TrimSpace(string(output))
	if count == "3" {
		status.Firewall = ModuleStatus{Enabled: true, Status: "enabled", Details: "All profiles enabled"}
	} else if count != "0" {
		status.Firewall = ModuleStatus{Enabled: true, Status: "partial", Details: count + " of 3 profiles enabled"}
	} else {
		status.Firewall = ModuleStatus{Enabled: false, Status: "disabled", Details: "Firewall is disabled"}
	}
}

func checkAntivirus(status *SecurityStatus) {
	// Check Windows Defender status
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-MpComputerStatus | Select-Object -ExpandProperty RealTimeProtectionEnabled`)
	output, err := cmd.Output()
	if err != nil {
		status.Antivirus = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not determine antivirus status"}
		return
	}

	result := strings.TrimSpace(strings.ToLower(string(output)))
	if result == "true" {
		status.Antivirus = ModuleStatus{Enabled: true, Status: "enabled", Details: "Windows Defender real-time protection active"}
	} else {
		status.Antivirus = ModuleStatus{Enabled: false, Status: "disabled", Details: "Real-time protection is disabled"}
	}
}

func checkBitLocker(status *SecurityStatus) {
	// Check BitLocker status on C: drive
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-BitLockerVolume -MountPoint C: -ErrorAction SilentlyContinue).ProtectionStatus`)
	output, err := cmd.Output()
	if err != nil {
		status.DiskEncryption = ModuleStatus{Enabled: false, Status: "unknown", Details: "BitLocker status unavailable"}
		return
	}

	result := strings.TrimSpace(string(output))
	if result == "On" || result == "1" {
		status.DiskEncryption = ModuleStatus{Enabled: true, Status: "enabled", Details: "BitLocker is enabled on system drive"}
	} else if result == "Off" || result == "0" {
		status.DiskEncryption = ModuleStatus{Enabled: false, Status: "disabled", Details: "BitLocker is not enabled"}
	} else {
		status.DiskEncryption = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not determine BitLocker status"}
	}
}

func checkAutoUpdates(status *SecurityStatus) {
	// Check Windows Update service status
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-Service -Name wuauserv).Status`)
	output, err := cmd.Output()
	if err != nil {
		status.AutoUpdates = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not check Windows Update service"}
		return
	}

	result := strings.TrimSpace(string(output))
	if result == "Running" {
		status.AutoUpdates = ModuleStatus{Enabled: true, Status: "enabled", Details: "Windows Update service is running"}
	} else {
		status.AutoUpdates = ModuleStatus{Enabled: false, Status: "disabled", Details: "Windows Update service is " + result}
	}
}

func checkSecureBoot(status *SecurityStatus) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Confirm-SecureBootUEFI -ErrorAction SilentlyContinue`)
	output, err := cmd.Output()
	if err != nil {
		// Secure Boot might not be supported or we don't have permission
		status.SecureBoot = ModuleStatus{Enabled: false, Status: "unknown", Details: "Secure Boot status unavailable"}
		return
	}

	result := strings.TrimSpace(strings.ToLower(string(output)))
	if result == "true" {
		status.SecureBoot = ModuleStatus{Enabled: true, Status: "enabled", Details: "Secure Boot is enabled"}
	} else {
		status.SecureBoot = ModuleStatus{Enabled: false, Status: "disabled", Details: "Secure Boot is disabled"}
	}
}

func checkUAC(status *SecurityStatus) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System' -Name EnableLUA -ErrorAction SilentlyContinue).EnableLUA`)
	output, err := cmd.Output()
	if err != nil {
		status.UAC = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not check UAC status"}
		return
	}

	result := strings.TrimSpace(string(output))
	if result == "1" {
		status.UAC = ModuleStatus{Enabled: true, Status: "enabled", Details: "User Account Control is enabled"}
	} else {
		status.UAC = ModuleStatus{Enabled: false, Status: "disabled", Details: "User Account Control is disabled"}
	}
}

func checkPrivacySettings(status *SecurityStatus) {
	// Check telemetry level
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\DataCollection' -Name AllowTelemetry -ErrorAction SilentlyContinue).AllowTelemetry`)
	output, _ := cmd.Output()
	result := strings.TrimSpace(string(output))
	switch result {
	case "0":
		status.Privacy.TelemetryLevel = "security"
	case "1":
		status.Privacy.TelemetryLevel = "basic"
	case "2":
		status.Privacy.TelemetryLevel = "enhanced"
	case "3":
		status.Privacy.TelemetryLevel = "full"
	default:
		status.Privacy.TelemetryLevel = "unknown"
	}

	// Check Advertising ID
	cmd = exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-ItemProperty -Path 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\AdvertisingInfo' -Name Enabled -ErrorAction SilentlyContinue).Enabled`)
	output, _ = cmd.Output()
	status.Privacy.AdvertisingID = strings.TrimSpace(string(output)) == "1"

	// Check Location Services
	cmd = exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\location' -Name Value -ErrorAction SilentlyContinue).Value`)
	output, _ = cmd.Output()
	status.Privacy.LocationServices = strings.TrimSpace(string(output)) == "Allow"

	// Check Diagnostic Data (same as telemetry but user-facing)
	status.Privacy.DiagnosticData = status.Privacy.TelemetryLevel == "full" || status.Privacy.TelemetryLevel == "enhanced"

	// Check Activity History
	cmd = exec.Command("powershell", "-NoProfile", "-Command",
		`(Get-ItemProperty -Path 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\System' -Name EnableActivityFeed -ErrorAction SilentlyContinue).EnableActivityFeed`)
	output, _ = cmd.Output()
	result = strings.TrimSpace(string(output))
	status.Privacy.ActivityHistory = result != "0"
}
