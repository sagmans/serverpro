package providerutil

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type testProviderError struct {
	message string
}

func (e *testProviderError) Error() string { return e.message }

func TestValidateMutationProviderRejectsMismatch(t *testing.T) {
	if err := ValidateMutationProvider("vultr", "vultr"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMutationProvider("hetzner", "vultr"); err == nil {
		t.Fatal("expected ownership mismatch")
	}
}

func TestFailureRedactsSecretsAndPreservesCauseIdentity(t *testing.T) {
	cause := &testProviderError{message: "secret-token exposed"}
	diagnostics := Failure("secret-token", "provider failed", cause)
	err := diagnostics.Err()
	var typed *testProviderError
	if diagnostics.Passed() || strings.Contains(err.Error(), "secret-token") || strings.Contains(fmt.Sprintf("%+v", diagnostics), "secret-token") || !errors.Is(err, cause) || !errors.As(err, &typed) || typed != cause {
		t.Fatalf("diagnostics=%+v err=%v typed=%v", diagnostics, err, typed)
	}
}

func TestBootstrapSecretsExtractsKnownSecretShapes(t *testing.T) {
	got := BootstrapSecrets("tskey-auth-abc $6$salt$hash", "encoded")
	for _, want := range []string{"tskey-auth-abc", "$6$salt$hash", "encoded"} {
		found := false
		for _, value := range got {
			found = found || value == want
		}
		if !found {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
}
