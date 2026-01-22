//go:build !windows

package agent

import "github.com/cloudronix/agent/internal/config"

// IsWindowsService returns false on non-Windows platforms
func IsWindowsService() bool {
	return false
}

// RunAsService is not supported on non-Windows platforms
func RunAsService(cfg *config.Config) error {
	return Run(cfg)
}

// installWindows stub for non-Windows platforms (never called due to runtime.GOOS check)
func installWindows(cfg *config.Config) error {
	return nil
}

// uninstallWindows stub for non-Windows platforms (never called due to runtime.GOOS check)
func uninstallWindows() {}
