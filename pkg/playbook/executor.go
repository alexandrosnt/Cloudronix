package playbook

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"runtime"
	"time"
)

// Executor manages the execution of verified playbooks
//
// SECURITY: The executor WILL NOT run any playbook that has not passed
// all verification checks. This is enforced at the architecture level.
type Executor struct {
	// Verifier for security checks
	verifier *Verifier

	// Parser for YAML processing
	parser *Parser

	// Action handlers by type
	handlers map[string]ActionHandler

	// Current platform
	platform string

	// Device ID for reporting
	deviceID string

	// Callback for progress reporting
	onProgress func(taskName string, status TaskStatus)
}

// ActionHandler is the interface for action implementations
type ActionHandler interface {
	// Execute performs the action and returns the result
	Execute(ctx context.Context, params map[string]interface{}, vars *Variables) (*TaskResult, error)

	// Supports returns the list of platforms this handler supports
	Supports() []string

	// Validate checks if the params are valid (called during parsing)
	Validate(params map[string]interface{}) error
}

// ExecutorConfig holds configuration for the executor
type ExecutorConfig struct {
	// ServerPublicKey for signature verification (required)
	ServerPublicKey ed25519.PublicKey

	// DeviceID for execution reports
	DeviceID string

	// OnProgress callback for progress updates
	OnProgress func(taskName string, status TaskStatus)
}

// NewExecutor creates a new playbook executor
//
// SECURITY: The server public key is required and must be obtained during
// device enrollment. It should be stored securely and not fetched at runtime.
func NewExecutor(config ExecutorConfig) (*Executor, error) {
	verifier, err := NewVerifier(config.ServerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create verifier: %w", err)
	}

	e := &Executor{
		verifier:   verifier,
		parser:     NewParser(),
		handlers:   make(map[string]ActionHandler),
		platform:   runtime.GOOS,
		deviceID:   config.DeviceID,
		onProgress: config.OnProgress,
	}

	return e, nil
}

// RegisterHandler registers an action handler
func (e *Executor) RegisterHandler(actionType string, handler ActionHandler) {
	e.handlers[actionType] = handler
}

// Execute runs a signed playbook after verification
//
// SECURITY CRITICAL: This is the main entry point for playbook execution.
// The verification chain is enforced here - no shortcuts, no bypasses.
//
// Returns an ExecutionReport for audit purposes, even on failure.
func (e *Executor) Execute(ctx context.Context, sp *SignedPlaybook) (*ExecutionReport, error) {
	report := &ExecutionReport{
		PlaybookID: sp.PlaybookID,
		DeviceID:   e.deviceID,
		StartTime:  time.Now(),
		Status:     "pending",
	}

	// =========================================================================
	// STEP 1: SECURITY VERIFICATION (MANDATORY)
	// =========================================================================
	verificationRecord, verifyErr := e.verifier.Verify(sp)
	report.Verification = *verificationRecord

	if verifyErr != nil {
		// SECURITY VIOLATION - playbook rejected
		report.Status = "rejected"
		report.EndTime = time.Now()
		report.TotalDuration = report.EndTime.Sub(report.StartTime).String()
		report.ErrorMessage = fmt.Sprintf("SECURITY: %v", verifyErr)
		return report, verifyErr
	}

	// =========================================================================
	// STEP 2: PARSE THE VERIFIED CONTENT
	// =========================================================================
	playbook, parseErr := e.parser.Parse(sp.Content)
	if parseErr != nil {
		report.Status = "failed"
		report.EndTime = time.Now()
		report.TotalDuration = report.EndTime.Sub(report.StartTime).String()
		report.ErrorMessage = fmt.Sprintf("Parse error: %v", parseErr)
		return report, parseErr
	}

	sp.Playbook = playbook
	report.PlaybookName = playbook.Name

	// =========================================================================
	// STEP 3: PLATFORM COMPATIBILITY CHECK
	// =========================================================================
	if len(playbook.Platforms) > 0 {
		compatible := false
		for _, p := range playbook.Platforms {
			if p == e.platform {
				compatible = true
				break
			}
		}
		if !compatible {
			report.Status = "rejected"
			report.EndTime = time.Now()
			report.TotalDuration = report.EndTime.Sub(report.StartTime).String()
			report.ErrorMessage = fmt.Sprintf("Platform '%s' not supported by this playbook", e.platform)
			return report, ErrPlatformMismatch
		}
	}

	// =========================================================================
	// STEP 4: EXECUTE TASKS
	// =========================================================================
	report.Status = "running"
	report.TasksTotal = len(playbook.Tasks)

	vars := NewVariables()
	vars.SetUserVars(playbook.Variables)

	// Track which handlers to notify
	notifiedHandlers := make(map[string]bool)

	for _, task := range playbook.Tasks {
		select {
		case <-ctx.Done():
			report.Status = "cancelled"
			report.EndTime = time.Now()
			report.TotalDuration = report.EndTime.Sub(report.StartTime).String()
			return report, ctx.Err()
		default:
		}

		result := e.executeTask(ctx, &task, vars)
		report.TaskResults = append(report.TaskResults, *result)

		switch result.Status {
		case TaskStatusCompleted:
			report.TasksCompleted++
			// Track notified handlers
			for _, handlerName := range task.Notify {
				if result.Changed {
					notifiedHandlers[handlerName] = true
				}
			}
		case TaskStatusFailed:
			report.TasksFailed++
			if !task.IgnoreErrors {
				// Stop execution on failure (unless error handling says otherwise)
				if playbook.OnError == nil || playbook.OnError.Strategy == "stop" {
					report.Status = "failed"
					report.EndTime = time.Now()
					report.TotalDuration = report.EndTime.Sub(report.StartTime).String()
					report.ErrorMessage = result.Error
					return report, fmt.Errorf("task '%s' failed: %s", task.Name, result.Error)
				}
			}
		case TaskStatusSkipped:
			report.TasksSkipped++
		}

		// Store result for variable reference if registered
		if task.Register != "" {
			vars.SetTaskResult(task.Register, result)
		}
	}

	// =========================================================================
	// STEP 5: RUN NOTIFIED HANDLERS
	// =========================================================================
	for _, handler := range playbook.Handlers {
		if notifiedHandlers[handler.Name] {
			result := e.executeTask(ctx, &handler, vars)
			report.TaskResults = append(report.TaskResults, *result)

			if result.Status == TaskStatusFailed && !handler.IgnoreErrors {
				report.TasksFailed++
			}
		}
	}

	// =========================================================================
	// STEP 6: COMPLETE
	// =========================================================================
	report.Status = "completed"
	report.EndTime = time.Now()
	report.TotalDuration = report.EndTime.Sub(report.StartTime).String()
	report.RebootRequired = playbook.RequiresReboot

	return report, nil
}

