//go:build windows

package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/cloudronix/agent/internal/config"
)

const serviceName = "CloudronixAgent"
const serviceDisplayName = "Cloudronix Agent"
const serviceDescription = "Cloudronix device management agent"

// Standard installation directory
var installDir = filepath.Join(os.Getenv("ProgramFiles"), "Cloudronix")

// cloudronixService implements svc.Handler
type cloudronixService struct {
	cfg *config.Config
}

// Execute is the main service entry point called by Windows SCM
func (s *cloudronixService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	// Start the agent in a goroutine
	stopCh := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		errCh <- runAgent(s.cfg, stopCh)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	// Wait for stop signal or error
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				close(stopCh)
				// Wait a bit for graceful shutdown
				time.Sleep(2 * time.Second)
				return
			case svc.Interrogate:
				changes <- c.CurrentStatus
			}
		case err := <-errCh:
			if err != nil {
				// Log error somewhere if needed
			}
			return
		}
	}
}

// RunAsService runs the agent as a Windows Service
func RunAsService(cfg *config.Config) error {
	return svc.Run(serviceName, &cloudronixService{cfg: cfg})
}

// IsWindowsService checks if we're running as a Windows Service
func IsWindowsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

// installWindows installs the agent as a Windows Service
func installWindows(cfg *config.Config) error {
	srcPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	fmt.Println("Installing Cloudronix Agent...")

	// Create installation directory
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}

	// Copy executable to install directory
	dstPath := filepath.Join(installDir, "cloudronix-agent.exe")
	if srcPath != dstPath {
		fmt.Printf("Copying agent to %s...\n", installDir)
		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("failed to copy executable: %w", err)
		}
	}

	// Open service manager
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	// Check if service already exists
	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists - run 'cloudronix-agent uninstall' first", serviceName)
	}

	// Create the service
	s, err = m.CreateService(serviceName,
		dstPath,
		mgr.Config{
			DisplayName:  serviceDisplayName,
			Description:  serviceDescription,
			StartType:    mgr.StartAutomatic,
			ServiceStartName: "LocalSystem",
		},
		"run",
		"--config", cfg.ConfigDir,
	)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}
	defer s.Close()

	// Configure recovery options (restart on failure)
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	if err := s.SetRecoveryActions(recoveryActions, 3600); err != nil {
		fmt.Printf("Warning: failed to set recovery actions: %v\n", err)
	}

	// Start the service
	fmt.Println("Starting service...")
	if err := s.Start(); err != nil {
		fmt.Printf("Warning: failed to start service: %v\n", err)
		fmt.Println("You may need to start it manually: sc start CloudronixAgent")
	} else {
		fmt.Println("Service started successfully")
	}

	fmt.Println()
	fmt.Printf("Installed to: %s\n", dstPath)
	fmt.Printf("Service name: %s\n", serviceName)
	fmt.Println("Check status: sc query CloudronixAgent")

	return nil
}

// uninstallWindows removes the Windows Service
func uninstallWindows() {
	m, err := mgr.Connect()
	if err != nil {
		fmt.Printf("Warning: failed to connect to service manager: %v\n", err)
		return
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		fmt.Printf("Service %s not found\n", serviceName)
		return
	}
	defer s.Close()

	// Stop the service
	fmt.Println("Stopping service...")
	s.Control(svc.Stop)
	time.Sleep(2 * time.Second)

	// Delete the service
	fmt.Println("Removing service...")
	if err := s.Delete(); err != nil {
		fmt.Printf("Warning: failed to delete service: %v\n", err)
	}

	// Remove installed executable
	exePath := filepath.Join(installDir, "cloudronix-agent.exe")
	if err := os.Remove(exePath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: failed to remove executable: %v\n", err)
	}

	// Try to remove install directory (will fail if not empty)
	os.Remove(installDir)
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
}
