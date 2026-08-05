package providerutil

import (
	"fmt"
	"regexp"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/redact"
)

var bootstrapSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`tskey-[A-Za-z0-9_-]+`),
	regexp.MustCompile(`\$6\$[^\s"']+`),
}

// ValidateMutationProvider prevents an adapter from mutating another provider's record.
func ValidateMutationProvider(actual, expected compute.ProviderName) error {
	if actual != expected {
		return fmt.Errorf("provider ownership mismatch: state provider %q provider %q", actual, expected)
	}
	return nil
}

// Failure returns one secret-safe provider diagnostic.
func Failure(secret, prefix string, err error, extraSecrets ...string) compute.Diagnostics {
	secrets := append([]string{secret}, extraSecrets...)
	message := redact.New(secrets...).String(fmt.Sprintf("%s: %v", prefix, err))
	return compute.Diagnostics{compute.FailureDiagnostic(message, err)}
}

// BootstrapSecrets returns full and embedded bootstrap secret forms for redaction.
func BootstrapSecrets(data string, encoded ...string) []string {
	secrets := []string{data}
	seen := map[string]bool{data: true}
	for _, value := range encoded {
		if value != "" && !seen[value] {
			secrets = append(secrets, value)
			seen[value] = true
		}
	}
	for _, pattern := range bootstrapSecretPatterns {
		for _, match := range pattern.FindAllString(data, -1) {
			if !seen[match] {
				secrets = append(secrets, match)
				seen[match] = true
			}
		}
	}
	return secrets
}
