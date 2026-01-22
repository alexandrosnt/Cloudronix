//go:build !windows

package actions

import (
	"context"
	"fmt"

	"github.com/cloudronix/agent/pkg/playbook"
)

// RegistryHandler is a stub for non-Windows platforms
type RegistryHandler struct{}

// NewRegistryHandler creates a new registry handler (stub on non-Windows)
func NewRegistryHandler() *RegistryHandler {
	return &RegistryHandler{}
}

// Supports returns Windows only
func (h *RegistryHandler) Supports() []string {
	return []string{"windows"}
}

// Validate checks if the params are valid
func (h *RegistryHandler) Validate(params map[string]interface{}) error {
	return fmt.Errorf("registry action is only available on Windows")
}

// Execute is not available on non-Windows platforms
func (h *RegistryHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	return nil, fmt.Errorf("registry action is only available on Windows")
}
