package actions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cloudronix/agent/pkg/playbook"
)

// LineinfileHandler manages line-level file modifications
type LineinfileHandler struct{}

// NewLineinfileHandler creates a new lineinfile handler
func NewLineinfileHandler() *LineinfileHandler {
	return &LineinfileHandler{}
}

// Supports returns all platforms
func (h *LineinfileHandler) Supports() []string {
	return []string{"all"}
}

// Validate checks if the params are valid
func (h *LineinfileHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["path"]; !ok {
		return fmt.Errorf("lineinfile action requires 'path' parameter")
	}
	return nil
}

// Execute performs the lineinfile operation
func (h *LineinfileHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	result := &playbook.TaskResult{
		StartTime: time.Now(),
		Status:    playbook.TaskStatusRunning,
	}

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter must be a non-empty string")
	}

	// Determine operation
	state := "present" // default
	if s, ok := params["state"].(string); ok {
		state = s
	}

	var err error
	switch state {
	case "present":
		result.Changed, err = h.ensurePresent(path, params)
	case "absent":
		result.Changed, err = h.ensureAbsent(path, params)
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

// ensurePresent ensures a line is present in the file
func (h *LineinfileHandler) ensurePresent(path string, params map[string]interface{}) (bool, error) {
	line, hasLine := params["line"].(string)
	regexStr, hasRegex := params["regexp"].(string)

	if !hasLine && !hasRegex {
		return false, fmt.Errorf("'line' or 'regexp' parameter is required for state 'present'")
	}

	// Create file if it doesn't exist
	create := true
	if c, ok := params["create"].(bool); ok {
		create = c
	}

	// Read existing content
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if !create {
				return false, fmt.Errorf("file '%s' does not exist and create=false", path)
			}
			// Create directory structure
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return false, fmt.Errorf("failed to create directory: %w", err)
			}
			content = []byte{}
		} else {
			return false, err
		}
	}

	lines := strings.Split(string(content), "\n")
	changed := false

	// Handle different insertion modes
	insertBefore, hasInsertBefore := params["insertbefore"].(string)
	insertAfter, hasInsertAfter := params["insertafter"].(string)
	_ = insertBefore // Will be used below

	// If we have a regex, find and replace matching lines
	if hasRegex {
		regex, err := regexp.Compile(regexStr)
		if err != nil {
			return false, fmt.Errorf("invalid regexp: %w", err)
		}

		found := false
		for i, l := range lines {
			if regex.MatchString(l) {
				found = true
				if hasLine && l != line {
					lines[i] = line
					changed = true
				}
				break // Only replace first match by default
			}
		}

		// If no match and we have a line to insert
		if !found && hasLine {
			lines, changed = h.insertLine(lines, line, insertAfter, hasInsertAfter, insertBefore, hasInsertBefore)
		}
	} else if hasLine {
		// No regex, just ensure line exists
		found := false
		for _, l := range lines {
			if l == line {
				found = true
				break
			}
		}

		if !found {
			lines, changed = h.insertLine(lines, line, insertAfter, hasInsertAfter, insertBefore, hasInsertBefore)
		}
	}

	if changed {
		// Write back to file
		newContent := strings.Join(lines, "\n")
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			return false, fmt.Errorf("failed to write file: %w", err)
		}
	}

	return changed, nil
}

// ensureAbsent ensures a line is not present in the file
func (h *LineinfileHandler) ensureAbsent(path string, params map[string]interface{}) (bool, error) {
	line, hasLine := params["line"].(string)
	regexStr, hasRegex := params["regexp"].(string)

	if !hasLine && !hasRegex {
		return false, fmt.Errorf("'line' or 'regexp' parameter is required for state 'absent'")
	}

	// Read existing content
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // File doesn't exist, line is already absent
		}
		return false, err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	changed := false

	var regex *regexp.Regexp
	if hasRegex {
		regex, err = regexp.Compile(regexStr)
		if err != nil {
			return false, fmt.Errorf("invalid regexp: %w", err)
		}
	}

	for _, l := range lines {
		remove := false

		if hasRegex && regex.MatchString(l) {
			remove = true
		} else if hasLine && l == line {
			remove = true
		}

		if remove {
			changed = true
		} else {
			newLines = append(newLines, l)
		}
	}

	if changed {
		newContent := strings.Join(newLines, "\n")
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			return false, fmt.Errorf("failed to write file: %w", err)
		}
	}

	return changed, nil
}

// insertLine inserts a line at the appropriate position
func (h *LineinfileHandler) insertLine(lines []string, line string, insertAfter string, hasInsertAfter bool, insertBefore string, hasInsertBefore bool) ([]string, bool) {
	if hasInsertAfter {
		if insertAfter == "EOF" {
			// Insert at end of file
			return append(lines, line), true
		}

		// Find the line to insert after
		regex, err := regexp.Compile(insertAfter)
		if err == nil {
			for i, l := range lines {
				if regex.MatchString(l) {
					// Insert after this line
					newLines := make([]string, 0, len(lines)+1)
					newLines = append(newLines, lines[:i+1]...)
					newLines = append(newLines, line)
					newLines = append(newLines, lines[i+1:]...)
					return newLines, true
				}
			}
		}
	}

	if hasInsertBefore {
		if insertBefore == "BOF" {
			// Insert at beginning of file
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, line)
			newLines = append(newLines, lines...)
			return newLines, true
		}

		// Find the line to insert before
		regex, err := regexp.Compile(insertBefore)
		if err == nil {
			for i, l := range lines {
				if regex.MatchString(l) {
					// Insert before this line
					newLines := make([]string, 0, len(lines)+1)
					newLines = append(newLines, lines[:i]...)
					newLines = append(newLines, line)
					newLines = append(newLines, lines[i:]...)
					return newLines, true
				}
			}
		}
	}

	// Default: append to end
	return append(lines, line), true
}

