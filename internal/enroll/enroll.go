package enroll

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"

	"github.com/cloudronix/agent/internal/config"
	"github.com/cloudronix/agent/pkg/sysinfo"
)

// EnrollmentRequest is sent to the server
type EnrollmentRequest struct {
	Token        string `json:"token"`
	CSRPEM       string `json:"csr_pem"`
	DeviceType   string `json:"device_type"`
	OSName       string `json:"os_name,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	Architecture string `json:"architecture,omitempty"`
}

// EnrollmentResponse is received from the server
type EnrollmentResponse struct {
	DeviceID         string `json:"device_id"`
	CertificatePEM   string `json:"certificate_pem"`
	CACertificatePEM string `json:"ca_certificate_pem"`
	AgentURL         string `json:"agent_url"`
	// Server's Ed25519 public key for playbook signature verification (base64 encoded)
	ServerPublicKey []byte `json:"server_public_key,omitempty"`
}

// Enroll enrolls the device with the Cloudronix server
func Enroll(cfg *config.Config, token string) error {
	fmt.Println("Starting device enrollment...")

	// Check if already enrolled
	if cfg.IsEnrolled() {
		return fmt.Errorf("device is already enrolled (device ID: %s)\nUse 'cloudronix-agent uninstall' to remove existing enrollment", cfg.DeviceID)
	}

	// Generate ECDSA P-384 key pair
	fmt.Println("Generating device key pair...")
	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Create CSR
	fmt.Println("Creating certificate signing request...")
	csrPEM, err := createCSR(privateKey)
	if err != nil {
		return fmt.Errorf("failed to create CSR: %w", err)
	}

	// Gather system information
	sysInfo := sysinfo.Collect()

	// Determine device type
	deviceType := determineDeviceType()

	// Create enrollment request
	req := EnrollmentRequest{
		Token:        token,
		CSRPEM:       csrPEM,
		DeviceType:   deviceType,
		OSName:       sysInfo.OSName,
		OSVersion:    sysInfo.OSVersion,
		Hostname:     sysInfo.Hostname,
		Architecture: sysInfo.Architecture,
	}

	// Send enrollment request
	fmt.Printf("Enrolling with server at %s...\n", cfg.ServerURL)
	resp, err := sendEnrollmentRequest(cfg.ServerURL, req)
	if err != nil {
		return fmt.Errorf("enrollment failed: %w", err)
	}

	// Save credentials
	fmt.Println("Saving credentials...")
	if err := saveCredentials(cfg, privateKey, resp); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	// Update config
	cfg.DeviceID = resp.DeviceID
	if resp.AgentURL != "" {
		cfg.AgentURL = resp.AgentURL
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("Enrollment successful!")
	fmt.Printf("Device ID: %s\n", resp.DeviceID)
	fmt.Printf("Agent URL: %s\n", cfg.AgentURL)
	fmt.Println()
	fmt.Println("Run 'cloudronix-agent run' to start the agent, or")
	fmt.Println("Run 'cloudronix-agent install' to install as a system service.")

	return nil
}

// createCSR creates a Certificate Signing Request
func createCSR(privateKey *ecdsa.PrivateKey) (string, error) {
	hostname, _ := os.Hostname()

	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   hostname,
			Organization: []string{"Cloudronix Device"},
		},
		SignatureAlgorithm: x509.ECDSAWithSHA384,
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, privateKey)
	if err != nil {
		return "", err
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrDER,
	})

	return string(csrPEM), nil
}

// sendEnrollmentRequest sends the enrollment request to the server
func sendEnrollmentRequest(serverURL string, req EnrollmentRequest) (*EnrollmentResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := serverURL + "/api/v1/enroll"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var errResp struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(respBody, &errResp) == nil {
			return nil, fmt.Errorf("server error: %s - %s", errResp.Error, errResp.Message)
		}
		return nil, fmt.Errorf("server error: %s", httpResp.Status)
	}

	var resp EnrollmentResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// saveCredentials saves the private key and certificates
func saveCredentials(cfg *config.Config, privateKey *ecdsa.PrivateKey, resp *EnrollmentResponse) error {
	paths := cfg.Paths()

	// Save private key
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})
	if err := os.WriteFile(paths.PrivateKey, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Save device certificate
	if err := os.WriteFile(paths.Certificate, []byte(resp.CertificatePEM), 0644); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	// Save CA certificate
	if err := os.WriteFile(paths.CACert, []byte(resp.CACertificatePEM), 0644); err != nil {
		return fmt.Errorf("failed to write CA certificate: %w", err)
	}

	// Save server public key for playbook signature verification
	if len(resp.ServerPublicKey) > 0 {
		if err := cfg.SaveServerPublicKey(resp.ServerPublicKey); err != nil {
			fmt.Printf("Warning: failed to save server public key: %v\n", err)
			fmt.Println("Playbook execution will be disabled")
		} else {
			fmt.Println("Server public key saved - playbook execution enabled")
		}
	}

	return nil
}

// determineDeviceType detects the device type
func determineDeviceType() string {
	switch runtime.GOOS {
	case "android":
		return "mobile"
	case "darwin":
		// Could be laptop or desktop, default to laptop
		return "laptop"
	case "windows", "linux":
		// Check if it's a server OS
		if isServerOS() {
			return "server"
		}
		// Check for laptop indicators
		if isLaptop() {
			return "laptop"
		}
		return "desktop"
	default:
		return "other"
	}
}

// isServerOS checks if the OS appears to be a server edition
func isServerOS() bool {
	// This is a simple heuristic
	// Could be improved with more sophisticated detection
	if runtime.GOOS == "linux" {
		// Check for server indicators
		if _, err := os.Stat("/etc/systemd/system"); err == nil {
			// Has systemd, likely server or desktop
			return false
		}
	}
	return false
}

// isLaptop checks for laptop indicators
func isLaptop() bool {
	// Check for battery presence (basic heuristic)
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat("/sys/class/power_supply/BAT0"); err == nil {
			return true
		}
		if _, err := os.Stat("/sys/class/power_supply/BAT1"); err == nil {
			return true
		}
	case "windows":
		// Windows detection handled by gopsutil in sysinfo
		return false
	}
	return false
}
