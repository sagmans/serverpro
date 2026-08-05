package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsMissingRequiredFields(t *testing.T) {
	err := (Config{}).Validate()
	if err == nil || !strings.Contains(err.Error(), "missing required config") || !strings.Contains(err.Error(), "namespace") || !strings.Contains(err.Error(), "credentials.json_path") {
		t.Fatalf("Validate() error = %v", err)
	}
}
