//go:build !linux

package actions

import (
	"context"
	"fmt"

	"github.com/cloudronix/agent/pkg/playbook"
)

// SysctlHandler is a stub for non-Linux platforms
type SysctlHandler struct{}

// NewSysctlHandler creates a new sysctl handler (stub on non-Linux)
func NewSysctlHandler() *SysctlHandler {
	return &SysctlHandler{}
}

// Supports returns Linux only
func (h *SysctlHandler) Supports() []string {
	return []string{"linux"}
}

// Validate checks if the params are valid
func (h *SysctlHandler) Validate(params map[string]interface{}) error {
	return fmt.Errorf("sysctl action is only available on Linux")
}

// Execute is not available on non-Linux platforms
func (h *SysctlHandler) Execute(ctx context.Context, params map[string]interface{}, vars *playbook.Variables) (*playbook.TaskResult, error) {
	return nil, fmt.Errorf("sysctl action is only available on Linux")
}
