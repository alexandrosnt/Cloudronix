package playbook

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Variable patterns
var (
	// {{ variable }} - playbook variables and built-ins
	varPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_\.]*)\s*\}\}`)

	// ${ENV_VAR} - environment variables
	envPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
)

// Variables manages variable resolution for playbook execution
type Variables struct {
	// User-defined variables from playbook
	userVars map[string]string

	// Task results (from register)
	taskResults map[string]*TaskResult

	// Built-in variables (platform, paths, etc.)
	builtins map[string]string
}

// NewVariables creates a new variable context
func NewVariables() *Variables {
	v := &Variables{
		userVars:    make(map[string]string),
		taskResults: make(map[string]*TaskResult),
		builtins:    make(map[string]string),
	}
	v.initBuiltins()
	return v
}

// initBuiltins sets up built-in variables
func (v *Variables) initBuiltins() {
	// Platform information
	v.builtins["platform"] = runtime.GOOS
	v.builtins["arch"] = runtime.GOARCH
	v.builtins["os_family"] = getOSFamily()

	// Get hostname
	if hostname, err := os.Hostname(); err == nil {
		v.builtins["hostname"] = hostname
	}

	// Cross-platform paths
	v.builtins["user_home"] = getUserHome()
	v.builtins["user_config"] = getUserConfig()
	v.builtins["user_cache"] = getUserCache()
	v.builtins["system_config"] = getSystemConfig()
	v.builtins["temp_dir"] = os.TempDir()

	// Path separator
	v.builtins["path_sep"] = string(filepath.Separator)
}

// SetUserVars sets variables from the playbook's variables section
func (v *Variables) SetUserVars(vars map[string]string) {
	for key, value := range vars {
		// Resolve any environment variables in the value
		resolved := v.resolveEnvVars(value)
		v.userVars[key] = resolved
	}
}

// SetTaskResult stores a task result for later reference
func (v *Variables) SetTaskResult(name string, result *TaskResult) {
	v.taskResults[name] = result
}

// Set sets a single variable
func (v *Variables) Set(name, value string) {
	v.userVars[name] = value
}

// Get retrieves a variable value
func (v *Variables) Get(name string) (string, bool) {
	// Check user vars first
	if val, ok := v.userVars[name]; ok {
		return val, true
	}
	// Check builtins
	if val, ok := v.builtins[name]; ok {
		return val, true
	}
	return "", false
}

// GetTaskResult retrieves a registered task result
func (v *Variables) GetTaskResult(name string) (*TaskResult, bool) {
	result, ok := v.taskResults[name]
	return result, ok
}

// Substitute replaces all variable references in a string
//
// Supports:
//   - {{ variable }} - playbook variables and built-ins
//   - {{ env.VAR }} - environment variables via built-in syntax
//   - ${ENV_VAR} - direct environment variables
//   - {{ result.stdout }} - task result properties
func (v *Variables) Substitute(input string) (string, error) {
	result := input

	// First, resolve ${ENV_VAR} patterns
	result = v.resolveEnvVars(result)

	// Then, resolve {{ variable }} patterns
	var lastErr error
	result = varPattern.ReplaceAllStringFunc(result, func(match string) string {
		// Extract variable name
		submatch := varPattern.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		varName := submatch[1]

		// Handle special prefixes
		if strings.HasPrefix(varName, "env.") {
			// {{ env.VAR }} - environment variable
			envName := strings.TrimPrefix(varName, "env.")
			if val := os.Getenv(envName); val != "" {
				return val
			}
			return match // Keep original if not found
		}

		// Handle task result references
		if strings.Contains(varName, ".") {
			parts := strings.SplitN(varName, ".", 2)
			if result, ok := v.taskResults[parts[0]]; ok {
				val, err := v.getTaskResultProperty(result, parts[1])
				if err != nil {
					lastErr = err
					return match
				}
				return val
			}
		}

		// Regular variable lookup
		if val, ok := v.Get(varName); ok {
			return val
		}

		// Variable not found - this might be an error
		lastErr = &VariableError{
			VariableName: varName,
			Cause:        ErrVariableNotFound,
		}
		return match // Keep original
	})

	return result, lastErr
}

// SubstituteMap substitutes variables in all string values of a map
func (v *Variables) SubstituteMap(params map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range params {
		switch val := value.(type) {
		case string:
			resolved, err := v.Substitute(val)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		case map[string]interface{}:
			resolved, err := v.SubstituteMap(val)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		case []interface{}:
			resolved, err := v.substituteSlice(val)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		default:
			result[key] = value
		}
	}

	return result, nil
}

// substituteSlice substitutes variables in a slice
func (v *Variables) substituteSlice(items []interface{}) ([]interface{}, error) {
	result := make([]interface{}, len(items))

	for i, item := range items {
		switch val := item.(type) {
		case string:
			resolved, err := v.Substitute(val)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		case map[string]interface{}:
			resolved, err := v.SubstituteMap(val)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		default:
			result[i] = item
		}
	}

	return result, nil
}

// resolveEnvVars resolves ${ENV_VAR} patterns
func (v *Variables) resolveEnvVars(input string) string {
	return envPattern.ReplaceAllStringFunc(input, func(match string) string {
		submatch := envPattern.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		envName := submatch[1]
		if val := os.Getenv(envName); val != "" {
			return val
		}
		return match // Keep original if not found
	})
}

// getTaskResultProperty extracts a property from a task result
func (v *Variables) getTaskResultProperty(result *TaskResult, property string) (string, error) {
	switch property {
	case "stdout":
		return result.Stdout, nil
	case "stderr":
		return result.Stderr, nil
	case "exit_code":
		return fmt.Sprintf("%d", result.ExitCode), nil
	case "status":
		return string(result.Status), nil
	case "changed":
		return fmt.Sprintf("%t", result.Changed), nil
	default:
		return "", fmt.Errorf("unknown property '%s'", property)
	}
}

// Helper functions for cross-platform paths

func getUserHome() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home
	}
	return ""
}

func getUserConfig() string {
	switch runtime.GOOS {
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return appdata
		}
		return filepath.Join(getUserHome(), "AppData", "Roaming")
	case "darwin":
		return filepath.Join(getUserHome(), "Library", "Application Support")
	default: // Linux and others
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return xdg
		}
		return filepath.Join(getUserHome(), ".config")
	}
}

func getUserCache() string {
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "cache")
		}
		return filepath.Join(getUserHome(), "AppData", "Local", "cache")
	case "darwin":
		return filepath.Join(getUserHome(), "Library", "Caches")
	default: // Linux and others
		if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
			return xdg
		}
		return filepath.Join(getUserHome(), ".cache")
	}
}

func getSystemConfig() string {
	switch runtime.GOOS {
	case "windows":
		if programData := os.Getenv("ProgramData"); programData != "" {
			return programData
		}
		return "C:\\ProgramData"
	default: // Linux, macOS
		return "/etc"
	}
}

func getOSFamily() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "darwin"
	case "linux":
		// Could detect debian/redhat/etc from /etc/os-release
		return "linux"
	case "android":
		return "android"
	default:
		return runtime.GOOS
	}
}
