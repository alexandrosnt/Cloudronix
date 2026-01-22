//go:build darwin

package actions

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cloudronix/agent/pkg/playbook"
)

// DefaultsHandler manages macOS defaults (plist preferences)
type DefaultsHandler struct{}

// NewDefaultsHandler creates a new defaults handler
func NewDefaultsHandler() *DefaultsHandler {
	return &DefaultsHandler{}
}

// Supports returns macOS only
func (h *DefaultsHandler) Supports() []string {
	return []string{"darwin"}
}

// Validate checks if the params are valid
func (h *DefaultsHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["domain"]; !ok {
		return fmt.Errorf("defaults action requires 'domain' parameter")
	}
	if _, ok := params["key"]; !ok {
		return fmt.Errorf("defaults action requires 'key' parameter")
	}
	return nil
}

// Execute performs the defaults operation
func (h *DefaultsHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	result := &playbook.TaskResult{
		StartTime: time.Now(),
		Status:    playbook.TaskStatusRunning,
	}

	domain, ok := params["domain"].(string)
	if !ok || domain == "" {
		return nil, fmt.Errorf("domain parameter must be a non-empty string")
	}

	key, ok := params["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("key parameter must be a non-empty string")
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
		result.Changed, err = h.ensurePresent(domain, key, value, params)

	case "absent":
		result.Changed, err = h.ensureAbsent(domain, key)

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

// ensurePresent sets a defaults value
func (h *DefaultsHandler) ensurePresent(domain, key string, value interface{}, params map[string]interface{}) (bool, error) {
	// Get current value
	currentValue, currentType, err := h.getCurrentValue(domain, key)
	if err == nil {
		// Value exists, check if it's the same
		if h.valuesEqual(currentValue, currentType, value) {
			return false, nil // Already set correctly
		}
	}

	// Determine value type
	valueType := "string" // default
	if t, ok := params["type"].(string); ok {
		valueType = t
	} else {
		// Auto-detect type
		valueType = h.detectType(value)
	}

	// Host can be specified for global defaults
	host := ""
	if h, ok := params["host"].(string); ok {
		host = h
	}

	// Build defaults write command
	args := []string{"write"}
	if host != "" {
		args = append(args, "-host", host)
	}
	args = append(args, domain, key)

	// Add type and value
	switch valueType {
	case "string":
		args = append(args, "-string", fmt.Sprintf("%v", value))

	case "int", "integer":
		args = append(args, "-int", fmt.Sprintf("%v", value))

	case "float":
		args = append(args, "-float", fmt.Sprintf("%v", value))

	case "bool", "boolean":
		boolVal := false
		switch v := value.(type) {
		case bool:
			boolVal = v
		case string:
			boolVal = strings.ToLower(v) == "true" || v == "1" || v == "yes"
		case int:
			boolVal = v != 0
		case float64:
			boolVal = v != 0
		}
		args = append(args, "-bool", strconv.FormatBool(boolVal))

	case "data":
		// Data should be hex-encoded
		args = append(args, "-data", fmt.Sprintf("%v", value))

	case "date":
		args = append(args, "-date", fmt.Sprintf("%v", value))

	case "array":
		// Handle array values
		args = append(args, "-array")
		switch v := value.(type) {
		case []interface{}:
			for _, item := range v {
				args = append(args, fmt.Sprintf("%v", item))
			}
		case []string:
			args = append(args, v...)
		}

	case "array-add":
		// Add to existing array
		args[0] = "write"
		args = append(args, "-array-add")
		args = append(args, fmt.Sprintf("%v", value))

	case "dict":
		// Handle dictionary values
		args = append(args, "-dict")
		switch v := value.(type) {
		case map[string]interface{}:
			for k, val := range v {
				args = append(args, k, fmt.Sprintf("%v", val))
			}
		}

	default:
		return false, fmt.Errorf("unknown defaults type: %s", valueType)
	}

	// Execute defaults command
	cmd := exec.Command("defaults", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("defaults write failed: %v - %s", err, string(output))
	}

	return true, nil
}

// ensureAbsent removes a defaults value
func (h *DefaultsHandler) ensureAbsent(domain, key string) (bool, error) {
	// Check if value exists
	_, _, err := h.getCurrentValue(domain, key)
	if err != nil {
		// Value doesn't exist
		return false, nil
	}

	// Delete the value
	cmd := exec.Command("defaults", "delete", domain, key)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("defaults delete failed: %v - %s", err, string(output))
	}

	return true, nil
}

// getCurrentValue reads the current defaults value
func (h *DefaultsHandler) getCurrentValue(domain, key string) (string, string, error) {
	// Get value
	cmd := exec.Command("defaults", "read", domain, key)
	output, err := cmd.Output()
	if err != nil {
		return "", "", err
	}

	value := strings.TrimSpace(string(output))

	// Get type
	cmd = exec.Command("defaults", "read-type", domain, key)
	typeOutput, err := cmd.Output()
	if err != nil {
		return value, "string", nil // Assume string if can't get type
	}

	// Parse type output: "Type is string" or "Type is integer" etc.
	typeStr := strings.TrimSpace(string(typeOutput))
	if strings.HasPrefix(typeStr, "Type is ") {
		typeStr = strings.TrimPrefix(typeStr, "Type is ")
	}

	return value, typeStr, nil
}

// valuesEqual compares a current value with a desired value
func (h *DefaultsHandler) valuesEqual(current, currentType string, desired interface{}) bool {
	switch currentType {
	case "integer":
		currentInt, err := strconv.ParseInt(current, 10, 64)
		if err != nil {
			return false
		}
		switch v := desired.(type) {
		case int:
			return currentInt == int64(v)
		case int64:
			return currentInt == v
		case float64:
			return currentInt == int64(v)
		case string:
			desiredInt, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return false
			}
			return currentInt == desiredInt
		}

	case "float":
		currentFloat, err := strconv.ParseFloat(current, 64)
		if err != nil {
			return false
		}
		switch v := desired.(type) {
		case float64:
			return currentFloat == v
		case int:
			return currentFloat == float64(v)
		case string:
			desiredFloat, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return false
			}
			return currentFloat == desiredFloat
		}

	case "boolean":
		currentBool := current == "1" || strings.ToLower(current) == "true"
		switch v := desired.(type) {
		case bool:
			return currentBool == v
		case string:
			desiredBool := strings.ToLower(v) == "true" || v == "1" || v == "yes"
			return currentBool == desiredBool
		case int:
			return currentBool == (v != 0)
		}

	default:
		// String comparison
		return current == fmt.Sprintf("%v", desired)
	}

	return false
}

// detectType auto-detects the value type
func (h *DefaultsHandler) detectType(value interface{}) string {
	switch value.(type) {
	case bool:
		return "bool"
	case int, int32, int64:
		return "int"
	case float32, float64:
		return "float"
	case []interface{}, []string:
		return "array"
	case map[string]interface{}:
		return "dict"
	default:
		return "string"
	}
}
