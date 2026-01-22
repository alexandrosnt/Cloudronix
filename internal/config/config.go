package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Config holds the agent configuration
type Config struct {
	// Directory where config and credentials are stored
	ConfigDir string `json:"-"`

	// Server URLs
	ServerURL string `json:"server_url"` // Main API (enrollment)
	AgentURL  string `json:"agent_url"`  // mTLS agent API

	// Device identity (set after enrollment)
	DeviceID string `json:"device_id,omitempty"`

	// Intervals
	HeartbeatInterval int `json:"heartbeat_interval"` // seconds
	ReportInterval    int `json:"report_interval"`    // seconds
}

// Paths returns important file paths
type Paths struct {
	Config          string // config.json
	Certificate     string // device.crt
	PrivateKey      string // device.key
	CACert          string // ca.crt
	ServerPublicKey string // server.pub (Ed25519 for playbook verification)
}

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		ServerURL:         "https://cloudronix.alexandrosntonas.com",
		AgentURL:          "https://agent.alexandrosntonas.com",
		HeartbeatInterval: 60,
		ReportInterval:    300,
	}
}

// Load loads configuration from the specified directory or default
func Load(configDir string) (*Config, error) {
	if configDir == "" {
		configDir = defaultConfigDir()
	}

	cfg := DefaultConfig()
	cfg.ConfigDir = configDir

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Try to load existing config
	configPath := filepath.Join(configDir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// First run - check environment variables
			if url := os.Getenv("CLOUDRONIX_SERVER_URL"); url != "" {
				cfg.ServerURL = url
			}
			if url := os.Getenv("CLOUDRONIX_AGENT_URL"); url != "" {
				cfg.AgentURL = url
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// Save saves the configuration to disk
func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	configPath := filepath.Join(c.ConfigDir, "config.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// Paths returns the file paths for certificates and config
func (c *Config) Paths() Paths {
	return Paths{
		Config:          filepath.Join(c.ConfigDir, "config.json"),
		Certificate:     filepath.Join(c.ConfigDir, "device.crt"),
		PrivateKey:      filepath.Join(c.ConfigDir, "device.key"),
		CACert:          filepath.Join(c.ConfigDir, "ca.crt"),
		ServerPublicKey: filepath.Join(c.ConfigDir, "server.pub"),
	}
}

// IsEnrolled returns true if the device has been enrolled
func (c *Config) IsEnrolled() bool {
	if c.DeviceID == "" {
		return false
	}

	paths := c.Paths()
	if _, err := os.Stat(paths.Certificate); os.IsNotExist(err) {
		return false
	}
	if _, err := os.Stat(paths.PrivateKey); os.IsNotExist(err) {
		return false
	}

	return true
}

// LoadServerPublicKey loads the server's Ed25519 public key from disk
func (c *Config) LoadServerPublicKey() ([]byte, error) {
	paths := c.Paths()
	data, err := os.ReadFile(paths.ServerPublicKey)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("server public key not found - playbook execution disabled")
		}
		return nil, fmt.Errorf("failed to read server public key: %w", err)
	}
	return data, nil
}

// SaveServerPublicKey saves the server's Ed25519 public key to disk
func (c *Config) SaveServerPublicKey(key []byte) error {
	paths := c.Paths()
	if err := os.WriteFile(paths.ServerPublicKey, key, 0600); err != nil {
		return fmt.Errorf("failed to write server public key: %w", err)
	}
	return nil
}

// HasServerPublicKey returns true if the server public key exists
func (c *Config) HasServerPublicKey() bool {
	paths := c.Paths()
	_, err := os.Stat(paths.ServerPublicKey)
	return err == nil
}

// defaultConfigDir returns the default configuration directory
func defaultConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		// Use %LOCALAPPDATA%\Cloudronix
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			return filepath.Join(localAppData, "Cloudronix")
		}
		return filepath.Join(os.Getenv("USERPROFILE"), ".cloudronix")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "Cloudronix")
	default:
		// Linux and others
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cloudronix")
	}
}
