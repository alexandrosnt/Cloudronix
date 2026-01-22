// Package actions provides action handlers for the playbook executor
package actions

import (
	"github.com/cloudronix/agent/pkg/playbook"
)

// RegisterAllHandlers registers all built-in action handlers with an executor
func RegisterAllHandlers(executor *playbook.Executor) {
	// Cross-platform actions
	executor.RegisterHandler(playbook.ActionCommand, NewCommandHandler())
	executor.RegisterHandler(playbook.ActionFile, NewFileHandler())
	executor.RegisterHandler(playbook.ActionLineinfile, NewLineinfileHandler())
	executor.RegisterHandler(playbook.ActionEnv, NewEnvHandler())
	executor.RegisterHandler(playbook.ActionService, NewServiceHandler())

	// Platform-specific actions (stubs on unsupported platforms)
	executor.RegisterHandler(playbook.ActionRegistry, NewRegistryHandler())
	executor.RegisterHandler(playbook.ActionSysctl, NewSysctlHandler())
	executor.RegisterHandler(playbook.ActionDefaults, NewDefaultsHandler())
}

// CreateHandler creates a handler by action type name
func CreateHandler(actionType string) playbook.ActionHandler {
	switch actionType {
	case playbook.ActionCommand:
		return NewCommandHandler()
	case playbook.ActionFile:
		return NewFileHandler()
	case playbook.ActionLineinfile:
		return NewLineinfileHandler()
	case playbook.ActionEnv:
		return NewEnvHandler()
	case playbook.ActionService:
		return NewServiceHandler()
	case playbook.ActionRegistry:
		return NewRegistryHandler()
	case playbook.ActionSysctl:
		return NewSysctlHandler()
	case playbook.ActionDefaults:
		return NewDefaultsHandler()
	default:
		return nil
	}
}
