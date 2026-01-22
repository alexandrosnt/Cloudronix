package playbook

import (
	"fmt"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parser handles playbook YAML parsing and validation
type Parser struct {
	// Current platform for validation
	platform string
}

// NewParser creates a new playbook parser for the current platform
func NewParser() *Parser {
	platform := runtime.GOOS
	// Map GOOS to our platform names
	if platform == "windows" || platform == "linux" || platform == "darwin" {
		// Direct mapping
	} else if strings.Contains(platform, "android") {
		platform = PlatformAndroid
	}
	return &Parser{platform: platform}
}

// Parse parses YAML content into a Playbook struct
//
// This performs:
//   1. YAML syntax parsing
//   2. Schema validation
//   3. Platform compatibility check
func (p *Parser) Parse(content string) (*Playbook, error) {
	var pb Playbook

	// Parse YAML
	if err := yaml.Unmarshal([]byte(content), &pb); err != nil {
		return nil, &ParseError{
			Message: fmt.Sprintf("YAML parse failed: %v", err),
			Cause:   ErrInvalidYAML,
		}
	}

	// Validate the playbook
	if err := p.Validate(&pb); err != nil {
		return nil, err
	}

	return &pb, nil
}

// Validate performs comprehensive validation on a parsed playbook
func (p *Parser) Validate(pb *Playbook) error {
	// Version check
	if pb.Version == "" {
		pb.Version = SchemaVersion // Default to current version
	}
	if !p.isSupportedVersion(pb.Version) {
		return &ValidationError{
			Field:   "version",
			Message: fmt.Sprintf("version '%s' is not supported, expected '%s'", pb.Version, SchemaVersion),
		}
	}

	// Required fields
	if pb.Name == "" {
		return &ValidationError{Field: "name", Message: "playbook name is required"}
	}

	if len(pb.Tasks) == 0 {
		return &ValidationError{Field: "tasks", Message: "playbook must have at least one task"}
	}

	// Validate platforms
	if len(pb.Platforms) > 0 {
		for _, plat := range pb.Platforms {
			if !p.isValidPlatform(plat) {
				return &ValidationError{
					Field:   "platforms",
					Message: fmt.Sprintf("invalid platform '%s'", plat),
				}
			}
		}

		// Check if current platform is supported
		if !p.isPlatformSupported(pb.Platforms) {
			return &ValidationError{
				Field:   "platforms",
				Message: fmt.Sprintf("playbook does not support platform '%s'", p.platform),
			}
		}
	}

	// Validate each task
	for i, task := range pb.Tasks {
		if err := p.validateTask(&task, i); err != nil {
			return err
		}
	}

	// Validate handlers
	for i, handler := range pb.Handlers {
		if err := p.validateTask(&handler, i); err != nil {
			return &ValidationError{
				Field:   fmt.Sprintf("handlers[%d]", i),
				Message: err.Error(),
			}
		}
	}

	return nil
}

// validateTask validates a single task definition
func (p *Parser) validateTask(task *Task, index int) error {
	fieldPrefix := fmt.Sprintf("tasks[%d]", index)

	// Task name is required
	if task.Name == "" {
		return &ValidationError{
			Field:   fieldPrefix + ".name",
			Message: "task name is required",
		}
	}

	// Action is required
	if task.Action == "" {
		return &ValidationError{
			Field:   fieldPrefix + ".action",
			Message: "task action is required",
		}
	}

	// Validate action type
	if !p.isValidAction(task.Action) {
		return &ValidationError{
			Field:   fieldPrefix + ".action",
			Message: fmt.Sprintf("unknown action '%s'", task.Action),
		}
	}

	// Validate platform-specific actions
	if err := p.validateActionPlatform(task.Action, task.Platform); err != nil {
		return &ValidationError{
			Field:   fieldPrefix + ".action",
			Message: err.Error(),
		}
	}

	// Validate params based on action type
	if err := p.validateActionParams(task.Action, task.Params, fieldPrefix); err != nil {
		return err
	}

	// Validate retries
	if task.Retries < 0 {
		return &ValidationError{
			Field:   fieldPrefix + ".retries",
			Message: "retries cannot be negative",
		}
	}

	if task.RetryDelay < 0 {
		return &ValidationError{
			Field:   fieldPrefix + ".retry_delay",
			Message: "retry_delay cannot be negative",
		}
	}

	return nil
}

// validateActionParams validates parameters for a specific action type
func (p *Parser) validateActionParams(action string, params map[string]interface{}, fieldPrefix string) error {
	switch action {
	case ActionCommand:
		// command action requires 'command' param
		if _, ok := params["command"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.command",
				Message: "command action requires 'command' parameter",
			}
		}

	case ActionFile:
		// file action requires 'path' param
		if _, ok := params["path"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.path",
				Message: "file action requires 'path' parameter",
			}
		}

	case ActionRegistry:
		// registry action requires 'path' and 'key' params
		if _, ok := params["path"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.path",
				Message: "registry action requires 'path' parameter",
			}
		}

	case ActionSysctl:
		// sysctl action requires 'name' param
		if _, ok := params["name"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.name",
				Message: "sysctl action requires 'name' parameter",
			}
		}

	case ActionDefaults:
		// defaults action requires 'domain' and 'key' params
		if _, ok := params["domain"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.domain",
				Message: "defaults action requires 'domain' parameter",
			}
		}
		if _, ok := params["key"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.key",
				Message: "defaults action requires 'key' parameter",
			}
		}

	case ActionSettings:
		// settings action requires 'namespace' and 'key' params
		if _, ok := params["namespace"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.namespace",
				Message: "settings action requires 'namespace' parameter",
			}
		}
		if _, ok := params["key"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.key",
				Message: "settings action requires 'key' parameter",
			}
		}

	case ActionEnv:
		// env action requires 'name' param
		if _, ok := params["name"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.name",
				Message: "env action requires 'name' parameter",
			}
		}

	case ActionService:
		// service action requires 'name' param
		if _, ok := params["name"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.name",
				Message: "service action requires 'name' parameter",
			}
		}

	case ActionLineinfile:
		// lineinfile action requires 'path' and 'line' params
		if _, ok := params["path"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.path",
				Message: "lineinfile action requires 'path' parameter",
			}
		}

	case ActionPackage:
		// package action requires 'name' param
		if _, ok := params["name"]; !ok {
			return &ValidationError{
				Field:   fieldPrefix + ".params.name",
				Message: "package action requires 'name' parameter",
			}
		}
	}

	return nil
}

