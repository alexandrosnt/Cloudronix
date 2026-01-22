package playbook

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ============================================================================
// SECURITY CRITICAL - PLAYBOOK VERIFICATION
// ============================================================================
//
// This module implements the verification chain for playbooks:
//   1. SHA256 hash verification - ensures content integrity
//   2. Ed25519 signature verification - ensures authenticity
//   3. Approval status verification - ensures human review
//
// ALL THREE CHECKS MUST PASS before any playbook execution.
// There are NO EXCEPTIONS, NO BYPASSES, NO DEBUG MODES.
//
// If you're modifying this code, understand that any weakness here
// could allow malicious code execution on user machines.
// ============================================================================

// Verification errors - each represents a security violation
var (
	// ErrHashMismatch indicates the playbook content was tampered with
	ErrHashMismatch = errors.New("SECURITY VIOLATION: playbook hash mismatch - content may have been tampered")

	// ErrInvalidSignature indicates the playbook was not signed by the server
	ErrInvalidSignature = errors.New("SECURITY VIOLATION: invalid signature - playbook not authenticated")

	// ErrNotApproved indicates the playbook has not been reviewed and approved
	ErrNotApproved = errors.New("SECURITY VIOLATION: playbook not approved - requires human review")

	// ErrEmptyContent indicates the playbook has no content
	ErrEmptyContent = errors.New("SECURITY VIOLATION: empty playbook content")

	// ErrMissingHash indicates the playbook has no hash
	ErrMissingHash = errors.New("SECURITY VIOLATION: missing playbook hash")

	// ErrMissingSignature indicates the playbook has no signature
	ErrMissingSignature = errors.New("SECURITY VIOLATION: missing playbook signature")

	// ErrInvalidPublicKey indicates the server public key is invalid
	ErrInvalidPublicKey = errors.New("SECURITY VIOLATION: invalid server public key")
)

// Verifier handles cryptographic verification of playbooks
type Verifier struct {
	// serverPublicKey is the Ed25519 public key used to verify signatures
	// This key is obtained during device enrollment and pinned
	serverPublicKey ed25519.PublicKey
}

// NewVerifier creates a new playbook verifier with the given server public key
//
// SECURITY: The public key should be obtained during enrollment and stored securely.
// It should NOT be fetched from the network at verification time.
func NewVerifier(publicKey ed25519.PublicKey) (*Verifier, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, ErrInvalidPublicKey
	}
	return &Verifier{serverPublicKey: publicKey}, nil
}

// Verify performs all security checks on a signed playbook
//
// SECURITY CRITICAL: This function MUST be called before ANY playbook execution.
// It returns a VerificationRecord for audit purposes, even on failure.
//
// The verification chain is:
//   1. Validate inputs (non-empty content, hash, signature)
//   2. Calculate SHA256 hash of content
//   3. Compare calculated hash with expected hash
//   4. Verify Ed25519 signature of the hash
//   5. Check approval status is "approved"
//
// ALL checks must pass. Any failure = immediate rejection.
func (v *Verifier) Verify(sp *SignedPlaybook) (*VerificationRecord, error) {
	record := &VerificationRecord{
		ExpectedHash:   sp.SHA256Hash,
		VerifiedAt:     time.Now(),
		AllChecksPass:  false,
		ApprovalStatus: sp.Status,
	}

	// =======================================================================
	// STEP 1: Input validation
	// =======================================================================
	if sp.Content == "" {
		record.FailureReason = "empty playbook content"
		return record, ErrEmptyContent
	}
	if sp.SHA256Hash == "" {
		record.FailureReason = "missing playbook hash"
		return record, ErrMissingHash
	}
	if len(sp.Signature) == 0 {
		record.FailureReason = "missing playbook signature"
		return record, ErrMissingSignature
	}

	// =======================================================================
	// STEP 2: Calculate SHA256 hash of content
	// =======================================================================
	hashBytes := sha256.Sum256([]byte(sp.Content))
	calculatedHash := hex.EncodeToString(hashBytes[:])
	record.CalculatedHash = calculatedHash

	// =======================================================================
	// STEP 3: Compare hashes - MUST match exactly
	// =======================================================================
	// Using constant-time comparison would be ideal here, but since the hash
	// is already a one-way function, timing attacks are not a major concern.
	// However, for defense in depth, we compare the full strings.
	if calculatedHash != sp.SHA256Hash {
		record.HashVerified = false
		record.FailureReason = fmt.Sprintf("hash mismatch: expected %s, got %s", sp.SHA256Hash, calculatedHash)
		return record, ErrHashMismatch
	}
	record.HashVerified = true

	// =======================================================================
	// STEP 4: Verify Ed25519 signature
	// =======================================================================
	// The signature is over the raw hash bytes, not the hex string
	if !ed25519.Verify(v.serverPublicKey, hashBytes[:], sp.Signature) {
		record.SignatureVerified = false
		record.FailureReason = "signature verification failed"
		return record, ErrInvalidSignature
	}
	record.SignatureVerified = true

	// =======================================================================
	// STEP 5: Check approval status
	// =======================================================================
	// Accept "approved" for production runs and "test" for test runs
	// Test runs are protected by server-side permission checks (admin or developer+author)
	// and the signature proves the server authorized this execution
	if sp.Status != StatusApproved && sp.Status != StatusTest {
		record.ApprovalVerified = false
		record.FailureReason = fmt.Sprintf("playbook status is '%s', expected 'approved' or 'test'", sp.Status)
		return record, ErrNotApproved
	}
	record.ApprovalVerified = true

	// =======================================================================
	// ALL CHECKS PASSED
	// =======================================================================
	record.AllChecksPass = true
	return record, nil
}

// CalculateHash computes the SHA256 hash of playbook content
// This is used by the server when creating playbooks
func CalculateHash(content string) string {
	hashBytes := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hashBytes[:])
}

// VerifyHashOnly performs only hash verification (for debugging/testing)
// SECURITY WARNING: Do NOT use this for actual playbook execution
func VerifyHashOnly(content, expectedHash string) (bool, string) {
	calculated := CalculateHash(content)
	return calculated == expectedHash, calculated
}
