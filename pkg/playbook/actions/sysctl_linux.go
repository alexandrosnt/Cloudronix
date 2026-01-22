//go:build linux

package actions

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudronix/agent/pkg/playbook"
)

// SysctlHandler manages Linux kernel parameters
type SysctlHandler struct{}

// NewSysctlHandler creates a new sysctl handler
func NewSysctlHandler() *SysctlHandler {
	return &SysctlHandler{}
}

// Supports returns Linux only
func (h *SysctlHandler) Supports() []string {
	return []string{"linux"}
}

// Validate checks if the params are valid
func (h *SysctlHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["name"]; !ok {
		return fmt.Errorf("sysctl action requires 'name' parameter")
	}
	return nil
}

// Execute performs the sysctl operation
func (h *SysctlHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	result := &playbook.TaskResult{
		StartTime: time.Now(),
		Status:    playbook.TaskStatusRunning,
	}

	name, ok := params["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name parameter must be a non-empty string")
	}

	// Determine operation
	state := "present" // default
	if s, ok := params["state"].(string); ok {
		state = s
	}

	var err error
	switch state {
	case "present":
		value, hasValue := params["value"]
		if !hasValue {
			return nil, fmt.Errorf("'value' parameter required for state 'present'")
		}
		valueStr := fmt.Sprintf("%v", value)

		// Check if sysctl file should be created for persistence
		sysctl := true
		if s, ok := params["sysctl_set"].(bool); ok {
			sysctl = s
		}

		// Check if value should be reloaded immediately
		reload := true
		if r, ok := params["reload"].(bool); ok {
			reload = r
		}

		result.Changed, err = h.ensurePresent(name, valueStr, sysctl, reload, params)

	case "absent":
		result.Changed, err = h.ensureAbsent(name, params)

	default:
		return nil, fmt.Errorf("unknown state '%s'", state)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()

	if err != nil {
		result.Status = playbook.TaskStatusFailed
		result.Error = err.Error()
		return result, err
	}

	result.Status = playbook.TaskStatusCompleted
	return result, nil
}

// ensurePresent sets a sysctl value
func (h *SysctlHandler) ensurePresent(name, value string, sysctl, reload bool, params map[string]interface{}) (bool, error) {
	changed := false

	// Get current value
	currentValue, err := h.getCurrentValue(name)
	if err != nil {
		// Parameter might not exist, which is fine
		currentValue = ""
	}

	// Compare values
	if strings.TrimSpace(currentValue) != strings.TrimSpace(value) {
		// Apply immediately using sysctl command or /proc/sys
		if reload {
			err := h.applyValue(name, value)
			if err != nil {
				return false, fmt.Errorf("failed to apply sysctl value: %w", err)
			}
			changed = true
		}
	}

	// Write to sysctl.conf for persistence
	if sysctl {
		sysctlFile := "/etc/sysctl.d/99-cloudronix.conf"
		if f, ok := params["sysctl_file"].(string); ok {
			sysctlFile = f
		}

		persistChanged, err := h.persistValue(name, value, sysctlFile)
		if err != nil {
			return changed, fmt.Errorf("failed to persist sysctl value: %w", err)
		}
		if persistChanged {
			changed = true
		}
	}

	return changed, nil
}

// ensureAbsent removes a sysctl value from config
func (h *SysctlHandler) ensureAbsent(name string, params map[string]interface{}) (bool, error) {
	sysctlFile := "/etc/sysctl.d/99-cloudronix.conf"
	if f, ok := params["sysctl_file"].(string); ok {
		sysctlFile = f
	}

	// Read existing file
	content, err := os.ReadFile(sysctlFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // File doesn't exist, nothing to remove
		}
		return false, err
	}

	// Remove the line with this parameter
	lines := strings.Split(string(content), "\n")
	var newLines []string
	found := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, name+" ") || strings.HasPrefix(trimmed, name+"=") {
			found = true
			continue
		}
		newLines = append(newLines, line)
	}

	if !found {
		return false, nil // Parameter not in file
	}

	// Write back
	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(sysctlFile, []byte(newContent), 0644); err != nil {
		return false, fmt.Errorf("failed to update sysctl file: %w", err)
	}

	return true, nil
}

// getCurrentValue reads the current sysctl value
func (h *SysctlHandler) getCurrentValue(name string) (string, error) {
	// Try /proc/sys first
	procPath := "/proc/sys/" + strings.ReplaceAll(name, ".", "/")
	content, err := os.ReadFile(procPath)
	if err == nil {
		return strings.TrimSpace(string(content)), nil
	}

	// Fall back to sysctl command
	cmd := exec.Command("sysctl", "-n", name)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// applyValue sets the sysctl value immediately
func (h *SysctlHandler) applyValue(name, value string) error {
	// Try /proc/sys first
	procPath := "/proc/sys/" + strings.ReplaceAll(name, ".", "/")
	err := os.WriteFile(procPath, []byte(value), 0644)
	if err == nil {
		return nil
	}

	// Fall back to sysctl command
	cmd := exec.Command("sysctl", "-w", fmt.Sprintf("%s=%s", name, value))
	return cmd.Run()
}

// persistValue writes the sysctl value to a config file
func (h *SysctlHandler) persistValue(name, value, sysctlFile string) (bool, error) {
	// Ensure directory exists
	dir := filepath.Dir(sysctlFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, err
	}

	// Read existing file
	var content string
	existingContent, err := os.ReadFile(sysctlFile)
	if err == nil {
		content = string(existingContent)
	}

	// Check if parameter already exists with same value
	targetLine := fmt.Sprintf("%s = %s", name, value)
	lines := strings.Split(content, "\n")
	found := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, name+" ") || strings.HasPrefix(trimmed, name+"=") {
			// Check if value is the same
			if strings.Contains(trimmed, value) {
				return false, nil // Already set to correct value
			}
			// Update the line
			lines[i] = targetLine
			found = true
			break
		}
	}

	if !found {
		// Add new line
		if content != "" && !strings.HasSuffix(content, "\n") {
			lines = append(lines, "")
		}
		lines = append(lines, targetLine)
	}

	// Write back
	newContent := strings.Join(lines, "\n")
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	if err := os.WriteFile(sysctlFile, []byte(newContent), 0644); err != nil {
		return false, err
	}

	return true, nil
}
