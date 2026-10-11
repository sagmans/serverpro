//go:build serverpro_full_chain_e2e

package e2e_test

import (
	"os"
	"strings"
	"testing"
)

func TestFullChainE2EHasDistinctFailureArtifactCIJob(t *testing.T) {
	makefile := readRepoFile(t, "../../Makefile")
	for _, want := range []string{"test-full-chain-e2e:", "go test ./internal/e2e"} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing %q", want)
		}
	}

	workflow := readRepoFile(t, "../../.github/workflows/ci.yml")
	for _, want := range []string{
		"full-chain-e2e:",
		"run: make test-full-chain-e2e",
		"if: failure()",
		"actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9",
		"path: .artifacts/full-chain-e2e",
		"retention-days: 3",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("CI workflow missing %q", want)
		}
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