// executeTask executes a single task with retry logic
func (e *Executor) executeTask(ctx context.Context, task *Task, vars *Variables) *TaskResult {
	result := &TaskResult{
		TaskName:   task.Name,
		TaskID:     task.ID,
		StartTime:  time.Now(),
		Status:     TaskStatusPending,
		ResultMeta: task.Result, // Copy result definition for UI display
	}

	// Report progress
	if e.onProgress != nil {
		e.onProgress(task.Name, TaskStatusRunning)
	}

	// Check platform filter
	if task.Platform != "" && task.Platform != e.platform {
		result.Status = TaskStatusSkipped
		result.Message = fmt.Sprintf("Skipped: platform filter '%s' doesn't match '%s'", task.Platform, e.platform)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime).String()
		return result
	}

	// Evaluate condition
	if task.When != "" {
		condition := NewCondition(vars)
		condResult, err := condition.Evaluate(task.When)
		if err != nil {
			result.Status = TaskStatusFailed
			result.Error = fmt.Sprintf("condition evaluation failed: %v", err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime).String()
			return result
		}
		if !condResult {
			result.Status = TaskStatusSkipped
			result.Message = fmt.Sprintf("Skipped: condition '%s' evaluated to false", task.When)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime).String()
			return result
		}
	}

	// Get the handler
	handler, ok := e.handlers[task.Action]
	if !ok {
		result.Status = TaskStatusFailed
		result.Error = fmt.Sprintf("no handler registered for action '%s'", task.Action)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime).String()
		return result
	}

	// Check platform support
	supported := false
	for _, p := range handler.Supports() {
		if p == e.platform || p == "all" {
			supported = true
			break
		}
	}
	if !supported {
		result.Status = TaskStatusFailed
		result.Error = fmt.Sprintf("action '%s' does not support platform '%s'", task.Action, e.platform)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime).String()
		return result
	}

	// Substitute variables in params
	params, err := vars.SubstituteMap(task.Params)
	if err != nil {
		result.Status = TaskStatusFailed
		result.Error = fmt.Sprintf("variable substitution failed: %v", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime).String()
		return result
	}

	// Execute with retries
	maxAttempts := task.Retries + 1
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result.Status = TaskStatusRunning

		execResult, execErr := handler.Execute(ctx, params, vars)
		if execErr == nil && execResult != nil {
			// Success
			result.Status = TaskStatusCompleted
			result.Changed = execResult.Changed
			result.Stdout = execResult.Stdout
			result.Stderr = execResult.Stderr
			result.ExitCode = execResult.ExitCode
			result.Message = execResult.Message
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime).String()

			if e.onProgress != nil {
				e.onProgress(task.Name, TaskStatusCompleted)
			}
			return result
		}

		lastErr = execErr
		if execResult != nil {
			result.Stdout = execResult.Stdout
			result.Stderr = execResult.Stderr
			result.ExitCode = execResult.ExitCode
		}

		// Retry delay
		if attempt < maxAttempts && task.RetryDelay > 0 {
			select {
			case <-ctx.Done():
				result.Status = TaskStatusFailed
				result.Error = "cancelled during retry delay"
				result.EndTime = time.Now()
				result.Duration = result.EndTime.Sub(result.StartTime).String()
				return result
			case <-time.After(time.Duration(task.RetryDelay) * time.Second):
				// Continue to next attempt
			}
		}
	}

	// All retries exhausted
	result.Status = TaskStatusFailed
	if lastErr != nil {
		result.Error = lastErr.Error()
	} else {
		result.Error = "task failed after all retries"
	}
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()

	// Execute rollback if defined
	if task.Rollback != nil {
		rollbackResult := e.executeTask(ctx, task.Rollback, vars)
		if rollbackResult.Status == TaskStatusFailed {
			result.Error = fmt.Sprintf("%s (rollback also failed: %s)", result.Error, rollbackResult.Error)
		} else {
			result.Message = "Rollback executed successfully"
		}
	}

	if e.onProgress != nil {
		e.onProgress(task.Name, TaskStatusFailed)
	}

	return result
}

