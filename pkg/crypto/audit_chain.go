package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
)

// GenesisHash is the hardcoded initial hash seed for the root of every tenant's audit log chain.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// CalculateRecordHash computes the HMAC-SHA256 signature for an audit log entry.
func CalculateRecordHash(secretKey []byte, prevHash string, event *domain.AuditEvent) (string, error) {
	if len(secretKey) == 0 {
		return "", errors.New("audit HMAC secret key cannot be empty")
	}
	if prevHash == "" {
		prevHash = GenesisHash
	}

	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		return "", fmt.Errorf("failed to marshal audit details for hashing: %w", err)
	}

	// Canonical payload representation:
	// prevHash | tenant_id | actor_id | actor_type | event_action | target_type | target_id | details_json | timestamp_utc
	payload := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s",
		prevHash,
		event.TenantID.String(),
		event.ActorID,
		string(event.ActorType),
		event.EventAction,
		event.TargetType,
		event.TargetID,
		string(detailsJSON),
		event.CreatedAt.UTC().Format(time.RFC3339Nano),
	)

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyAuditChain checks whether a sequence of audit events maintains cryptographic integrity.
// Returns (isValid, tamperedIndex, error). If valid, tamperedIndex is -1.
func VerifyAuditChain(secretKey []byte, records []domain.AuditEvent) (bool, int, error) {
	if len(records) == 0 {
		return true, -1, nil
	}

	expectedPrevHash := GenesisHash

	for i, record := range records {
		if record.PreviousHash != expectedPrevHash {
			return false, i, fmt.Errorf("audit chain broken at index %d: previous hash '%s' does not match expected '%s'",
				i, record.PreviousHash, expectedPrevHash)
		}

		expectedHash, err := CalculateRecordHash(secretKey, record.PreviousHash, &record)
		if err != nil {
			return false, i, fmt.Errorf("failed to compute hash for record %d: %w", i, err)
		}

		if record.RecordHash != expectedHash {
			return false, i, fmt.Errorf("tamper detected at index %d: record hash '%s' does not match computed '%s'",
				i, record.RecordHash, expectedHash)
		}

		expectedPrevHash = record.RecordHash
	}

	return true, -1, nil
}
