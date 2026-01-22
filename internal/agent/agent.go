package agent

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudronix/agent/internal/client"
	"github.com/cloudronix/agent/internal/config"
	"github.com/cloudronix/agent/pkg/playbook"
	"github.com/cloudronix/agent/pkg/sysinfo"
)

const (
	agentVersion = "0.1.0"

	// Default job poll interval if not specified by server
	defaultJobPollInterval = 2 * time.Second
)

// Run starts the agent in foreground mode or as Windows Service
func Run(cfg *config.Config) error {
	if !cfg.IsEnrolled() {
		return fmt.Errorf("device is not enrolled\nRun 'cloudronix-agent enroll <token>' first")
	}

	// Check if running as Windows Service
	if IsWindowsService() {
		return RunAsService(cfg)
	}

	// Run in foreground mode
	return runAgent(cfg, nil)
}

// runAgent is the main agent loop
// stopCh is optional - if provided, agent will stop when it's closed (for Windows Service)
func runAgent(cfg *config.Config, stopCh <-chan struct{}) error {
	fmt.Printf("Starting Cloudronix Agent v%s\n", agentVersion)
	fmt.Printf("Device ID: %s\n", cfg.DeviceID)
	fmt.Printf("Agent URL: %s\n", cfg.AgentURL)

	// Create API client
	apiClient, err := client.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Get initial config from server
	fmt.Println("Connecting to server...")
	serverConfig, err := apiClient.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get server config: %w", err)
	}

	fmt.Printf("Connected! Device name: %s\n", serverConfig.DeviceName)

	// Update intervals from server
	heartbeatInterval := time.Duration(serverConfig.HeartbeatIntervalSeconds) * time.Second
	reportInterval := time.Duration(serverConfig.ReportIntervalSeconds) * time.Second

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Send initial report
	fmt.Println("Sending initial system report...")
	info := sysinfo.Collect()
	info.AgentVersion = agentVersion
	if err := apiClient.SendReport(info); err != nil {
		fmt.Printf("Warning: failed to send initial report: %v\n", err)
	}

	// Initialize job runner if server public key is available
	var jobRunner *JobRunner
	if cfg.HasServerPublicKey() {
		pubKeyBytes, err := cfg.LoadServerPublicKey()
		if err != nil {
			fmt.Printf("Warning: failed to load server public key: %v\n", err)
			fmt.Println("Playbook execution disabled - jobs will not be processed")
		} else if len(pubKeyBytes) == ed25519.PublicKeySize {
			jobRunner, err = NewJobRunner(JobRunnerConfig{
				Config:          cfg,
				APIClient:       apiClient,
				ServerPublicKey: ed25519.PublicKey(pubKeyBytes),
				OnJobStart: func(job *client.PendingJob) {
					fmt.Printf("[JOB] Starting job %s: %s\n", job.JobID, job.PlaybookName)
				},
				OnJobComplete: func(job *client.PendingJob, _ *playbook.ExecutionReport) {
					fmt.Printf("[JOB] Completed job %s\n", job.JobID)
				},
				OnJobError: func(job *client.PendingJob, err error) {
					fmt.Printf("[JOB] Job %s failed: %v\n", job.JobID, err)
				},
			})
			if err != nil {
				fmt.Printf("Warning: failed to create job runner: %v\n", err)
				fmt.Println("Playbook execution disabled")
			} else {
				fmt.Println("Playbook execution enabled")
			}
		} else {
			fmt.Printf("Warning: invalid server public key size (%d bytes, expected %d)\n",
				len(pubKeyBytes), ed25519.PublicKeySize)
		}
	} else {
		fmt.Println("Note: No server public key found - playbook execution disabled")
		fmt.Println("Re-enroll to enable playbook execution")
	}

	// Connect to WebSocket for real-time job notifications
	wsClient := client.NewWebSocketClient(cfg)
	if err := wsClient.Connect(ctx); err != nil {
		fmt.Printf("Warning: WebSocket connection failed: %v\n", err)
		fmt.Println("Falling back to polling mode")
	} else {
		defer wsClient.Close()
	}

	// Start heartbeat, report, and metrics loops
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	reportTicker := time.NewTicker(reportInterval)
	// Metrics collected every 5 seconds for real-time monitoring
	metricsTicker := time.NewTicker(5 * time.Second)
	// Fallback polling (in case WebSocket is down)
	jobPollTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()
	defer reportTicker.Stop()
	defer metricsTicker.Stop()
	defer jobPollTicker.Stop()

	fmt.Printf("Agent running (heartbeat: %v, report: %v, metrics: 5s)\n", heartbeatInterval, reportInterval)
	fmt.Println("Press Ctrl+C to stop")

	// Initial job check
	if jobRunner != nil {
		go func() {
			if err := jobRunner.RunOnce(ctx); err != nil {
				fmt.Printf("Initial job check failed: %v\n", err)
			}
		}()
	}

	// Handle Windows Service stop signal
	if stopCh != nil {
		go func() {
			<-stopCh
			fmt.Println("Service stop signal received")
			cancel()
		}()
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Agent stopped")
			return nil

		case <-wsClient.Done():
			// WebSocket disconnected, try to reconnect
			fmt.Println("WebSocket disconnected, reconnecting...")
			time.Sleep(2 * time.Second)
			if err := wsClient.Connect(ctx); err != nil {
				fmt.Printf("Reconnect failed: %v\n", err)
			}

		case notification := <-wsClient.JobChannel():
			// Real-time job notification - execute immediately!
			if jobRunner != nil {
				fmt.Printf(">>> Executing job immediately: %s\n", notification.PlaybookName)
				go func() {
					if err := jobRunner.RunOnce(ctx); err != nil {
						fmt.Printf("Job execution failed: %v\n", err)
					}
				}()
			}

		case <-heartbeatTicker.C:
			if _, err := apiClient.SendHeartbeat(); err != nil {
				fmt.Printf("Heartbeat failed: %v\n", err)
			}

		case <-reportTicker.C:
			info := sysinfo.Collect()
			info.AgentVersion = agentVersion
			if err := apiClient.SendReport(info); err != nil {
				fmt.Printf("Report failed: %v\n", err)
			}

		case <-metricsTicker.C:
			metrics := sysinfo.CollectMetrics()
			tempStr := "N/A"
			if metrics.Temperature != nil {
				tempStr = fmt.Sprintf("%.1f°C", *metrics.Temperature)
			}
			fmt.Printf("[Metrics] CPU: %.1f%%, RAM: %.1f%%, Temp: %s, Processes: %d\n",
				metrics.CPU.UsagePercent, metrics.Memory.UsagePercent, tempStr, len(metrics.TopProcesses))
			if err := apiClient.SendMetrics(metrics); err != nil {
				fmt.Printf("[Metrics] Send failed: %v\n", err)
			} else {
				fmt.Println("[Metrics] Sent successfully")
			}

		case <-jobPollTicker.C:
			// Fallback polling in case WebSocket missed something
			if jobRunner != nil {
				if err := jobRunner.RunOnce(ctx); err != nil {
					// Silently ignore poll errors
				}
			}
		}
	}
}

