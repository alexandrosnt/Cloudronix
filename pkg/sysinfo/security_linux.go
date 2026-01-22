//go:build linux

package sysinfo

import (
	"os"
	"os/exec"
	"strings"
)

func collectPlatformSecurity(status *SecurityStatus) {
	// Check firewall status (iptables/nftables/ufw/firewalld)
	checkLinuxFirewall(status)

	// Check for antivirus (ClamAV is common on Linux)
	checkLinuxAntivirus(status)

	// Check disk encryption (LUKS)
	checkLUKS(status)

	// Check auto updates
	checkLinuxAutoUpdates(status)

	// Check Secure Boot
	checkLinuxSecureBoot(status)

	// Check SELinux/AppArmor (equivalent to UAC)
	checkMACSystem(status)

	// Check privacy settings
	checkLinuxPrivacy(status)
}

func checkLinuxFirewall(status *SecurityStatus) {
	// Try UFW first (most common on Ubuntu/Debian)
	cmd := exec.Command("ufw", "status")
	output, err := cmd.Output()
	if err == nil {
		result := strings.ToLower(string(output))
		if strings.Contains(result, "status: active") {
			status.Firewall = ModuleStatus{Enabled: true, Status: "enabled", Details: "UFW firewall is active"}
			return
		} else if strings.Contains(result, "status: inactive") {
			status.Firewall = ModuleStatus{Enabled: false, Status: "disabled", Details: "UFW firewall is inactive"}
			return
		}
	}

	// Try firewalld (common on RHEL/Fedora/CentOS)
	cmd = exec.Command("systemctl", "is-active", "firewalld")
	output, err = cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "active" {
		status.Firewall = ModuleStatus{Enabled: true, Status: "enabled", Details: "firewalld is active"}
		return
	}

	// Check iptables rules exist
	cmd = exec.Command("iptables", "-L", "-n")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		ruleCount := 0
		for _, line := range lines {
			if strings.HasPrefix(line, "ACCEPT") || strings.HasPrefix(line, "DROP") || strings.HasPrefix(line, "REJECT") {
				ruleCount++
			}
		}
		if ruleCount > 0 {
			status.Firewall = ModuleStatus{Enabled: true, Status: "enabled", Details: "iptables rules configured"}
			return
		}
	}

	// Check nftables
	cmd = exec.Command("nft", "list", "ruleset")
	output, err = cmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		status.Firewall = ModuleStatus{Enabled: true, Status: "enabled", Details: "nftables rules configured"}
		return
	}

	status.Firewall = ModuleStatus{Enabled: false, Status: "unknown", Details: "No firewall detected"}
}

func checkLinuxAntivirus(status *SecurityStatus) {
	// Check for ClamAV daemon
	cmd := exec.Command("systemctl", "is-active", "clamav-daemon")
	output, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "active" {
		status.Antivirus = ModuleStatus{Enabled: true, Status: "enabled", Details: "ClamAV daemon is active"}
		return
	}

	// Check if clamd is running
	cmd = exec.Command("pgrep", "-x", "clamd")
	if err := cmd.Run(); err == nil {
		status.Antivirus = ModuleStatus{Enabled: true, Status: "enabled", Details: "ClamAV daemon is running"}
		return
	}

	// Check for other common AV solutions
	avProcesses := []string{"sophos", "symantec", "mcafee", "avg", "avast", "bitdefender", "kaspersky", "eset"}
	cmd = exec.Command("ps", "aux")
	output, err = cmd.Output()
	if err == nil {
		outputLower := strings.ToLower(string(output))
		for _, av := range avProcesses {
			if strings.Contains(outputLower, av) {
				status.Antivirus = ModuleStatus{Enabled: true, Status: "enabled", Details: av + " antivirus detected"}
				return
			}
		}
	}

	// Linux typically doesn't need AV - note this as informational
	status.Antivirus = ModuleStatus{Enabled: false, Status: "not_installed", Details: "No antivirus installed (optional on Linux)"}
}

func checkLUKS(status *SecurityStatus) {
	// Check if root filesystem is on LUKS
	cmd := exec.Command("lsblk", "-o", "NAME,TYPE,MOUNTPOINT", "-J")
	output, err := cmd.Output()
	if err == nil && strings.Contains(string(output), "crypt") {
		status.DiskEncryption = ModuleStatus{Enabled: true, Status: "enabled", Details: "LUKS encryption detected"}
		return
	}

	// Check /etc/crypttab for configured encrypted volumes
	if data, err := os.ReadFile("/etc/crypttab"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				status.DiskEncryption = ModuleStatus{Enabled: true, Status: "enabled", Details: "Encrypted volumes configured in crypttab"}
				return
			}
		}
	}

	// Check dmsetup for active crypt targets
	cmd = exec.Command("dmsetup", "ls", "--target", "crypt")
	output, err = cmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 && !strings.Contains(string(output), "No devices found") {
		status.DiskEncryption = ModuleStatus{Enabled: true, Status: "enabled", Details: "dm-crypt volumes active"}
		return
	}

	status.DiskEncryption = ModuleStatus{Enabled: false, Status: "disabled", Details: "No disk encryption detected"}
}

