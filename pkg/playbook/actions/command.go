package actions

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/cloudronix/agent/pkg/playbook"
)

// CommandHandler executes shell commands
type CommandHandler struct{}

// NewCommandHandler creates a new command handler
func NewCommandHandler() *CommandHandler {
	return &CommandHandler{}
}

// Supports returns all platforms
func (h *CommandHandler) Supports() []string {
	return []string{"all"}
}

// Validate checks if the params are valid
func (h *CommandHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["command"]; !ok {
		return fmt.Errorf("command action requires 'command' parameter")
	}
	return nil
}

// Execute runs the command
func (h *CommandHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	result := &playbook.TaskResult{
		StartTime: time.Now(),
		Status:    playbook.TaskStatusRunning,
	}

	// Get command string
	cmdStr, ok := params["command"].(string)
	if !ok || cmdStr == "" {
		return nil, fmt.Errorf("command parameter must be a non-empty string")
	}

	// Get optional parameters
	var workDir string
	if wd, ok := params["chdir"].(string); ok {
		workDir = wd
	}

	var shell string
	var shellArgs []string
	if s, ok := params["shell"].(string); ok {
		shell = s
	}

	// Get timeout (default 5 minutes)
	timeout := 5 * time.Minute
	if t, ok := params["timeout"].(int); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	} else if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	// Set up shell based on platform
	if shell == "" {
		switch runtime.GOOS {
		case "windows":
			shell = "cmd"
			shellArgs = []string{"/C"}
		default: // linux, darwin, etc.
			shell = "/bin/sh"
			shellArgs = []string{"-c"}
		}
	} else {
		// Custom shell specified
		switch shell {
		case "powershell", "pwsh":
			shell = "powershell"
			shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command"}
		case "bash":
			shell = "/bin/bash"
			shellArgs = []string{"-c"}
		case "cmd":
			shell = "cmd"
			shellArgs = []string{"/C"}
		default:
			shellArgs = []string{"-c"}
		}
	}

	// Build command
	cmdArgs := append(shellArgs, cmdStr)
	cmd := exec.CommandContext(ctx, shell, cmdArgs...)

	if workDir != "" {
		cmd.Dir = workDir
	}

	// Set up output capture
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set up environment
	if envMap, ok := params["environment"].(map[string]interface{}); ok {
		for key, val := range envMap {
			if strVal, ok := val.(string); ok {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, strVal))
			}
		}
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd = exec.CommandContext(timeoutCtx, shell, cmdArgs...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Execute
	err := cmd.Run()

	result.Stdout = strings.TrimSpace(stdout.String())
	result.Stderr = strings.TrimSpace(stderr.String())
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}

		// Check if it was a timeout
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return result, fmt.Errorf("command timed out after %v", timeout)
		}

		// Check if caller wants to fail on non-zero exit
		if failOnError, ok := params["fail_on_error"].(bool); ok && !failOnError {
			// Don't treat non-zero exit as error
			result.Status = playbook.TaskStatusCompleted
			result.Changed = true
			return result, nil
		}

		return result, fmt.Errorf("command failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	result.ExitCode = 0
	result.Status = playbook.TaskStatusCompleted
	result.Changed = true // Commands are assumed to make changes

	// Check creates/removes for idempotency
	if creates, ok := params["creates"].(string); ok && creates != "" {
		// If the file exists, the command was already run
		if fileExists(creates) {
			result.Changed = false
			result.Message = fmt.Sprintf("Skipped: '%s' already exists", creates)
		}
	}

	return result, nil
}

// fileExists checks if a file or directory exists
func fileExists(path string) bool {
	_, err := exec.Command("test", "-e", path).Output()
	return err == nil
}
