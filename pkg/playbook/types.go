// Package playbook implements a secure, cross-platform playbook execution engine.
// Playbooks are YAML-based declarative configurations that describe system changes.
//
// SECURITY: All playbooks MUST be cryptographically verified before execution.
// The verification chain is: SHA256 hash → Ed25519 signature → Approval status.
// Any verification failure results in immediate rejection - NO EXCEPTIONS.
package playbook

import (
	"time"
)

// Version of the playbook schema supported by this agent
const SchemaVersion = "1.0"

// Playbook represents a complete playbook definition
type Playbook struct {
	// Schema version for compatibility checking
	Version string `yaml:"version"`

	// Metadata
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Author      string `yaml:"author,omitempty"`

	// Targeting
	Platforms       []string `yaml:"platforms"`                  // windows, linux, darwin, android
	MinAgentVersion string   `yaml:"min_agent_version,omitempty"` // Minimum agent version required

	// Execution hints
	RequiresReboot bool `yaml:"requires_reboot,omitempty"`
	RequiresAdmin  bool `yaml:"requires_admin,omitempty"`

	// Variables defined in the playbook
	Variables map[string]string `yaml:"variables,omitempty"`

	// Tasks to execute in order
	Tasks []Task `yaml:"tasks"`

	// Handlers - triggered by notify, run once at end
	Handlers []Task `yaml:"handlers,omitempty"`

	// Error handling
	OnError *ErrorHandler `yaml:"on_error,omitempty"`

	// Post-execution
	OnComplete *CompletionHandler `yaml:"on_complete,omitempty"`
}

// SignedPlaybook wraps a playbook with its security metadata
// CRITICAL: This structure is used for verification before execution
type SignedPlaybook struct {
	// The raw YAML content (used for hash calculation)
	Content string `json:"content"`

	// Security fields - ALL must be verified
	SHA256Hash string `json:"sha256_hash"` // Hex-encoded SHA256 of Content
	Signature  []byte `json:"signature"`   // Ed25519 signature of the hash
	Status     string `json:"status"`      // Must be "approved"

	// Metadata from server
	PlaybookID string    `json:"playbook_id"`
	ApprovedBy string    `json:"approved_by,omitempty"`
	ApprovedAt time.Time `json:"approved_at,omitempty"`

	// Parsed playbook (populated after verification)
	Playbook *Playbook `json:"-"`
}

// ResultDefinition defines how a task's output should be displayed in results UI
type ResultDefinition struct {
	// Label is the display name shown in results (e.g., "Firewall Status")
	Label string `yaml:"label" json:"label"`

	// Type determines how the value is displayed: text, boolean, table, list, json
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Extract is an optional regex/jq pattern to extract specific data from stdout
	Extract string `yaml:"extract,omitempty" json:"extract,omitempty"`
}

// Task represents a single action to execute
type Task struct {
	// Identification
	Name string `yaml:"name"`
	ID   string `yaml:"id,omitempty"` // For referencing in conditions

	// Platform filter - only run on these platforms
	Platform string `yaml:"platform,omitempty"` // Single platform or empty for all

	// Conditional execution
	When string `yaml:"when,omitempty"` // Condition expression

	// The action to perform
	Action string                 `yaml:"action"` // command, file, registry, sysctl, etc.
	Params map[string]interface{} `yaml:"params"` // Action-specific parameters

	// Output capture
	Register string `yaml:"register,omitempty"` // Variable name to store result

	// Result definition - how to display this task's output in results UI
	Result *ResultDefinition `yaml:"result,omitempty"`

	// Error handling
	IgnoreErrors bool `yaml:"ignore_errors,omitempty"`
	Retries      int  `yaml:"retries,omitempty"`
	RetryDelay   int  `yaml:"retry_delay,omitempty"` // Seconds

	// Handler notification
	Notify []string `yaml:"notify,omitempty"` // Handler names to trigger

	// Rollback on failure
	Rollback *Task `yaml:"rollback,omitempty"`
}

