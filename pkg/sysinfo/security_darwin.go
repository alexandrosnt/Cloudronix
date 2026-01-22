//go:build darwin

package sysinfo

import (
	"os/exec"
	"strings"
)

func collectPlatformSecurity(status *SecurityStatus) {
	// Check macOS Application Firewall
	checkMacFirewall(status)

	// Check XProtect (built-in antivirus)
	checkXProtect(status)

	// Check FileVault (disk encryption)
	checkFileVault(status)

	// Check Software Update auto-updates
	checkMacAutoUpdates(status)

	// Check Secure Boot (for T2/Apple Silicon Macs)
	checkMacSecureBoot(status)

	// Check System Integrity Protection (SIP)
	checkSIP(status)

	// Check Gatekeeper
	checkGatekeeper(status)

	// Check privacy settings
	checkMacPrivacy(status)
}

func checkMacFirewall(status *SecurityStatus) {
	// Check Application Firewall status
	cmd := exec.Command("/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate")
	output, err := cmd.Output()
	if err != nil {
		// Try alternative method
		cmd = exec.Command("defaults", "read", "/Library/Preferences/com.apple.alf", "globalstate")
		output, err = cmd.Output()
		if err != nil {
			status.Firewall = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not determine firewall status"}
			return
		}
		result := strings.TrimSpace(string(output))
		if result == "1" || result == "2" {
			status.Firewall = ModuleStatus{Enabled: true, Status: "enabled", Details: "Application Firewall is enabled"}
		} else {
			status.Firewall = ModuleStatus{Enabled: false, Status: "disabled", Details: "Application Firewall is disabled"}
		}
		return
	}

	result := strings.ToLower(string(output))
	if strings.Contains(result, "enabled") {
		status.Firewall = ModuleStatus{Enabled: true, Status: "enabled", Details: "Application Firewall is enabled"}
	} else {
		status.Firewall = ModuleStatus{Enabled: false, Status: "disabled", Details: "Application Firewall is disabled"}
	}
}

func checkXProtect(status *SecurityStatus) {
	// XProtect is always enabled on macOS, check if it's up to date
	cmd := exec.Command("system_profiler", "SPInstallHistoryDataType", "-detailLevel", "mini")
	output, err := cmd.Output()
	if err == nil {
		result := string(output)
		if strings.Contains(result, "XProtect") {
			status.Antivirus = ModuleStatus{Enabled: true, Status: "enabled", Details: "XProtect is active and updated"}
			return
		}
	}

	// Check XProtect plist exists
	cmd = exec.Command("ls", "/Library/Apple/System/Library/CoreServices/XProtect.bundle")
	if err := cmd.Run(); err == nil {
		status.Antivirus = ModuleStatus{Enabled: true, Status: "enabled", Details: "XProtect is installed"}
		return
	}

	// XProtect should always be present on modern macOS
	status.Antivirus = ModuleStatus{Enabled: true, Status: "enabled", Details: "XProtect (built-in malware protection)"}
}

func checkFileVault(status *SecurityStatus) {
	cmd := exec.Command("fdesetup", "status")
	output, err := cmd.Output()
	if err != nil {
		status.DiskEncryption = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not determine FileVault status"}
		return
	}

	result := string(output)
	if strings.Contains(result, "FileVault is On") {
		status.DiskEncryption = ModuleStatus{Enabled: true, Status: "enabled", Details: "FileVault is enabled"}
	} else if strings.Contains(result, "FileVault is Off") {
		status.DiskEncryption = ModuleStatus{Enabled: false, Status: "disabled", Details: "FileVault is disabled"}
	} else if strings.Contains(result, "Encryption in progress") {
		status.DiskEncryption = ModuleStatus{Enabled: true, Status: "partial", Details: "FileVault encryption in progress"}
	} else {
		status.DiskEncryption = ModuleStatus{Enabled: false, Status: "unknown", Details: result}
	}
}

func checkMacAutoUpdates(status *SecurityStatus) {
	// Check if automatic updates are enabled
	cmd := exec.Command("defaults", "read", "/Library/Preferences/com.apple.SoftwareUpdate", "AutomaticCheckEnabled")
	output, err := cmd.Output()
	autoCheck := err == nil && strings.TrimSpace(string(output)) == "1"

	cmd = exec.Command("defaults", "read", "/Library/Preferences/com.apple.SoftwareUpdate", "AutomaticDownload")
	output, err = cmd.Output()
	autoDownload := err == nil && strings.TrimSpace(string(output)) == "1"

	cmd = exec.Command("defaults", "read", "/Library/Preferences/com.apple.SoftwareUpdate", "AutomaticallyInstallMacOSUpdates")
	output, err = cmd.Output()
	autoInstall := err == nil && strings.TrimSpace(string(output)) == "1"

	if autoCheck && autoDownload && autoInstall {
		status.AutoUpdates = ModuleStatus{Enabled: true, Status: "enabled", Details: "Automatic updates fully enabled"}
	} else if autoCheck {
		status.AutoUpdates = ModuleStatus{Enabled: true, Status: "partial", Details: "Auto-check enabled, auto-install disabled"}
	} else {
		status.AutoUpdates = ModuleStatus{Enabled: false, Status: "disabled", Details: "Automatic updates disabled"}
	}
}

