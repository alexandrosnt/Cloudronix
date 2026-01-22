package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/cloudronix/agent/pkg/playbook"
)

// FileHandler manages file operations
type FileHandler struct{}

// NewFileHandler creates a new file handler
func NewFileHandler() *FileHandler {
	return &FileHandler{}
}

// Supports returns all platforms
func (h *FileHandler) Supports() []string {
	return []string{"all"}
}

// Validate checks if the params are valid
func (h *FileHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["path"]; !ok {
		return fmt.Errorf("file action requires 'path' parameter")
	}
	return nil
}

// Execute performs the file operation
func (h *FileHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	result := &playbook.TaskResult{
		StartTime: time.Now(),
		Status:    playbook.TaskStatusRunning,
	}

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter must be a non-empty string")
	}

	// Determine operation state
	state := "file" // default
	if s, ok := params["state"].(string); ok {
		state = s
	}

	var err error
	switch state {
	case "absent":
		result.Changed, err = h.ensureAbsent(path)
	case "directory":
		result.Changed, err = h.ensureDirectory(path, params)
	case "file":
		result.Changed, err = h.ensureFile(path, params)
	case "touch":
		result.Changed, err = h.touchFile(path, params)
	case "link":
		result.Changed, err = h.ensureLink(path, params)
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

// ensureAbsent removes a file or directory
func (h *FileHandler) ensureAbsent(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil // Already absent
	}
	if err != nil {
		return false, err
	}

	err = os.RemoveAll(path)
	if err != nil {
		return false, fmt.Errorf("failed to remove '%s': %w", path, err)
	}

	return true, nil
}

// ensureDirectory creates a directory if it doesn't exist
func (h *FileHandler) ensureDirectory(path string, params map[string]interface{}) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			// Directory exists, check permissions
			return h.setPermissions(path, params)
		}
		return false, fmt.Errorf("'%s' exists but is not a directory", path)
	}

	if !os.IsNotExist(err) {
		return false, err
	}

	// Create directory
	mode := os.FileMode(0755)
	if m, ok := params["mode"].(string); ok {
		parsed, err := strconv.ParseUint(m, 8, 32)
		if err == nil {
			mode = os.FileMode(parsed)
		}
	}

	if err := os.MkdirAll(path, mode); err != nil {
		return false, fmt.Errorf("failed to create directory '%s': %w", path, err)
	}

	// Set permissions (for Unix systems)
	h.setPermissions(path, params)

	return true, nil
}

// ensureFile creates or updates a file
func (h *FileHandler) ensureFile(path string, params map[string]interface{}) (bool, error) {
	content, hasContent := params["content"].(string)
	src, hasSrc := params["src"].(string)

	if hasContent && hasSrc {
		return false, fmt.Errorf("cannot specify both 'content' and 'src'")
	}

	var newContent []byte
	if hasContent {
		newContent = []byte(content)
	} else if hasSrc {
		data, err := os.ReadFile(src)
		if err != nil {
			return false, fmt.Errorf("failed to read source file '%s': %w", src, err)
		}
		newContent = data
	}

	// Check if file exists and compare content
	existingContent, err := os.ReadFile(path)
	if err == nil {
		if len(newContent) > 0 {
			// Compare hashes
			existingHash := sha256.Sum256(existingContent)
			newHash := sha256.Sum256(newContent)
			if existingHash == newHash {
				// Content is the same, just check permissions
				return h.setPermissions(path, params)
			}
		} else {
			// No content specified, just ensure file exists and set permissions
			return h.setPermissions(path, params)
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Write file
	mode := os.FileMode(0644)
	if m, ok := params["mode"].(string); ok {
		parsed, err := strconv.ParseUint(m, 8, 32)
		if err == nil {
			mode = os.FileMode(parsed)
		}
	}

	if len(newContent) > 0 {
		if err := os.WriteFile(path, newContent, mode); err != nil {
			return false, fmt.Errorf("failed to write file '%s': %w", path, err)
		}
	} else {
		// Create empty file
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, mode)
		if err != nil {
			return false, fmt.Errorf("failed to create file '%s': %w", path, err)
		}
		f.Close()
	}

	// Set permissions
	h.setPermissions(path, params)

	return true, nil
}

// touchFile updates the modification time or creates an empty file
func (h *FileHandler) touchFile(path string, params map[string]interface{}) (bool, error) {
	now := time.Now()

	// Check if file exists
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		// Create empty file
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return false, fmt.Errorf("failed to create parent directory: %w", err)
		}

		f, err := os.Create(path)
		if err != nil {
			return false, fmt.Errorf("failed to create file '%s': %w", path, err)
		}
		f.Close()
		h.setPermissions(path, params)
		return true, nil
	}

	if err != nil {
		return false, err
	}

	// Update times
	if err := os.Chtimes(path, now, now); err != nil {
		return false, fmt.Errorf("failed to update times on '%s': %w", path, err)
	}

	return true, nil
}

// ensureLink creates a symbolic link
func (h *FileHandler) ensureLink(path string, params map[string]interface{}) (bool, error) {
	target, ok := params["src"].(string)
	if !ok || target == "" {
		return false, fmt.Errorf("link state requires 'src' parameter for link target")
	}

	// Check if link already exists and points to correct target
	existingTarget, err := os.Readlink(path)
	if err == nil {
		if existingTarget == target {
			return false, nil // Already correct
		}
		// Remove existing link
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("failed to remove existing link: %w", err)
		}
	} else if !os.IsNotExist(err) {
		// Check if it's a regular file
		info, statErr := os.Stat(path)
		if statErr == nil && !info.Mode().IsRegular() {
			return false, fmt.Errorf("'%s' exists and is not a symbolic link", path)
		}
		// Remove existing file
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("failed to remove existing file: %w", err)
		}
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Create symlink
	if err := os.Symlink(target, path); err != nil {
		return false, fmt.Errorf("failed to create symlink: %w", err)
	}

	return true, nil
}

// setPermissions sets file permissions and ownership
func (h *FileHandler) setPermissions(path string, params map[string]interface{}) (bool, error) {
	changed := false

	// Set mode
	if m, ok := params["mode"].(string); ok {
		parsed, err := strconv.ParseUint(m, 8, 32)
		if err == nil {
			mode := os.FileMode(parsed)
			info, err := os.Stat(path)
			if err == nil && info.Mode().Perm() != mode.Perm() {
				if err := os.Chmod(path, mode); err != nil {
					return false, fmt.Errorf("failed to set mode: %w", err)
				}
				changed = true
			}
		}
	}

	// Set ownership (Unix only)
	if runtime.GOOS != "windows" {
		owner, hasOwner := params["owner"].(string)
		group, hasGroup := params["group"].(string)
		if hasOwner || hasGroup {
			// Would need to look up UID/GID and use os.Chown
			// For now, skip ownership changes
			_ = owner
			_ = group
		}
	}

	return changed, nil
}

// CopyFile copies a file from src to dest
func CopyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	return err
}

// FileHash calculates the SHA256 hash of a file
func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