// TaskResult holds the outcome of a task execution
type TaskResult struct {
	// Task identification
	TaskName string `json:"task_name"`
	TaskID   string `json:"task_id,omitempty"`

	// Execution status
	Status  TaskStatus `json:"status"`
	Changed bool       `json:"changed"` // Did the task make changes?

	// Output from command actions
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`

	// Error information
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`

	// Result metadata for UI display (populated from task.Result if defined)
	ResultMeta *ResultDefinition `json:"result_meta,omitempty"`

	// Timing
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  string    `json:"duration"` // String like "1.5s", not time.Duration
}

// TaskStatus represents the execution status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusSkipped   TaskStatus = "skipped"
)

// ErrorHandler defines how to handle playbook errors
type ErrorHandler struct {
	Strategy     string `yaml:"strategy"`      // stop, continue, rollback
	NotifyServer bool   `yaml:"notify_server"` // Report failure to server
	Message      string `yaml:"message"`       // Custom error message
}

// CompletionHandler defines post-execution behavior
type CompletionHandler struct {
	RebootPrompt bool   `yaml:"reboot_prompt"` // Prompt user to reboot
	Message      string `yaml:"message"`       // Message to display
}

// ExecutionReport is the full report sent back to the server
type ExecutionReport struct {
	// Playbook identification
	PlaybookID   string `json:"playbook_id"`
	PlaybookName string `json:"playbook_name"`

	// Device identification
	DeviceID string `json:"device_id"`

	// Security verification record - CRITICAL for audit
	Verification VerificationRecord `json:"verification"`

	// Execution summary
	Status         string    `json:"status"` // completed, failed, rejected
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	TotalDuration  string    `json:"total_duration"`
	TasksTotal     int       `json:"tasks_total"`
	TasksCompleted int       `json:"tasks_completed"`
	TasksFailed    int       `json:"tasks_failed"`
	TasksSkipped   int       `json:"tasks_skipped"`

	// Detailed results
	TaskResults []TaskResult `json:"task_results"`

	// Error information (if failed)
	ErrorMessage string `json:"error_message,omitempty"`

	// Post-execution
	RebootRequired bool `json:"reboot_required"`
}

// VerificationRecord documents the security checks performed
// CRITICAL: This proves the playbook was verified before execution
type VerificationRecord struct {
	// Hash verification
	ExpectedHash   string `json:"expected_hash"`
	CalculatedHash string `json:"calculated_hash"`
	HashVerified   bool   `json:"hash_verified"`

	// Signature verification
	SignatureVerified bool `json:"signature_verified"`

	// Approval status
	ApprovalStatus   string `json:"approval_status"`
	ApprovalVerified bool   `json:"approval_verified"`

	// Overall result
	AllChecksPass bool      `json:"all_checks_pass"`
	VerifiedAt    time.Time `json:"verified_at"`

	// If verification failed, why
	FailureReason string `json:"failure_reason,omitempty"`
}

// Action types supported by the playbook engine
const (
	ActionCommand    = "command"    // Execute shell command
	ActionFile       = "file"       // File operations
	ActionLineinfile = "lineinfile" // Modify lines in file
	ActionEnv        = "env"        // Environment variables
	ActionService    = "service"    // Service management
	ActionRegistry   = "registry"   // Windows registry (Windows only)
	ActionSysctl     = "sysctl"     // Kernel parameters (Linux only)
	ActionDefaults   = "defaults"   // macOS defaults (macOS only)
	ActionSettings   = "settings"   // Android settings (Android only)
	ActionPackage    = "package"    // Package management (Android only)
)

// Platforms supported
const (
	PlatformWindows = "windows"
	PlatformLinux   = "linux"
	PlatformDarwin  = "darwin"
	PlatformAndroid = "android"
)

// Playbook statuses
const (
	StatusPending    = "pending"
	StatusApproved   = "approved"
	StatusRejected   = "rejected"
	StatusDeprecated = "deprecated"
	StatusTest       = "test" // For test runs authorized by admin/developer
)
