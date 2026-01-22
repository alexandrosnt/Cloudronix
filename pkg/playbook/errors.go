package playbook

import (
	"errors"
	"fmt"
)

// Parser errors
var (
	ErrInvalidYAML        = errors.New("invalid YAML syntax")
	ErrUnsupportedVersion = errors.New("unsupported playbook schema version")
	ErrMissingName        = errors.New("playbook name is required")
	ErrMissingTasks       = errors.New("playbook must have at least one task")
	ErrInvalidPlatform    = errors.New("invalid platform specified")
	ErrMissingAction      = errors.New("task action is required")
	ErrInvalidAction      = errors.New("unknown action type")
)

// Execution errors
var (
	ErrPlatformMismatch    = errors.New("playbook does not support this platform")
	ErrAgentVersionTooLow  = errors.New("agent version is too low for this playbook")
	ErrConditionFailed     = errors.New("condition evaluation failed")
	ErrActionFailed        = errors.New("action execution failed")
	ErrVariableNotFound    = errors.New("variable not found")
	ErrInvalidVariableName = errors.New("invalid variable name")
)

// ParseError wraps parsing errors with context
type ParseError struct {
	Line    int
	Column  int
	Message string
	Cause   error
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("parse error at line %d, column %d: %s", e.Line, e.Column, e.Message)
	}
	return fmt.Sprintf("parse error: %s", e.Message)
}

func (e *ParseError) Unwrap() error {
	return e.Cause
}

// ValidationError represents a playbook validation failure
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error in '%s': %s", e.Field, e.Message)
}

// TaskError wraps errors that occur during task execution
type TaskError struct {
	TaskName string
	TaskID   string
	Action   string
	Cause    error
}

func (e *TaskError) Error() string {
	if e.TaskID != "" {
		return fmt.Sprintf("task '%s' (id=%s, action=%s) failed: %v", e.TaskName, e.TaskID, e.Action, e.Cause)
	}
	return fmt.Sprintf("task '%s' (action=%s) failed: %v", e.TaskName, e.Action, e.Cause)
}

func (e *TaskError) Unwrap() error {
	return e.Cause
}

// ConditionError represents an error in condition evaluation
type ConditionError struct {
	Expression string
	Cause      error
}

func (e *ConditionError) Error() string {
	return fmt.Sprintf("condition '%s' evaluation failed: %v", e.Expression, e.Cause)
}

func (e *ConditionError) Unwrap() error {
	return e.Cause
}

// VariableError represents an error in variable substitution
type VariableError struct {
	VariableName string
	Cause        error
}

func (e *VariableError) Error() string {
	return fmt.Sprintf("variable '{{ %s }}' error: %v", e.VariableName, e.Cause)
}

func (e *VariableError) Unwrap() error {
	return e.Cause
}