func checkLinuxAutoUpdates(status *SecurityStatus) {
	// Check unattended-upgrades (Debian/Ubuntu)
	cmd := exec.Command("systemctl", "is-enabled", "unattended-upgrades")
	output, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "enabled" {
		status.AutoUpdates = ModuleStatus{Enabled: true, Status: "enabled", Details: "unattended-upgrades is enabled"}
		return
	}

	// Check apt-daily timer
	cmd = exec.Command("systemctl", "is-active", "apt-daily.timer")
	output, err = cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "active" {
		status.AutoUpdates = ModuleStatus{Enabled: true, Status: "enabled", Details: "apt-daily timer is active"}
		return
	}

	// Check dnf-automatic (Fedora/RHEL)
	cmd = exec.Command("systemctl", "is-enabled", "dnf-automatic.timer")
	output, err = cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "enabled" {
		status.AutoUpdates = ModuleStatus{Enabled: true, Status: "enabled", Details: "dnf-automatic is enabled"}
		return
	}

	// Check yum-cron (older RHEL/CentOS)
	cmd = exec.Command("systemctl", "is-enabled", "yum-cron")
	output, err = cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "enabled" {
		status.AutoUpdates = ModuleStatus{Enabled: true, Status: "enabled", Details: "yum-cron is enabled"}
		return
	}

	status.AutoUpdates = ModuleStatus{Enabled: false, Status: "disabled", Details: "Automatic updates not configured"}
}

func checkLinuxSecureBoot(status *SecurityStatus) {
	// Check mokutil for Secure Boot status
	cmd := exec.Command("mokutil", "--sb-state")
	output, err := cmd.Output()
	if err == nil {
		result := strings.ToLower(string(output))
		if strings.Contains(result, "secureboot enabled") {
			status.SecureBoot = ModuleStatus{Enabled: true, Status: "enabled", Details: "Secure Boot is enabled"}
			return
		} else if strings.Contains(result, "secureboot disabled") {
			status.SecureBoot = ModuleStatus{Enabled: false, Status: "disabled", Details: "Secure Boot is disabled"}
			return
		}
	}

	// Alternative: check /sys/firmware/efi/efivars
	if _, err := os.Stat("/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"); err == nil {
		// File exists, read the value
		data, err := os.ReadFile("/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c")
		if err == nil && len(data) >= 5 && data[4] == 1 {
			status.SecureBoot = ModuleStatus{Enabled: true, Status: "enabled", Details: "Secure Boot is enabled"}
			return
		}
	}

	// Check if system is even UEFI
	if _, err := os.Stat("/sys/firmware/efi"); os.IsNotExist(err) {
		status.SecureBoot = ModuleStatus{Enabled: false, Status: "not_available", Details: "System uses legacy BIOS (not UEFI)"}
		return
	}

	status.SecureBoot = ModuleStatus{Enabled: false, Status: "unknown", Details: "Could not determine Secure Boot status"}
}

func checkMACSystem(status *SecurityStatus) {
	// Check SELinux
	cmd := exec.Command("getenforce")
	output, err := cmd.Output()
	if err == nil {
		result := strings.TrimSpace(string(output))
		if result == "Enforcing" {
			status.UAC = ModuleStatus{Enabled: true, Status: "enabled", Details: "SELinux is enforcing"}
			return
		} else if result == "Permissive" {
			status.UAC = ModuleStatus{Enabled: true, Status: "partial", Details: "SELinux is permissive (logging only)"}
			return
		}
	}

	// Check AppArmor
	cmd = exec.Command("aa-status", "--enabled")
	if err := cmd.Run(); err == nil {
		// Get more details
		cmd = exec.Command("aa-status")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, "profiles are loaded") {
					status.UAC = ModuleStatus{Enabled: true, Status: "enabled", Details: "AppArmor is active - " + strings.TrimSpace(line)}
					return
				}
			}
		}
		status.UAC = ModuleStatus{Enabled: true, Status: "enabled", Details: "AppArmor is enabled"}
		return
	}

	// Check if AppArmor module is loaded
	if data, err := os.ReadFile("/sys/module/apparmor/parameters/enabled"); err == nil {
		if strings.TrimSpace(string(data)) == "Y" {
			status.UAC = ModuleStatus{Enabled: true, Status: "enabled", Details: "AppArmor kernel module is enabled"}
			return
		}
	}

	status.UAC = ModuleStatus{Enabled: false, Status: "disabled", Details: "No MAC system (SELinux/AppArmor) detected"}
}

func checkLinuxPrivacy(status *SecurityStatus) {
	// Linux doesn't have centralized telemetry like Windows
	// Check for common telemetry opt-outs

	// Check if Ubuntu telemetry is disabled
	if data, err := os.ReadFile("/etc/ubuntu-advantage/uaclient.conf"); err == nil {
		if strings.Contains(string(data), "enable_telemetry: false") {
			status.Privacy.TelemetryLevel = "security"
		} else {
			status.Privacy.TelemetryLevel = "basic"
		}
	} else {
		// Most Linux distros don't have system-level telemetry
		status.Privacy.TelemetryLevel = "security"
	}

	// Linux doesn't have advertising ID
	status.Privacy.AdvertisingID = false

	// Check if location services are available (GNOME)
	cmd := exec.Command("gsettings", "get", "org.gnome.system.location", "enabled")
	output, _ := cmd.Output()
	status.Privacy.LocationServices = strings.TrimSpace(string(output)) == "true"

	// No centralized diagnostic data on Linux
	status.Privacy.DiagnosticData = false

	// Check for shell history (user activity tracking)
	homeDir, _ := os.UserHomeDir()
	if _, err := os.Stat(homeDir + "/.bash_history"); err == nil {
		status.Privacy.ActivityHistory = true
	} else {
		status.Privacy.ActivityHistory = false
	}
}
