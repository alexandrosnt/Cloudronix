package sysinfo

import (
	"runtime"
)

// SecurityStatus contains the security status of the system
type SecurityStatus struct {
	Firewall       ModuleStatus  `json:"firewall"`
	Antivirus      ModuleStatus  `json:"antivirus"`
	DiskEncryption ModuleStatus  `json:"disk_encryption"`
	AutoUpdates    ModuleStatus  `json:"auto_updates"`
	SecureBoot     ModuleStatus  `json:"secure_boot"`
	UAC            ModuleStatus  `json:"uac"`
	Privacy        PrivacyStatus `json:"privacy"`
	Score          int           `json:"score"`
	Platform       string        `json:"platform"`
}

// ModuleStatus represents the status of a security module
type ModuleStatus struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"` // "enabled", "disabled", "partial", "unknown"
	Details string `json:"details,omitempty"`
}

// PrivacyStatus contains privacy-related settings
type PrivacyStatus struct {
	TelemetryLevel    string `json:"telemetry_level"`    // "full", "enhanced", "basic", "security"
	AdvertisingID     bool   `json:"advertising_id"`
	LocationServices  bool   `json:"location_services"`
	DiagnosticData    bool   `json:"diagnostic_data"`
	ActivityHistory   bool   `json:"activity_history"`
}

// CollectSecurityStatus gathers security information from the system
func CollectSecurityStatus() *SecurityStatus {
	status := &SecurityStatus{
		Firewall:       ModuleStatus{Status: "unknown"},
		Antivirus:      ModuleStatus{Status: "unknown"},
		DiskEncryption: ModuleStatus{Status: "unknown"},
		AutoUpdates:    ModuleStatus{Status: "unknown"},
		SecureBoot:     ModuleStatus{Status: "unknown"},
		UAC:            ModuleStatus{Status: "unknown"},
		Privacy:        PrivacyStatus{TelemetryLevel: "unknown"},
		Platform:       runtime.GOOS,
	}

	// Platform-specific collection is done in security_<platform>.go files
	// via the collectPlatformSecurity function
	collectPlatformSecurity(status)

	// Calculate security score
	status.Score = calculateSecurityScore(status)

	return status
}

func calculateSecurityScore(s *SecurityStatus) int {
	score := 0
	maxScore := 0

	// Firewall: 20 points
	maxScore += 20
	if s.Firewall.Enabled {
		score += 20
	}

	// Antivirus: 25 points
	maxScore += 25
	if s.Antivirus.Enabled {
		score += 25
	}

	// Disk Encryption: 15 points
	maxScore += 15
	if s.DiskEncryption.Enabled {
		score += 15
	}

	// Auto Updates: 15 points
	maxScore += 15
	if s.AutoUpdates.Enabled {
		score += 15
	}

	// Secure Boot: 10 points
	maxScore += 10
	if s.SecureBoot.Enabled {
		score += 10
	}

	// UAC: 10 points
	maxScore += 10
	if s.UAC.Enabled {
		score += 10
	}

	// Privacy (lower telemetry = better): 5 points
	maxScore += 5
	switch s.Privacy.TelemetryLevel {
	case "security":
		score += 5
	case "basic":
		score += 4
	case "enhanced":
		score += 2
	case "full":
		score += 0
	default:
		score += 2 // unknown, assume middle
	}

	if maxScore == 0 {
		return 0
	}
	return (score * 100) / maxScore
}
