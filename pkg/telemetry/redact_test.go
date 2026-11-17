package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "AWS Access Key ID",
			input:    "Failed authenticating with AKIAIOSFODNN7EXAMPLE in us-east-1",
			expected: "Failed authenticating with AKIA**************** in us-east-1",
		},
		{
			name:     "Bearer Authorization Token",
			input:    "Request header: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.signature",
			expected: "Request header: Bearer [REDACTED_BEARER_TOKEN]",
		},
		{
			name:     "Normal log message",
			input:    "Successfully queried 42 EC2 instances",
			expected: "Successfully queried 42 EC2 instances",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := RedactString(tc.input)
			if actual != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, actual)
			}
		})
	}
}

func TestRedactingLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelInfo)

	logger.InfoContext(context.Background(),
		"Discovered AWS credentials for account connection",
		slog.String("aws_secret_access_key", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
		slog.String("api_key", "secret-cradle-token-12345"),
		slog.String("raw_header", "Bearer 1234567890abcdefghijklmnop"),
		slog.String("account_id", "123456789012"),
	)

	loggedData := buf.String()

	// Parse JSON to verify keys
	var logMap map[string]interface{}
	if err := json.Unmarshal([]byte(loggedData), &logMap); err != nil {
		t.Fatalf("failed to parse log JSON: %v, raw: %s", err, loggedData)
	}

	// Sensitive keys must be masked
	if logMap["aws_secret_access_key"] != MaskedReplacement {
		t.Errorf("expected aws_secret_access_key to be redacted, got: %v", logMap["aws_secret_access_key"])
	}
	if logMap["api_key"] != MaskedReplacement {
		t.Errorf("expected api_key to be redacted, got: %v", logMap["api_key"])
	}

	// Bearer token inside string value must be redacted
	rawHeader, ok := logMap["raw_header"].(string)
	if !ok || !strings.Contains(rawHeader, "[REDACTED_BEARER_TOKEN]") {
		t.Errorf("expected raw_header to have masked bearer token, got: %v", rawHeader)
	}

	// Safe data must remain intact
	if logMap["account_id"] != "123456789012" {
		t.Errorf("expected account_id to be preserved, got: %v", logMap["account_id"])
	}
}
