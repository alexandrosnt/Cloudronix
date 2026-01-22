package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cloudronix/agent/internal/auth"
	"github.com/cloudronix/agent/internal/config"
	"github.com/cloudronix/agent/pkg/playbook"
	"github.com/cloudronix/agent/pkg/sysinfo"
)

// Client is the API client for communicating with the Cloudronix server
type Client struct {
	cfg         *config.Config
	httpClient  *http.Client
	credentials *auth.Credentials
}

// AgentConfig is the configuration received from the server
type AgentConfig struct {
	DeviceID                 string `json:"device_id"`
	DeviceName               string `json:"device_name"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
	ReportIntervalSeconds    int    `json:"report_interval_seconds"`
}

// HeartbeatResponse is the response from a heartbeat request
type HeartbeatResponse struct {
	Ack        bool      `json:"ack"`
	ServerTime time.Time `json:"server_time"`
}

// NewClient creates a new API client with mTLS authentication
func NewClient(cfg *config.Config) (*Client, error) {
	httpClient, err := auth.NewMTLSClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create mTLS client: %w", err)
	}

	// Load credentials for header-based authentication (Cloudflare mode)
	credentials, err := auth.LoadCredentials(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials: %w", err)
	}

	return &Client{
		cfg:         cfg,
		httpClient:  httpClient,
		credentials: credentials,
	}, nil
}

// GetConfig fetches the device configuration from the server
func (c *Client) GetConfig() (*AgentConfig, error) {
	url := c.cfg.AgentURL + "/agent/config"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var cfg AgentConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// HeartbeatRequest is sent to the server
type HeartbeatRequest struct {
	Status    string `json:"status"`
	LatencyMs *int64 `json:"latency_ms,omitempty"`
}

// lastLatencyMs stores the previous heartbeat latency for the next request
var lastLatencyMs *int64

// SendHeartbeat sends a heartbeat to the server and measures latency
func (c *Client) SendHeartbeat() (*HeartbeatResponse, error) {
	url := c.cfg.AgentURL + "/agent/heartbeat"

	// Include previous latency in request
	heartbeatReq := HeartbeatRequest{
		Status:    "ok",
		LatencyMs: lastLatencyMs,
	}
	body, _ := json.Marshal(heartbeatReq)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeaders(req)

	// Measure round-trip time
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	latency := time.Since(start).Milliseconds()
	lastLatencyMs = &latency

	if err != nil {
		return nil, fmt.Errorf("failed to send heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var heartbeat HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&heartbeat); err != nil {
		return nil, fmt.Errorf("failed to parse heartbeat response: %w", err)
	}

	return &heartbeat, nil
}

// SendReport sends a system report to the server
func (c *Client) SendReport(info *sysinfo.SystemInfo) error {
	url := c.cfg.AgentURL + "/agent/report"

	body, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to serialize report: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

// SendMetrics sends real-time metrics to the server
func (c *Client) SendMetrics(metrics *sysinfo.Metrics) error {
	url := c.cfg.AgentURL + "/agent/metrics"

	body, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("failed to serialize metrics: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

// addAuthHeaders adds device authentication headers to the request
// These headers provide certificate-based authentication through Cloudflare
// The server verifies: certificate validity, signature (proves private key possession)
func (c *Client) addAuthHeaders(req *http.Request) {
	// Legacy headers for backwards compatibility
	req.Header.Set("X-Device-ID", c.cfg.DeviceID)
	req.Header.Set("X-Cert-Fingerprint", c.credentials.Fingerprint)

	// New certificate-based authentication headers for Cloudflare mode
	// 1. Certificate (base64-encoded DER)
	req.Header.Set("X-Client-Certificate", c.credentials.CertificateBase64())

	// 2. Timestamp (Unix seconds) - for replay protection
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Client-Timestamp", timestamp)

	// 3. Signature of "{timestamp}:{method}:{path}" - proves private key possession
	message := fmt.Sprintf("%s:%s:%s", timestamp, req.Method, req.URL.Path)
	if signature, err := c.credentials.Sign(message); err == nil {
		req.Header.Set("X-Client-Signature", signature)
	}
}

// parseError extracts error information from a response
func (c *Client) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}

	if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
		return fmt.Errorf("server error (%d): %s - %s", resp.StatusCode, errResp.Error, errResp.Message)
	}

	return fmt.Errorf("server error: %s", resp.Status)
}

// ============================================================================
// Job API - Playbook Execution Management
// ============================================================================

// PendingJob represents a job waiting to be executed
type PendingJob struct {
	JobID        string    `json:"job_id"`
	PlaybookID   string    `json:"playbook_id"`
	PlaybookName string    `json:"playbook_name"`
	Priority     int       `json:"priority"`
	IsTestRun    bool      `json:"is_test_run"`
	CreatedAt    time.Time `json:"created_at"`
}

// SignedPlaybookPayload is the response from the server containing a signed playbook
type SignedPlaybookPayload struct {
	PlaybookID   string    `json:"playbook_id"`
	Name         string    `json:"name"`
	Content      string    `json:"content"`
	SHA256Hash   string    `json:"sha256_hash"`
	Signature    []byte    `json:"signature"`
	Status       string    `json:"status"`
	ApprovedBy   *string   `json:"approved_by,omitempty"`
	ApprovedAt   time.Time `json:"approved_at,omitempty"`
	ServerPubKey []byte    `json:"server_public_key"`
	IsTestRun    bool      `json:"is_test_run"`
}

// ToSignedPlaybook converts the payload to the playbook package's SignedPlaybook type
func (p *SignedPlaybookPayload) ToSignedPlaybook() *playbook.SignedPlaybook {
	return &playbook.SignedPlaybook{
		PlaybookID: p.PlaybookID,
		Content:    p.Content,
		SHA256Hash: p.SHA256Hash,
		Signature:  p.Signature,
		Status:     p.Status,
		ApprovedAt: p.ApprovedAt,
	}
}

// GetPendingJobs fetches all pending jobs for this device
func (c *Client) GetPendingJobs() ([]PendingJob, error) {
	url := c.cfg.AgentURL + "/agent/jobs"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending jobs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var jobs []PendingJob
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, fmt.Errorf("failed to parse jobs: %w", err)
	}

	return jobs, nil
}

// MarkJobStarted tells the server that this job has started execution
func (c *Client) MarkJobStarted(jobID string) error {
	url := fmt.Sprintf("%s/agent/jobs/%s/start", c.cfg.AgentURL, jobID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to mark job started: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

// GetPlaybook fetches a signed playbook for execution
func (c *Client) GetPlaybook(playbookID string) (*SignedPlaybookPayload, error) {
	url := fmt.Sprintf("%s/agent/playbooks/%s", c.cfg.AgentURL, playbookID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get playbook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var payload SignedPlaybookPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to parse playbook: %w", err)
	}

	return &payload, nil
}

// GetTestPlaybook fetches a playbook for test execution (requires test job)
func (c *Client) GetTestPlaybook(jobID, playbookID string) (*SignedPlaybookPayload, error) {
	url := fmt.Sprintf("%s/agent/jobs/%s/playbooks/%s/test", c.cfg.AgentURL, jobID, playbookID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get test playbook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var payload SignedPlaybookPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to parse playbook: %w", err)
	}

	return &payload, nil
}

// SubmitExecutionReport sends the execution report to the server
func (c *Client) SubmitExecutionReport(jobID string, report *playbook.ExecutionReport) error {
	url := fmt.Sprintf("%s/agent/jobs/%s/report", c.cfg.AgentURL, jobID)

	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to serialize report: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to submit report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}