// BlockinfileHandler manages block-level file modifications
type BlockinfileHandler struct{}

// NewBlockinfileHandler creates a new blockinfile handler
func NewBlockinfileHandler() *BlockinfileHandler {
	return &BlockinfileHandler{}
}

// Supports returns all platforms
func (h *BlockinfileHandler) Supports() []string {
	return []string{"all"}
}

// Validate checks if the params are valid
func (h *BlockinfileHandler) Validate(params map[string]interface{}) error {
	if _, ok := params["path"]; !ok {
		return fmt.Errorf("blockinfile action requires 'path' parameter")
	}
	return nil
}

// Execute performs the blockinfile operation
func (h *BlockinfileHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	result := &playbook.TaskResult{
		StartTime: time.Now(),
		Status:    playbook.TaskStatusRunning,
	}

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter must be a non-empty string")
	}

	// Get block content
	block, _ := params["block"].(string)

	// Get marker pattern (default uses BEGIN/END markers)
	marker := "# {mark} MANAGED BLOCK"
	if m, ok := params["marker"].(string); ok {
		marker = m
	}

	beginMarker := strings.Replace(marker, "{mark}", "BEGIN", 1)
	endMarker := strings.Replace(marker, "{mark}", "END", 1)

	// Determine state
	state := "present"
	if s, ok := params["state"].(string); ok {
		state = s
	}

	var err error
	switch state {
	case "present":
		result.Changed, err = h.ensureBlockPresent(path, block, beginMarker, endMarker, params)
	case "absent":
		result.Changed, err = h.ensureBlockAbsent(path, beginMarker, endMarker)
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

// ensureBlockPresent ensures a block is present in the file
func (h *BlockinfileHandler) ensureBlockPresent(path, block, beginMarker, endMarker string, params map[string]interface{}) (bool, error) {
	// Create file if doesn't exist
	create := true
	if c, ok := params["create"].(bool); ok {
		create = c
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if !create {
				return false, fmt.Errorf("file '%s' does not exist and create=false", path)
			}
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return false, fmt.Errorf("failed to create directory: %w", err)
			}
			content = []byte{}
		} else {
			return false, err
		}
	}

	lines := strings.Split(string(content), "\n")

	// Find existing block
	beginIdx := -1
	endIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == strings.TrimSpace(beginMarker) {
			beginIdx = i
		}
		if strings.TrimSpace(l) == strings.TrimSpace(endMarker) {
			endIdx = i
		}
	}

	// Prepare new block
	newBlock := []string{beginMarker}
	if block != "" {
		newBlock = append(newBlock, strings.Split(block, "\n")...)
	}
	newBlock = append(newBlock, endMarker)

	var newLines []string
	if beginIdx >= 0 && endIdx > beginIdx {
		// Replace existing block
		existingBlock := strings.Join(lines[beginIdx:endIdx+1], "\n")
		newBlockStr := strings.Join(newBlock, "\n")
		if existingBlock == newBlockStr {
			return false, nil // No change needed
		}

		newLines = append(newLines, lines[:beginIdx]...)
		newLines = append(newLines, newBlock...)
		newLines = append(newLines, lines[endIdx+1:]...)
	} else {
		// Insert new block
		insertAfter, hasInsertAfter := params["insertafter"].(string)
		insertBefore, hasInsertBefore := params["insertbefore"].(string)

		inserted := false
		if hasInsertAfter {
			if insertAfter == "EOF" {
				newLines = append(lines, newBlock...)
				inserted = true
			} else {
				regex, err := regexp.Compile(insertAfter)
				if err == nil {
					for i, l := range lines {
						newLines = append(newLines, l)
						if regex.MatchString(l) {
							newLines = append(newLines, newBlock...)
							newLines = append(newLines, lines[i+1:]...)
							inserted = true
							break
						}
					}
				}
			}
		}

		if !inserted && hasInsertBefore {
			if insertBefore == "BOF" {
				newLines = append(newBlock, lines...)
				inserted = true
			} else {
				regex, err := regexp.Compile(insertBefore)
				if err == nil {
					for i, l := range lines {
						if regex.MatchString(l) {
							newLines = append(newLines, lines[:i]...)
							newLines = append(newLines, newBlock...)
							newLines = append(newLines, lines[i:]...)
							inserted = true
							break
						}
					}
				}
			}
		}

		if !inserted {
			// Default: append to end
			newLines = append(lines, newBlock...)
		}
	}

	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return false, fmt.Errorf("failed to write file: %w", err)
	}

	return true, nil
}

// ensureBlockAbsent removes a block from the file
func (h *BlockinfileHandler) ensureBlockAbsent(path, beginMarker, endMarker string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	lines := strings.Split(string(content), "\n")

	beginIdx := -1
	endIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == strings.TrimSpace(beginMarker) {
			beginIdx = i
		}
		if strings.TrimSpace(l) == strings.TrimSpace(endMarker) {
			endIdx = i
		}
	}

	if beginIdx < 0 || endIdx < beginIdx {
		return false, nil // Block not found
	}

	// Remove block
	newLines := append(lines[:beginIdx], lines[endIdx+1:]...)
	newContent := strings.Join(newLines, "\n")

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return false, fmt.Errorf("failed to write file: %w", err)
	}

	return true, nil
}
