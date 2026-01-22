//go:build !darwin

package actions

import (
	"context"
	"fmt"

	"github.com/cloudronix/agent/pkg/playbook"
)

// DefaultsHandler is a stub for non-macOS platforms
type DefaultsHandler struct{}

// NewDefaultsHandler creates a new defaults handler (stub on non-macOS)
func NewDefaultsHandler() *DefaultsHandler {
	return &DefaultsHandler{}
}

// Supports returns macOS only
func (h *DefaultsHandler) Supports() []string {
	return []string{"darwin"}
}

// Validate checks if the params are valid
func (h *DefaultsHandler) Validate(params map[string]interface{}) error {
	return fmt.Errorf("defaults action is only available on macOS")
}

// Execute is not available on non-macOS platforms
func (h *DefaultsHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	return nil, fmt.Errorf("defaults action is only available on macOS")
}
