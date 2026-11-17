package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// GenerateExternalID creates a cryptographically secure, unique ExternalId
// for AWS cross-account STS AssumeRole authentication.
// It combines a UUID v4 with 16 bytes of high-entropy cryptographic randomness.
func GenerateExternalID(tenantSlug string) (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate cryptographic random bytes: %w", err)
	}

	u := uuid.New()
	return fmt.Sprintf("cradle-%s-%s-%s", tenantSlug, u.String(), hex.EncodeToString(randomBytes)), nil
}
