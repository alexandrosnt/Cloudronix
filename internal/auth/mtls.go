package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"

	"github.com/cloudronix/agent/internal/config"
)

// Credentials holds the device's certificate and private key for authentication
type Credentials struct {
	CertificateDER []byte          // DER-encoded certificate
	PrivateKey     *ecdsa.PrivateKey
	Fingerprint    string
}

// LoadCredentials loads the device certificate and private key
func LoadCredentials(cfg *config.Config) (*Credentials, error) {
	paths := cfg.Paths()

	// Load certificate
	certPEM, err := os.ReadFile(paths.Certificate)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Load private key
	keyPEM, err := os.ReadFile(paths.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	// Try parsing as PKCS8 first (common format), then EC private key
	var privateKey *ecdsa.PrivateKey
	if key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err == nil {
		var ok bool
		privateKey, ok = key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not ECDSA")
		}
	} else if key, err := x509.ParseECPrivateKey(keyBlock.Bytes); err == nil {
		privateKey = key
	} else {
		return nil, fmt.Errorf("failed to parse private key")
	}

	// Calculate fingerprint
	fingerprint := sha256.Sum256(cert.Raw)

	return &Credentials{
		CertificateDER: cert.Raw,
		PrivateKey:     privateKey,
		Fingerprint:    hex.EncodeToString(fingerprint[:]),
	}, nil
}

// CertificateBase64 returns the certificate in base64-encoded DER format
func (c *Credentials) CertificateBase64() string {
	return base64.StdEncoding.EncodeToString(c.CertificateDER)
}

// Sign signs a message with the private key using ECDSA with SHA-256
// Returns the signature in base64 format
// Note: We hash the message here because Go's ecdsa.SignASN1 expects a pre-hashed value
// The Rust backend will also hash the message, so both sides must use the same approach
func (c *Credentials) Sign(message string) (string, error) {
	// Hash the message with SHA-256 (required by ecdsa.SignASN1)
	hash := sha256.Sum256([]byte(message))

	// Sign the hash with ECDSA
	signature, err := ecdsa.SignASN1(rand.Reader, c.PrivateKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign: %w", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

// NewMTLSClient creates an HTTP client for agent communication
// For Cloudflare mode (https://), uses system CAs - auth is via headers
// For direct mTLS mode, would use internal CA + client cert (not implemented yet)
func NewMTLSClient(cfg *config.Config) (*http.Client, error) {
	// For both http:// and https:// URLs going through Cloudflare,
	// we use a standard HTTP client. Authentication is handled via
	// X-Client-Certificate, X-Client-Timestamp, X-Client-Signature headers
	// (added by addAuthHeaders in api.go)

	// Use system root CAs for TLS verification (Cloudflare's cert is trusted)
	return &http.Client{}, nil
}

// GetCertificateFingerprint returns the SHA-256 fingerprint of the device certificate
func GetCertificateFingerprint(cfg *config.Config) (string, error) {
	paths := cfg.Paths()

	certPEM, err := os.ReadFile(paths.Certificate)
	if err != nil {
		return "", fmt.Errorf("failed to read certificate: %w", err)
	}

	// Parse the certificate to get fingerprint
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Calculate SHA-256 fingerprint
	fingerprint := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(fingerprint[:]), nil
}

// Ensure PrivateKey implements crypto.Signer (compile-time check)
var _ crypto.Signer = (*ecdsa.PrivateKey)(nil)