// Status displays the current agent status
func Status(cfg *config.Config) error {
	fmt.Println("Cloudronix Agent Status")
	fmt.Println("========================")

	if !cfg.IsEnrolled() {
		fmt.Println("Status: NOT ENROLLED")
		fmt.Println()
		fmt.Println("Run 'cloudronix-agent enroll <token>' to enroll this device")
		return nil
	}

	fmt.Println("Status: ENROLLED")
	fmt.Printf("Device ID: %s\n", cfg.DeviceID)
	fmt.Printf("Server URL: %s\n", cfg.ServerURL)
	fmt.Printf("Agent URL: %s\n", cfg.AgentURL)
	fmt.Printf("Config Dir: %s\n", cfg.ConfigDir)

	paths := cfg.Paths()
	fmt.Println()
	fmt.Println("Credentials:")

	if _, err := os.Stat(paths.Certificate); err == nil {
		fmt.Println("  Certificate: OK")
	} else {
		fmt.Println("  Certificate: MISSING")
	}

	if _, err := os.Stat(paths.PrivateKey); err == nil {
		fmt.Println("  Private Key: OK")
	} else {
		fmt.Println("  Private Key: MISSING")
	}

	if _, err := os.Stat(paths.CACert); err == nil {
		fmt.Println("  CA Certificate: OK")
	} else {
		fmt.Println("  CA Certificate: MISSING")
	}

	// Try to connect
	fmt.Println()
	fmt.Println("Testing connection...")

	apiClient, err := client.NewClient(cfg)
	if err != nil {
		fmt.Printf("Connection: FAILED (%v)\n", err)
		return nil
	}

	if _, err := apiClient.GetConfig(); err != nil {
		fmt.Printf("Connection: FAILED (%v)\n", err)
	} else {
		fmt.Println("Connection: OK")
	}

	return nil
}