func checkMacSecureBoot(status *SecurityStatus) {
	// Check Secure Boot status (requires T2 chip or Apple Silicon)
	cmd := exec.Command("system_profiler", "SPiBridgeDataType")
	output, err := cmd.Output()
	if err == nil && strings.Contains(string(output), "Secure Boot") {
		result := string(output)
		if strings.Contains(result, "Full Security") {
			status.SecureBoot = ModuleStatus{Enabled: true, Status: "enabled", Details: "Secure Boot: Full Security"}
			return
		} else if strings.Contains(result, "Medium Security") {
			status.SecureBoot = ModuleStatus{Enabled: true, Status: "partial", Details: "Secure Boot: Medium Security"}
			return
		} else if strings.Contains(result, "No Security") {
			status.SecureBoot = ModuleStatus{Enabled: false, Status: "disabled", Details: "Secure Boot: No Security"}
			return
		}
	}

	// Check for Apple Silicon
	cmd = exec.Command("sysctl", "-n", "machdep.cpu.brand_string")
	output, err = cmd.Output()
	if err == nil && strings.Contains(string(output), "Apple") {
		// Apple Silicon Macs always have Secure Boot
		status.SecureBoot = ModuleStatus{Enabled: true, Status: "enabled", Details: "Apple Silicon (Secure Boot built-in)"}
		return
	}

	// Older Mac without T2 chip
	status.SecureBoot = ModuleStatus{Enabled: false, Status: "not_available", Details: "Mac without T2 chip or Apple Silicon"}
}

func checkSIP(status *SecurityStatus) {
	cmd := exec.Command("csrutil", "status")
	output, err := cmd.Output()
	if err != nil {
		status.UAC = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not determine SIP status"}
		return
	}

	result := string(output)
	if strings.Contains(result, "enabled") {
		status.UAC = ModuleStatus{Enabled: true, Status: "enabled", Details: "System Integrity Protection is enabled"}
	} else if strings.Contains(result, "disabled") {
		status.UAC = ModuleStatus{Enabled: false, Status: "disabled", Details: "System Integrity Protection is DISABLED"}
	} else {
		// Might be partially enabled
		status.UAC = ModuleStatus{Enabled: true, Status: "partial", Details: "SIP is partially enabled"}
	}
}

func checkGatekeeper(status *SecurityStatus) {
	cmd := exec.Command("spctl", "--status")
	output, err := cmd.Output()
	if err != nil {
		return // Gatekeeper check optional, SIP is primary
	}

	result := string(output)
	if strings.Contains(result, "assessments enabled") {
		// Gatekeeper is enabled - could add to UAC details
		if status.UAC.Enabled {
			status.UAC.Details += " + Gatekeeper enabled"
		}
	}
}

func checkMacPrivacy(status *SecurityStatus) {
	// Check analytics sharing
	cmd := exec.Command("defaults", "read", "/Library/Application Support/CrashReporter/DiagnosticMessagesHistory.plist", "AutoSubmit")
	output, _ := cmd.Output()
	if strings.TrimSpace(string(output)) == "0" {
		status.Privacy.TelemetryLevel = "security"
	} else {
		status.Privacy.TelemetryLevel = "basic"
	}

	// Check personalized ads
	cmd = exec.Command("defaults", "read", "com.apple.AdLib", "allowApplePersonalizedAdvertising")
	output, _ = cmd.Output()
	status.Privacy.AdvertisingID = strings.TrimSpace(string(output)) == "1"

	// Check Location Services
	cmd = exec.Command("defaults", "read", "/var/db/locationd/Library/Preferences/ByHost/com.apple.locationd", "LocationServicesEnabled")
	output, _ = cmd.Output()
	status.Privacy.LocationServices = strings.TrimSpace(string(output)) == "1"

	// Check diagnostic data
	status.Privacy.DiagnosticData = status.Privacy.TelemetryLevel != "security"

	// Check Siri history (activity history equivalent)
	cmd = exec.Command("defaults", "read", "com.apple.assistant.support", "Siri Data Sharing Opt-In Status")
	output, _ = cmd.Output()
	status.Privacy.ActivityHistory = strings.TrimSpace(string(output)) == "2"
}
