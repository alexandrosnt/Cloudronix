package actions

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cloudronix/agent/pkg/playbook"
)

// EnvHandler manages environment variables
type EnvHandler struct{}

// NewEnvHandler creates a new environment handler
func NewEnvHandler() *EnvHandler {
	return &EnvHandler{}
}

// Supports returns all platforms
func (h *EnvHandler) Supports() []string {
	return []string{"all"}
}

// Validate checks if the params are valid
func (h *EnvHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["name"]; !ok {
		return fmt.Errorf("env action requires 'name' parameter")
	}
	return nil
}

// Execute performs the environment variable operation
func (h *EnvHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
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

	// Scope: process, user, or system
	scope := "user" // default
	if s, ok := params["scope"].(string); ok {
		scope = s
	}

	var err error
	switch state {
	case "present":
		value, hasValue := params["value"]
		if !hasValue {
			return nil, fmt.Errorf("'value' parameter required for state 'present'")
		}
		valueStr := fmt.Sprintf("%v", value)
		result.Changed, err = h.ensurePresent(name, valueStr, scope)

	case "absent":
		result.Changed, err = h.ensureAbsent(name, scope)

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

// ensurePresent sets an environment variable
func (h *EnvHandler) ensurePresent(name, value, scope string) (bool, error) {
	// Check current value
	currentValue := os.Getenv(name)
	if currentValue == value {
		return false, nil // Already set to correct value
	}

	switch scope {
	case "process":
		// Only set for current process
		if err := os.Setenv(name, value); err != nil {
			return false, fmt.Errorf("failed to set environment variable: %w", err)
		}
		return true, nil

	case "user":
		return h.setUserEnv(name, value)

	case "system":
		return h.setSystemEnv(name, value)

	default:
		return false, fmt.Errorf("unknown scope '%s'", scope)
	}
}

// ensureAbsent removes an environment variable
func (h *EnvHandler) ensureAbsent(name, scope string) (bool, error) {
	// Check if variable exists
	if _, exists := os.LookupEnv(name); !exists {
		return false, nil // Already absent
	}

	switch scope {
	case "process":
		if err := os.Unsetenv(name); err != nil {
			return false, fmt.Errorf("failed to unset environment variable: %w", err)
		}
		return true, nil

	case "user":
		return h.removeUserEnv(name)

	case "system":
		return h.removeSystemEnv(name)

	default:
		return false, fmt.Errorf("unknown scope '%s'", scope)
	}
}

// setUserEnv sets a user-level environment variable persistently
func (h *EnvHandler) setUserEnv(name, value string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		// Use PowerShell to set user environment variable
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(`[Environment]::SetEnvironmentVariable('%s', '%s', 'User')`, name, escapeForPowerShell(value)))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to set user environment variable: %v - %s", err, string(output))
		}
		return true, nil

	case "linux", "darwin":
		// Add to user's shell profile
		home := os.Getenv("HOME")
		if home == "" {
			return false, fmt.Errorf("HOME environment variable not set")
		}

		// Determine shell profile file
		profileFile := filepath.Join(home, ".bashrc")
		shell := os.Getenv("SHELL")
		if strings.Contains(shell, "zsh") {
			profileFile = filepath.Join(home, ".zshrc")
		} else if strings.Contains(shell, "fish") {
			profileFile = filepath.Join(home, ".config", "fish", "config.fish")
		}

		return h.addToProfile(profileFile, name, value)

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// setSystemEnv sets a system-level environment variable persistently
func (h *EnvHandler) setSystemEnv(name, value string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		// Use PowerShell to set machine environment variable (requires admin)
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(`[Environment]::SetEnvironmentVariable('%s', '%s', 'Machine')`, name, escapeForPowerShell(value)))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to set system environment variable (requires admin): %v - %s", err, string(output))
		}
		return true, nil

	case "linux":
		// Add to /etc/environment or /etc/profile.d/
		profileFile := "/etc/profile.d/cloudronix.sh"
		return h.addToProfile(profileFile, name, value)

	case "darwin":
		// Use launchctl setenv for system-wide
		cmd := exec.Command("launchctl", "setenv", name, value)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to set system environment variable: %v - %s", err, string(output))
		}
		return true, nil

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// removeUserEnv removes a user-level environment variable
func (h *EnvHandler) removeUserEnv(name string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(`[Environment]::SetEnvironmentVariable('%s', $null, 'User')`, name))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to remove user environment variable: %v - %s", err, string(output))
		}
		return true, nil

	case "linux", "darwin":
		home := os.Getenv("HOME")
		if home == "" {
			return false, fmt.Errorf("HOME environment variable not set")
		}

		profileFile := filepath.Join(home, ".bashrc")
		shell := os.Getenv("SHELL")
		if strings.Contains(shell, "zsh") {
			profileFile = filepath.Join(home, ".zshrc")
		}

		return h.removeFromProfile(profileFile, name)

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// removeSystemEnv removes a system-level environment variable
func (h *EnvHandler) removeSystemEnv(name string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(`[Environment]::SetEnvironmentVariable('%s', $null, 'Machine')`, name))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to remove system environment variable: %v - %s", err, string(output))
		}
		return true, nil

	case "linux":
		profileFile := "/etc/profile.d/cloudronix.sh"
		return h.removeFromProfile(profileFile, name)

	case "darwin":
		cmd := exec.Command("launchctl", "unsetenv", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to remove system environment variable: %v - %s", err, string(output))
		}
		return true, nil

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// addToProfile adds an export line to a shell profile file
func (h *EnvHandler) addToProfile(profileFile, name, value string) (bool, error) {
	// Ensure directory exists
	dir := filepath.Dir(profileFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("failed to create directory: %w", err)
	}

	// Read existing content
	content, err := os.ReadFile(profileFile)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	exportLine := fmt.Sprintf("export %s=\"%s\"", name, value)
	lines := strings.Split(string(content), "\n")

	// Check if already exists
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "export "+name+"=") {
			// Update existing line
			if strings.TrimSpace(line) == exportLine {
				return false, nil // Already set correctly
			}
			lines[i] = exportLine
			newContent := strings.Join(lines, "\n")
			if err := os.WriteFile(profileFile, []byte(newContent), 0644); err != nil {
				return false, fmt.Errorf("failed to write profile: %w", err)
			}
			return true, nil
		}
	}

	// Add new line
	newContent := string(content)
	if newContent != "" && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += exportLine + "\n"

	if err := os.WriteFile(profileFile, []byte(newContent), 0644); err != nil {
		return false, fmt.Errorf("failed to write profile: %w", err)
	}

	return true, nil
}

// removeFromProfile removes an export line from a shell profile file
func (h *EnvHandler) removeFromProfile(profileFile, name string) (bool, error) {
	content, err := os.ReadFile(profileFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	found := false

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "export "+name+"=") {
			found = true
			continue
		}
		newLines = append(newLines, line)
	}

	if !found {
		return false, nil
	}

	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(profileFile, []byte(newContent), 0644); err != nil {
		return false, fmt.Errorf("failed to write profile: %w", err)
	}

	return true, nil
}

// escapeForPowerShell escapes a string for use in PowerShell
func escapeForPowerShell(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	return s
}
