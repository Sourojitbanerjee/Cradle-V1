package crypto

import (
	"strings"
	"testing"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestGenerateExternalID(t *testing.T) {
	tenantSlug := "acme-corp"
	extID, err := GenerateExternalID(tenantSlug)
	if err != nil {
		t.Fatalf("unexpected error generating ExternalID: %v", err)
	}

	if !strings.HasPrefix(extID, "cradle-acme-corp-") {
		t.Errorf("expected ExternalID to start with 'cradle-acme-corp-', got '%s'", extID)
	}

	// Should have high entropy (at least 60 characters)
	if len(extID) < 60 {
		t.Errorf("expected ExternalID to have high length/entropy, got length %d", len(extID))
	}

	// Subsequent calls must generate distinct values
	extID2, _ := GenerateExternalID(tenantSlug)
	if extID == extID2 {
		t.Errorf("expected unique external IDs, got duplicate: %s", extID)
	}
}

func TestAuditChainVerification(t *testing.T) {
	secretKey := []byte("super-secure-audit-hmac-key-256bit!!")
	tenantID := uuid.New()
	now := time.Now().UTC()

	// Build a valid chain of 3 records
	records := make([]domain.AuditEvent, 3)
	prevHash := GenesisHash

	actions := []string{"tenant.created", "account.connected", "action.approved"}
	for i := 0; i < 3; i++ {
		event := domain.AuditEvent{
			ID:          int64(i + 1),
			TenantID:    tenantID,
			ActorID:     "user-123",
			ActorType:   domain.ActorTypeUser,
			EventAction: actions[i],
			TargetType:  "system",
			TargetID:    "target-1",
			Details: map[string]interface{}{
				"step": i,
			},
			PreviousHash: prevHash,
			CreatedAt:    now.Add(time.Duration(i) * time.Minute),
		}

		hash, err := CalculateRecordHash(secretKey, prevHash, &event)
		if err != nil {
			t.Fatalf("failed to calculate hash: %v", err)
		}
		event.RecordHash = hash
		records[i] = event
		prevHash = hash
	}

	// 1. Verify valid chain passes
	valid, index, err := VerifyAuditChain(secretKey, records)
	if !valid || index != -1 || err != nil {
		t.Fatalf("expected chain to be valid, got valid=%v, index=%d, err=%v", valid, index, err)
	}

	// 2. Tamper with record 1 payload (e.g. malicious DB update)
	tamperedRecords := make([]domain.AuditEvent, len(records))
	copy(tamperedRecords, records)
	tamperedRecords[1].Details = map[string]interface{}{"step": 999, "hacked": true}

	valid, index, err = VerifyAuditChain(secretKey, tamperedRecords)
	if valid || index != 1 || err == nil {
		t.Fatalf("expected tamper detection at index 1, got valid=%v, index=%d, err=%v", valid, index, err)
	}

	// 3. Drop a middle record (break chain linkage)
	droppedRecords := []domain.AuditEvent{records[0], records[2]}
	valid, index, err = VerifyAuditChain(secretKey, droppedRecords)
	if valid || index != 1 || err == nil {
		t.Fatalf("expected chain breakage detection when record dropped, got valid=%v, index=%d", valid, index)
	}
}