// DryRun validates and simulates playbook execution without making changes
//
// SECURITY: Even dry runs require full verification - we don't want to expose
// playbook structure to unverified content.
func (e *Executor) DryRun(ctx context.Context, sp *SignedPlaybook) (*ExecutionReport, error) {
	report := &ExecutionReport{
		PlaybookID: sp.PlaybookID,
		DeviceID:   e.deviceID,
		StartTime:  time.Now(),
		Status:     "dry_run",
	}

	// Still verify!
	verificationRecord, verifyErr := e.verifier.Verify(sp)
	report.Verification = *verificationRecord

	if verifyErr != nil {
		report.Status = "rejected"
		report.EndTime = time.Now()
		report.TotalDuration = report.EndTime.Sub(report.StartTime).String()
		report.ErrorMessage = fmt.Sprintf("SECURITY: %v", verifyErr)
		return report, verifyErr
	}

	// Parse
	playbook, parseErr := e.parser.Parse(sp.Content)
	if parseErr != nil {
		report.Status = "failed"
		report.EndTime = time.Now()
		report.TotalDuration = report.EndTime.Sub(report.StartTime).String()
		report.ErrorMessage = fmt.Sprintf("Parse error: %v", parseErr)
		return report, parseErr
	}

	report.PlaybookName = playbook.Name
	report.TasksTotal = len(playbook.Tasks)

	// Simulate each task
	vars := NewVariables()
	vars.SetUserVars(playbook.Variables)

	for _, task := range playbook.Tasks {
		simResult := &TaskResult{
			TaskName:  task.Name,
			TaskID:    task.ID,
			StartTime: time.Now(),
			Status:    TaskStatusPending,
		}

		// Check platform filter
		if task.Platform != "" && task.Platform != e.platform {
			simResult.Status = TaskStatusSkipped
			simResult.Message = "Would skip: platform filter"
		} else if task.When != "" {
			// We can't fully evaluate conditions in dry run, but we can validate syntax
			if err := ValidateCondition(task.When); err != nil {
				simResult.Status = TaskStatusFailed
				simResult.Error = fmt.Sprintf("Invalid condition: %v", err)
				report.TasksFailed++
			} else {
				simResult.Status = TaskStatusPending
				simResult.Message = fmt.Sprintf("Would execute if condition '%s' is true", task.When)
			}
		} else {
			simResult.Status = TaskStatusPending
			simResult.Message = "Would execute"
		}

		// Validate handler exists
		if _, ok := e.handlers[task.Action]; !ok {
			simResult.Status = TaskStatusFailed
			simResult.Error = fmt.Sprintf("No handler for action '%s'", task.Action)
			report.TasksFailed++
		}

		simResult.EndTime = time.Now()
		simResult.Duration = simResult.EndTime.Sub(simResult.StartTime).String()
		report.TaskResults = append(report.TaskResults, *simResult)
	}

	report.EndTime = time.Now()
	report.TotalDuration = report.EndTime.Sub(report.StartTime).String()

	if report.TasksFailed > 0 {
		report.Status = "dry_run_failed"
		return report, fmt.Errorf("dry run found %d issues", report.TasksFailed)
	}

	report.Status = "dry_run_ok"
	return report, nil
}
