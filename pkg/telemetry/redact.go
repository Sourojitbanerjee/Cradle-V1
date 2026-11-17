package telemetry

import (
	"regexp"
	"strings"
)

// Common sensitive patterns in cloud environments.
var (
	awsAccessKeyRegex = regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)
	bearerTokenRegex  = regexp.MustCompile(`(?i)(bearer\s+)[a-zA-Z0-9_\-\.]{15,}`)
	privateKeyRegex   = regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----[a-zA-Z0-9\n\r\/+=\s]+-----END [A-Z ]+PRIVATE KEY-----`)
)

// SensitiveKeyNames lists map/JSON keys whose values must always be masked.
var sensitiveKeyNames = map[string]bool{
	"password":              true,
	"passphrase":            true,
	"secret":                true,
	"secret_key":            true,
	"access_token":          true,
	"refresh_token":         true,
	"api_key":               true,
	"authorization":         true,
	"aws_secret_access_key": true,
	"private_key":           true,
}

const MaskedReplacement = "[REDACTED]"

// RedactString sanitizes known credentials and tokens from arbitrary strings.
func RedactString(input string) string {
	if input == "" {
		return ""
	}

	result := awsAccessKeyRegex.ReplaceAllString(input, "$1****************")
	result = bearerTokenRegex.ReplaceAllString(result, "${1}[REDACTED_BEARER_TOKEN]")
	result = privateKeyRegex.ReplaceAllString(result, "[REDACTED_PRIVATE_KEY]")

	return result
}

// IsSensitiveKey returns true if the key name implies a confidential credential.
func IsSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return sensitiveKeyNames[normalized]
}
