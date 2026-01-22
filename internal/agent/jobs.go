package agent

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"sync"
	"time"

	"github.com/cloudronix/agent/internal/client"
	"github.com/cloudronix/agent/internal/config"
	"github.com/cloudronix/agent/pkg/playbook"
	"github.com/cloudronix/agent/pkg/playbook/actions"
)

// JobRunner handles polling for and executing playbook jobs
type JobRunner struct {
	cfg       *config.Config
	apiClient *client.Client

	// Executor with registered handlers
	executor *playbook.Executor

	// Server's public key for signature verification (obtained during enrollment)
	serverPublicKey ed25519.PublicKey

	// Mutex to prevent concurrent job execution
	mu        sync.Mutex
	isRunning bool

	// Callback for job events
	onJobStart    func(job *client.PendingJob)
	onJobComplete func(job *client.PendingJob, report *playbook.ExecutionReport)
	onJobError    func(job *client.PendingJob, err error)
}

// JobRunnerConfig holds configuration for the job runner
type JobRunnerConfig struct {
	Config          *config.Config
	APIClient       *client.Client
	ServerPublicKey ed25519.PublicKey

	// Optional callbacks
	OnJobStart    func(job *client.PendingJob)
	OnJobComplete func(job *client.PendingJob, report *playbook.ExecutionReport)
	OnJobError    func(job *client.PendingJob, err error)
}

// NewJobRunner creates a new job runner
func NewJobRunner(cfg JobRunnerConfig) (*JobRunner, error) {
	if len(cfg.ServerPublicKey) == 0 {
		return nil, fmt.Errorf("server public key is required for playbook verification")
	}

	// Create executor with the server's public key
	executor, err := playbook.NewExecutor(playbook.ExecutorConfig{
		ServerPublicKey: cfg.ServerPublicKey,
		DeviceID:        cfg.Config.DeviceID,
		OnProgress: func(taskName string, status playbook.TaskStatus) {
			fmt.Printf("  Task '%s': %s\n", taskName, status)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	// Register all action handlers
	actions.RegisterAllHandlers(executor)

	return &JobRunner{
		cfg:             cfg.Config,
		apiClient:       cfg.APIClient,
		executor:        executor,
		serverPublicKey: cfg.ServerPublicKey,
		onJobStart:      cfg.OnJobStart,
		onJobComplete:   cfg.OnJobComplete,
		onJobError:      cfg.OnJobError,
	}, nil
}

// CheckAndRunJobs checks for pending jobs and executes them
// Returns the number of jobs executed
func (r *JobRunner) CheckAndRunJobs(ctx context.Context) (int, error) {
	// Prevent concurrent execution
	r.mu.Lock()
	if r.isRunning {
		r.mu.Unlock()
		return 0, nil
	}
	r.isRunning = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.isRunning = false
		r.mu.Unlock()
	}()

	// Fetch pending jobs
	jobs, err := r.apiClient.GetPendingJobs()
	if err != nil {
		return 0, fmt.Errorf("failed to fetch pending jobs: %w", err)
	}

	if len(jobs) == 0 {
		return 0, nil
	}

	executed := 0
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			return executed, ctx.Err()
		default:
		}

		if err := r.executeJob(ctx, &job); err != nil {
			fmt.Printf("Job %s failed: %v\n", job.JobID, err)
			if r.onJobError != nil {
				r.onJobError(&job, err)
			}
		}
		executed++
	}

	return executed, nil
}

// executeJob executes a single job
func (r *JobRunner) executeJob(ctx context.Context, job *client.PendingJob) error {
	fmt.Printf("\n========================================\n")
	fmt.Printf("Executing job: %s\n", job.JobID)
	fmt.Printf("Playbook: %s (%s)\n", job.PlaybookName, job.PlaybookID)
	if job.IsTestRun {
		fmt.Println("Mode: TEST RUN")
	}
	fmt.Printf("========================================\n")

	if r.onJobStart != nil {
		r.onJobStart(job)
	}

	// Mark job as started on the server
	if err := r.apiClient.MarkJobStarted(job.JobID); err != nil {
		return fmt.Errorf("failed to mark job started: %w", err)
	}

	// Fetch the playbook content
	var payload *client.SignedPlaybookPayload
	var err error

	if job.IsTestRun {
		payload, err = r.apiClient.GetTestPlaybook(job.JobID, job.PlaybookID)
	} else {
		payload, err = r.apiClient.GetPlaybook(job.PlaybookID)
	}

	if err != nil {
		return r.reportJobError(job, fmt.Errorf("failed to fetch playbook: %w", err))
	}

	// Convert to SignedPlaybook for execution
	signedPlaybook := payload.ToSignedPlaybook()

	// Execute the playbook (verification happens inside executor)
	report, execErr := r.executor.Execute(ctx, signedPlaybook)

	// Always submit the report, even if execution failed
	if submitErr := r.apiClient.SubmitExecutionReport(job.JobID, report); submitErr != nil {
		fmt.Printf("Warning: failed to submit execution report: %v\n", submitErr)
	}

	// Print execution summary
	fmt.Printf("\nExecution Summary:\n")
	fmt.Printf("  Status: %s\n", report.Status)
	fmt.Printf("  Duration: %s\n", report.TotalDuration)
	fmt.Printf("  Tasks: %d completed, %d failed, %d skipped\n",
		report.TasksCompleted, report.TasksFailed, report.TasksSkipped)
	if report.ErrorMessage != "" {
		fmt.Printf("  Error: %s\n", report.ErrorMessage)
	}
	fmt.Printf("========================================\n\n")

	if r.onJobComplete != nil {
		r.onJobComplete(job, report)
	}

	return execErr
}

// reportJobError creates and submits an error report for a job
func (r *JobRunner) reportJobError(job *client.PendingJob, err error) error {
	report := &playbook.ExecutionReport{
		PlaybookID:   job.PlaybookID,
		PlaybookName: job.PlaybookName,
		DeviceID:     r.cfg.DeviceID,
		Status:       "failed",
		StartTime:    time.Now(),
		EndTime:      time.Now(),
		ErrorMessage: err.Error(),
		Verification: playbook.VerificationRecord{
			AllChecksPass: false,
			VerifiedAt:    time.Now(),
			FailureReason: err.Error(),
		},
	}
	report.TotalDuration = "0s"

	if submitErr := r.apiClient.SubmitExecutionReport(job.JobID, report); submitErr != nil {
		fmt.Printf("Warning: failed to submit error report: %v\n", submitErr)
	}

	return err
}

// RunOnce checks for and executes any pending jobs once
func (r *JobRunner) RunOnce(ctx context.Context) error {
	count, err := r.CheckAndRunJobs(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		fmt.Printf("Executed %d jobs\n", count)
	}
	return nil
}
