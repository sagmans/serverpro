package cli

import (
	"os"
	"strings"
	"testing"
)

func TestUsageDocumentsScopedPathsAndNamespaceDelete(t *testing.T) {
	body, err := os.ReadFile("../../USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(body)
	for _, want := range []string{
		"--config <path>",
		"--state <path>",
		"serverpro namespace delete NAME",
		"`--config` is accepted only by",
		"`server create`, `server doctor`, and `server bootstrap`",
		"`--state` is accepted only by",
		"`server create`, `server status`, `server doctor`, `server ssh`",
		"`server delete`, `server start`, `server stop`, `server restart`",
		"`server bootstrap`, `ingress add`, `ingress list`, and `ingress remove`",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("USAGE.md missing %q", want)
		}
	}
}
