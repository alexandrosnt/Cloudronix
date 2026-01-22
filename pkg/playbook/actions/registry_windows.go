//go:build windows

package actions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/cloudronix/agent/pkg/playbook"
)

// RegistryHandler manages Windows registry operations
type RegistryHandler struct{}

// NewRegistryHandler creates a new registry handler
func NewRegistryHandler() *RegistryHandler {
	return &RegistryHandler{}
}

// Supports returns Windows only
func (h *RegistryHandler) Supports() []string {
	return []string{"windows"}
}

// Validate checks if the params are valid
func (h *RegistryHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["path"]; !ok {
		return fmt.Errorf("registry action requires 'path' parameter")
	}
	return nil
}

// Execute performs the registry operation
func (h *RegistryHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	result := &playbook.TaskResult{
		StartTime: time.Now(),
		Status:    playbook.TaskStatusRunning,
	}

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter must be a non-empty string")
	}

	// Parse path to get root key and subkey
	rootKey, subKey, err := parseRegistryPath(path)
	if err != nil {
		return nil, err
	}

	// Determine operation
	state := "present" // default
	if s, ok := params["state"].(string); ok {
		state = s
	}

	switch state {
	case "present":
		result.Changed, err = h.ensurePresent(rootKey, subKey, params)
	case "absent":
		result.Changed, err = h.ensureAbsent(rootKey, subKey, params)
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

// parseRegistryPath parses a registry path into root key and subkey
func parseRegistryPath(path string) (registry.Key, string, error) {
	parts := strings.SplitN(path, `\`, 2)
	if len(parts) < 2 {
		return 0, "", fmt.Errorf("invalid registry path: %s", path)
	}

	rootName := strings.ToUpper(parts[0])
	subKey := parts[1]

	var rootKey registry.Key
	switch rootName {
	case "HKEY_LOCAL_MACHINE", "HKLM":
		rootKey = registry.LOCAL_MACHINE
	case "HKEY_CURRENT_USER", "HKCU":
		rootKey = registry.CURRENT_USER
	case "HKEY_CLASSES_ROOT", "HKCR":
		rootKey = registry.CLASSES_ROOT
	case "HKEY_USERS", "HKU":
		rootKey = registry.USERS
	case "HKEY_CURRENT_CONFIG", "HKCC":
		rootKey = registry.CURRENT_CONFIG
	default:
		return 0, "", fmt.Errorf("unknown registry root: %s", rootName)
	}

	return rootKey, subKey, nil
}

// ensurePresent creates or updates a registry key/value
func (h *RegistryHandler) ensurePresent(rootKey registry.Key, subKey string, params map[string]interface{}) (bool, error) {
	// Open or create the key
	key, _, err := registry.CreateKey(rootKey, subKey, registry.ALL_ACCESS)
	if err != nil {
		return false, fmt.Errorf("failed to open/create registry key: %w", err)
	}
	defer key.Close()

	// If no value specified, just ensure key exists
	valueName, hasValue := params["name"].(string)
	if !hasValue {
		return true, nil // Key was created/opened
	}

	// Get value type
	valueType := "string" // default
	if t, ok := params["type"].(string); ok {
		valueType = strings.ToLower(t)
	}

	// Get value data
	valueData, hasData := params["value"]
	if !hasData {
		return false, fmt.Errorf("'value' parameter required when 'name' is specified")
	}

	// Check current value
	changed := false
	switch valueType {
	case "string", "sz":
		strVal := fmt.Sprintf("%v", valueData)
		existing, _, err := key.GetStringValue(valueName)
		if err != nil || existing != strVal {
			if err := key.SetStringValue(valueName, strVal); err != nil {
				return false, fmt.Errorf("failed to set string value: %w", err)
			}
			changed = true
		}

	case "expandstring", "expand_sz":
		strVal := fmt.Sprintf("%v", valueData)
		existing, _, err := key.GetStringValue(valueName)
		if err != nil || existing != strVal {
			if err := key.SetExpandStringValue(valueName, strVal); err != nil {
				return false, fmt.Errorf("failed to set expand string value: %w", err)
			}
			changed = true
		}

	case "dword", "integer":
		var intVal uint32
		switch v := valueData.(type) {
		case int:
			intVal = uint32(v)
		case int64:
			intVal = uint32(v)
		case float64:
			intVal = uint32(v)
		case string:
			parsed, err := strconv.ParseUint(v, 0, 32)
			if err != nil {
				return false, fmt.Errorf("invalid DWORD value: %v", valueData)
			}
			intVal = uint32(parsed)
		default:
			return false, fmt.Errorf("invalid DWORD value type: %T", valueData)
		}

		existing, _, err := key.GetIntegerValue(valueName)
		if err != nil || uint32(existing) != intVal {
			if err := key.SetDWordValue(valueName, intVal); err != nil {
				return false, fmt.Errorf("failed to set DWORD value: %w", err)
			}
			changed = true
		}

	case "qword":
		var intVal uint64
		switch v := valueData.(type) {
		case int:
			intVal = uint64(v)
		case int64:
			intVal = uint64(v)
		case float64:
			intVal = uint64(v)
		case string:
			parsed, err := strconv.ParseUint(v, 0, 64)
			if err != nil {
				return false, fmt.Errorf("invalid QWORD value: %v", valueData)
			}
			intVal = parsed
		default:
			return false, fmt.Errorf("invalid QWORD value type: %T", valueData)
		}

		existing, _, err := key.GetIntegerValue(valueName)
		if err != nil || existing != intVal {
			if err := key.SetQWordValue(valueName, intVal); err != nil {
				return false, fmt.Errorf("failed to set QWORD value: %w", err)
			}
			changed = true
		}

	case "multistring", "multi_sz":
		var strVals []string
		switch v := valueData.(type) {
		case []interface{}:
			for _, item := range v {
				strVals = append(strVals, fmt.Sprintf("%v", item))
			}
		case []string:
			strVals = v
		case string:
			strVals = strings.Split(v, "\n")
		default:
			return false, fmt.Errorf("invalid multi-string value type: %T", valueData)
		}

		existing, _, err := key.GetStringsValue(valueName)
		if err != nil || !stringSlicesEqual(existing, strVals) {
			if err := key.SetStringsValue(valueName, strVals); err != nil {
				return false, fmt.Errorf("failed to set multi-string value: %w", err)
			}
			changed = true
		}

	case "binary":
		var binVal []byte
		switch v := valueData.(type) {
		case []byte:
			binVal = v
		case string:
			// Assume hex string
			binVal = []byte(v)
		default:
			return false, fmt.Errorf("invalid binary value type: %T", valueData)
		}

		existing, _, err := key.GetBinaryValue(valueName)
		if err != nil || !bytesEqual(existing, binVal) {
			if err := key.SetBinaryValue(valueName, binVal); err != nil {
				return false, fmt.Errorf("failed to set binary value: %w", err)
			}
			changed = true
		}

	default:
		return false, fmt.Errorf("unknown registry value type: %s", valueType)
	}

	return changed, nil
}

// ensureAbsent removes a registry key or value
func (h *RegistryHandler) ensureAbsent(rootKey registry.Key, subKey string, params map[string]interface{}) (bool, error) {
	valueName, hasValue := params["name"].(string)

	if hasValue {
		// Delete specific value
		key, err := registry.OpenKey(rootKey, subKey, registry.SET_VALUE)
		if err != nil {
			if err == registry.ErrNotExist {
				return false, nil // Key doesn't exist, so value is already absent
			}
			return false, fmt.Errorf("failed to open registry key: %w", err)
		}
		defer key.Close()

		err = key.DeleteValue(valueName)
		if err != nil {
			if err == registry.ErrNotExist {
				return false, nil // Value already absent
			}
			return false, fmt.Errorf("failed to delete registry value: %w", err)
		}
		return true, nil
	}

	// Delete entire key
	err := registry.DeleteKey(rootKey, subKey)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil // Key already absent
		}
		return false, fmt.Errorf("failed to delete registry key: %w", err)
	}

	return true, nil
}

// stringSlicesEqual compares two string slices
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bytesEqual compares two byte slices
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