// validateActionPlatform checks if an action is valid for the given platform
func (p *Parser) validateActionPlatform(action, taskPlatform string) error {
	// Determine the effective platform (task-specific or current)
	platform := taskPlatform
	if platform == "" {
		platform = p.platform
	}

	// Check platform-specific actions
	switch action {
	case ActionRegistry:
		if platform != PlatformWindows {
			return fmt.Errorf("registry action is only available on Windows")
		}
	case ActionSysctl:
		if platform != PlatformLinux {
			return fmt.Errorf("sysctl action is only available on Linux")
		}
	case ActionDefaults:
		if platform != PlatformDarwin {
			return fmt.Errorf("defaults action is only available on macOS")
		}
	case ActionSettings, ActionPackage:
		if platform != PlatformAndroid {
			return fmt.Errorf("%s action is only available on Android", action)
		}
	}

	return nil
}

// isSupportedVersion checks if a schema version is supported
func (p *Parser) isSupportedVersion(version string) bool {
	// For now, only support exact match
	// In future, could support semantic versioning
	return version == SchemaVersion || version == "1" || version == "1.0"
}

// isValidPlatform checks if a platform name is valid
func (p *Parser) isValidPlatform(platform string) bool {
	switch platform {
	case PlatformWindows, PlatformLinux, PlatformDarwin, PlatformAndroid:
		return true
	default:
		return false
	}
}

// isPlatformSupported checks if the current platform is in the supported list
func (p *Parser) isPlatformSupported(platforms []string) bool {
	for _, plat := range platforms {
		if plat == p.platform {
			return true
		}
	}
	return false
}

// isValidAction checks if an action type is valid
func (p *Parser) isValidAction(action string) bool {
	switch action {
	case ActionCommand, ActionFile, ActionLineinfile, ActionEnv, ActionService,
		ActionRegistry, ActionSysctl, ActionDefaults, ActionSettings, ActionPackage:
		return true
	default:
		return false
	}
}

// GetPlatform returns the current platform
func (p *Parser) GetPlatform() string {
	return p.platform
}
