package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudronix/agent/internal/agent"
	"github.com/cloudronix/agent/internal/config"
	"github.com/cloudronix/agent/internal/enroll"
)

var (
	version = "0.1.0"
	cfgFile string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cloudronix-agent",
		Short: "Cloudronix Device Agent",
		Long: `Cloudronix Agent connects your device to the Cloudronix cloud management platform.

It provides secure communication via mTLS with quantum-resistant key exchange,
and reports system metrics to the central dashboard.`,
		Version: version,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config directory (default: ~/.cloudronix)")

	// Add commands
	rootCmd.AddCommand(enrollCmd())
	rootCmd.AddCommand(runCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(installCmd())
	rootCmd.AddCommand(uninstallCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func enrollCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enroll <token>",
		Short: "Enroll this device with Cloudronix",
		Long: `Enroll this device using a one-time enrollment token.

Generate the token from the Cloudronix web dashboard under Devices > Add Device.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token := args[0]

			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			return enroll.Enroll(cfg, token)
		},
	}

	return cmd
}

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the agent in foreground",
		Long:  `Run the Cloudronix agent in the foreground. Use 'install' to run as a system service.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			return agent.Run(cfg)
		},
	}

	return cmd
}

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show agent status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			return agent.Status(cfg)
		},
	}

	return cmd
}

func installCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install as system service",
		Long: `Install the Cloudronix agent as a system service.

On Windows, this installs a Windows Service.
On Linux, this creates a systemd unit.
On macOS, this creates a launchd plist.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			return agent.Install(cfg)
		},
	}

	return cmd
}

func uninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove system service and credentials",
		Long: `Uninstall the Cloudronix agent service and remove stored credentials.

This will stop the service, remove it from the system, and delete the configuration directory.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			return agent.Uninstall(cfg)
		},
	}

	return cmd
}
