package agent

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/cloudronix/agent/internal/config"
)

// Install installs the agent as a system service
func Install(cfg *config.Config) error {
	if !cfg.IsEnrolled() {
		return fmt.Errorf("device is not enrolled\nRun 'cloudronix-agent enroll <token>' first")
	}

	switch runtime.GOOS {
	case "windows":
		return installWindows(cfg)
	case "linux":
		return installLinux(cfg)
	case "darwin":
		return installDarwin(cfg)
	default:
		return fmt.Errorf("service installation not supported on %s", runtime.GOOS)
	}
}

// Uninstall removes the agent service and credentials
func Uninstall(cfg *config.Config) error {
	fmt.Println("Uninstalling Cloudronix Agent...")

	// Stop and remove service
	switch runtime.GOOS {
	case "windows":
		uninstallWindows()
	case "linux":
		uninstallLinux()
	case "darwin":
		uninstallDarwin()
	}

	// Remove config directory
	if cfg.ConfigDir != "" {
		fmt.Printf("Removing config directory: %s\n", cfg.ConfigDir)
		if err := os.RemoveAll(cfg.ConfigDir); err != nil {
			fmt.Printf("Warning: failed to remove config directory: %v\n", err)
		}
	}

	fmt.Println("Uninstall complete")
	return nil
}

// installLinux installs the agent as a systemd service
func installLinux(cfg *config.Config) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Copy to /usr/local/bin
	installPath := "/usr/local/bin/cloudronix-agent"
	if exePath != installPath {
		fmt.Printf("Copying agent to %s...\n", installPath)
		input, err := os.ReadFile(exePath)
		if err != nil {
			return fmt.Errorf("failed to read executable: %w", err)
		}
		if err := os.WriteFile(installPath, input, 0755); err != nil {
			return fmt.Errorf("failed to copy executable: %w", err)
		}
	}

	fmt.Println("Installing systemd service...")

	unit := fmt.Sprintf(`[Unit]
Description=Cloudronix Device Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run --config %s
Restart=always
RestartSec=10
User=root

[Install]
WantedBy=multi-user.target
`, installPath, cfg.ConfigDir)

	unitPath := "/etc/systemd/system/cloudronix-agent.service"
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Reload systemd
	exec.Command("systemctl", "daemon-reload").Run()

	// Enable the service
	if output, err := exec.Command("systemctl", "enable", "cloudronix-agent").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to enable service: %s - %w", string(output), err)
	}

	// Start the service
	if output, err := exec.Command("systemctl", "start", "cloudronix-agent").CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to start service: %s\n", string(output))
	} else {
		fmt.Println("Service started successfully")
	}

	fmt.Println()
	fmt.Printf("Installed to: %s\n", installPath)
	fmt.Println("systemd service installed: cloudronix-agent")
	fmt.Println("Check status: systemctl status cloudronix-agent")
	return nil
}

// uninstallLinux removes the systemd service
func uninstallLinux() {
	exec.Command("systemctl", "stop", "cloudronix-agent").Run()
	exec.Command("systemctl", "disable", "cloudronix-agent").Run()
	os.Remove("/etc/systemd/system/cloudronix-agent.service")
	exec.Command("systemctl", "daemon-reload").Run()
	os.Remove("/usr/local/bin/cloudronix-agent")
}

// installDarwin installs the agent as a launchd service
func installDarwin(cfg *config.Config) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Copy to /usr/local/bin
	installPath := "/usr/local/bin/cloudronix-agent"
	if exePath != installPath {
		fmt.Printf("Copying agent to %s...\n", installPath)
		input, err := os.ReadFile(exePath)
		if err != nil {
			return fmt.Errorf("failed to read executable: %w", err)
		}
		if err := os.WriteFile(installPath, input, 0755); err != nil {
			return fmt.Errorf("failed to copy executable: %w", err)
		}
	}

	fmt.Println("Installing launchd service...")

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>io.cloudronix.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
        <string>--config</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/cloudronix-agent.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/cloudronix-agent.log</string>
</dict>
</plist>
`, installPath, cfg.ConfigDir)

	plistPath := "/Library/LaunchDaemons/io.cloudronix.agent.plist"
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}

	// Load the service
	if output, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to load service: %s - %w", string(output), err)
	}

	fmt.Println()
	fmt.Printf("Installed to: %s\n", installPath)
	fmt.Println("launchd service installed: io.cloudronix.agent")
	fmt.Println("Check status: launchctl list | grep cloudronix")
	return nil
}

// uninstallDarwin removes the launchd service
func uninstallDarwin() {
	plistPath := "/Library/LaunchDaemons/io.cloudronix.agent.plist"
	exec.Command("launchctl", "unload", plistPath).Run()
	os.Remove(plistPath)
	os.Remove("/usr/local/bin/cloudronix-agent")
}
