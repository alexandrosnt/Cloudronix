package actions

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/cloudronix/agent/pkg/playbook"
)

// ServiceHandler manages system services
type ServiceHandler struct{}

// NewServiceHandler creates a new service handler
func NewServiceHandler() *ServiceHandler {
	return &ServiceHandler{}
}

// Supports returns all desktop platforms
func (h *ServiceHandler) Supports() []string {
	return []string{"windows", "linux", "darwin"}
}

// Validate checks if the params are valid
func (h *ServiceHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["name"]; !ok {
		return fmt.Errorf("service action requires 'name' parameter")
	}
	return nil
}

// Execute performs the service operation
func (h *ServiceHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	result := &playbook.TaskResult{
		StartTime: time.Now(),
		Status:    playbook.TaskStatusRunning,
	}

	name, ok := params["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name parameter must be a non-empty string")
	}

	// Determine operation
	state := "" // default is to not change state, just check enabled
	if s, ok := params["state"].(string); ok {
		state = s
	}

	enabled := ""
	if e, ok := params["enabled"].(bool); ok {
		if e {
			enabled = "yes"
		} else {
			enabled = "no"
		}
	} else if e, ok := params["enabled"].(string); ok {
		enabled = e
	}

	var err error

	// Handle state changes
	if state != "" {
		switch state {
		case "started":
			result.Changed, err = h.ensureStarted(name)
		case "stopped":
			result.Changed, err = h.ensureStopped(name)
		case "restarted":
			result.Changed, err = h.restart(name)
		case "reloaded":
			result.Changed, err = h.reload(name)
		default:
			return nil, fmt.Errorf("unknown state '%s'", state)
		}

		if err != nil {
			result.Status = playbook.TaskStatusFailed
			result.Error = err.Error()
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime).String()
			return result, err
		}
	}

	// Handle enabled changes
	if enabled != "" {
		enableChanged, err := h.setEnabled(name, enabled == "yes")
		if err != nil {
			result.Status = playbook.TaskStatusFailed
			result.Error = err.Error()
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime).String()
			return result, err
		}
		if enableChanged {
			result.Changed = true
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()
	result.Status = playbook.TaskStatusCompleted
	return result, nil
}

// ensureStarted starts a service if not running
func (h *ServiceHandler) ensureStarted(name string) (bool, error) {
	running, err := h.isRunning(name)
	if err != nil {
		return false, err
	}
	if running {
		return false, nil // Already running
	}

	return true, h.start(name)
}

// ensureStopped stops a service if running
func (h *ServiceHandler) ensureStopped(name string) (bool, error) {
	running, err := h.isRunning(name)
	if err != nil {
		return false, err
	}
	if !running {
		return false, nil // Already stopped
	}

	return true, h.stop(name)
}

// isRunning checks if a service is running
func (h *ServiceHandler) isRunning(name string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("sc", "query", name)
		output, err := cmd.Output()
		if err != nil {
			return false, nil // Service might not exist
		}
		return strings.Contains(string(output), "RUNNING"), nil

	case "linux":
		// Try systemctl first
		cmd := exec.Command("systemctl", "is-active", "--quiet", name)
		err := cmd.Run()
		if err == nil {
			return true, nil
		}
		// Try service command as fallback
		cmd = exec.Command("service", name, "status")
		output, err := cmd.Output()
		if err != nil {
			return false, nil
		}
		return strings.Contains(string(output), "running"), nil

	case "darwin":
		// Try launchctl
		cmd := exec.Command("launchctl", "list", name)
		err := cmd.Run()
		return err == nil, nil

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// start starts a service
func (h *ServiceHandler) start(name string) error {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("sc", "start", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to start service: %v - %s", err, string(output))
		}
		return nil

	case "linux":
		// Try systemctl first
		cmd := exec.Command("systemctl", "start", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Try service command as fallback
			cmd = exec.Command("service", name, "start")
			output, err = cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to start service: %v - %s", err, string(output))
			}
		}
		return nil

	case "darwin":
		cmd := exec.Command("launchctl", "start", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to start service: %v - %s", err, string(output))
		}
		return nil

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// stop stops a service
func (h *ServiceHandler) stop(name string) error {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("sc", "stop", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to stop service: %v - %s", err, string(output))
		}
		return nil

	case "linux":
		cmd := exec.Command("systemctl", "stop", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			cmd = exec.Command("service", name, "stop")
			output, err = cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to stop service: %v - %s", err, string(output))
			}
		}
		return nil

	case "darwin":
		cmd := exec.Command("launchctl", "stop", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to stop service: %v - %s", err, string(output))
		}
		return nil

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// restart restarts a service
func (h *ServiceHandler) restart(name string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		// Stop then start
		exec.Command("sc", "stop", name).Run()
		time.Sleep(2 * time.Second)
		cmd := exec.Command("sc", "start", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to restart service: %v - %s", err, string(output))
		}
		return true, nil

	case "linux":
		cmd := exec.Command("systemctl", "restart", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			cmd = exec.Command("service", name, "restart")
			output, err = cmd.CombinedOutput()
			if err != nil {
				return false, fmt.Errorf("failed to restart service: %v - %s", err, string(output))
			}
		}
		return true, nil

	case "darwin":
		exec.Command("launchctl", "stop", name).Run()
		time.Sleep(1 * time.Second)
		cmd := exec.Command("launchctl", "start", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to restart service: %v - %s", err, string(output))
		}
		return true, nil

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// reload reloads a service configuration
func (h *ServiceHandler) reload(name string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		// Windows doesn't have a reload concept, so we restart
		return h.restart(name)

	case "linux":
		cmd := exec.Command("systemctl", "reload", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Fallback to restart if reload not supported
			return h.restart(name)
		}
		_ = output
		return true, nil

	case "darwin":
		// macOS launchctl doesn't have reload, use kickstart
		cmd := exec.Command("launchctl", "kickstart", "-k", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return h.restart(name)
		}
		_ = output
		return true, nil

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// setEnabled enables or disables a service at boot
func (h *ServiceHandler) setEnabled(name string, enabled bool) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		startType := "auto"
		if !enabled {
			startType = "disabled"
		}
		cmd := exec.Command("sc", "config", name, "start=", startType)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to set service enabled: %v - %s", err, string(output))
		}
		return true, nil

	case "linux":
		action := "enable"
		if !enabled {
			action = "disable"
		}
		cmd := exec.Command("systemctl", action, name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to %s service: %v - %s", action, err, string(output))
		}
		return true, nil

	case "darwin":
		// macOS uses launchctl load/unload
		action := "load"
		flag := "-w"
		if !enabled {
			action = "unload"
		}
		// This is simplified - real implementation would need to find the plist path
		cmd := exec.Command("launchctl", action, flag, name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to %s service: %v - %s", action, err, string(output))
		}
		return true, nil

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
