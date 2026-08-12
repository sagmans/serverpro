package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestTestingMatrixDocumentsEveryCLICommand(t *testing.T) {
	doc := readTestingMatrix(t)
	for _, path := range commandPaths(New()) {
		if !strings.Contains(doc, "`"+path+"`") {
			t.Fatalf("TESTING.md missing command %s", path)
		}
	}
}

func TestCLISurfaceManifestAssignsEveryCommandEvidence(t *testing.T) {
	file, err := os.Open("../../scripts/cli-surface-dispositions.tsv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close CLI disposition manifest: %v", err)
		}
	})

	dispositions := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			t.Fatalf("invalid CLI disposition %q", line)
		}
		if _, exists := dispositions[fields[0]]; exists {
			t.Fatalf("duplicate CLI disposition %q", fields[0])
		}
		dispositions[fields[0]] = fields[1]
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{}
	for _, path := range commandPaths(New()) {
		expected[path] = true
		if dispositions[path] == "" {
			t.Errorf("CLI disposition missing for %q", path)
		}
	}
	for path := range dispositions {
		if !expected[path] {
			t.Errorf("CLI disposition names unregistered command %q", path)
		}
	}
}

func TestTestingMatrixDocumentsCorePackages(t *testing.T) {
	doc := readTestingMatrix(t)
	for _, path := range []string{
		"cmd/serverpro",
		"internal/cli",
		"internal/bootstraptools",
		"internal/cloudinit",
		"internal/compute",
		"internal/config",
		"internal/credentials",
		"internal/doctor",
		"internal/importsync",
		"internal/ingress",
		"internal/lifecycle",
		"internal/remote",
		"internal/state",
		"internal/provider/hetzner",
		"internal/provider/vultr",
		"internal/provider/digitalocean",
		"internal/provider/tailscale",
		"internal/provider/cloudflare",
	} {
		if !strings.Contains(doc, "`"+path+"`") {
			t.Fatalf("TESTING.md missing package %s", path)
		}
	}
}

// TestTestingMatrixDocumentsEveryGoPackage auto-discovers packages so a newly
// added package cannot silently escape the coverage matrix. WHY: the matrix is
// only trustworthy if it is impossible to add code without documenting how it is
// tested. This is the guard that makes the suite "expandable by default".
func TestTestingMatrixDocumentsEveryGoPackage(t *testing.T) {
	doc := readTestingMatrix(t)
	packages, err := goPackagePaths("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) == 0 {
		t.Fatal("discovered no Go packages")
	}
	for pkg := range packages {
		if !strings.Contains(doc, "`"+pkg+"`") {
			t.Errorf("TESTING.md does not document package %q; add it to the package capability matrix", pkg)
		}
	}
}

func TestGoPackagePathsIgnoresNestedWorktreeModule(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{
		"go.mod":                       "module example.com/root\n\ngo 1.26.0\n",
		"root.go":                      "package root\n",
		".pi/worktrees/feature/go.mod": "module example.com/feature\n\ngo 1.26.0\n",
		".pi/worktrees/feature/internal/stray/stray.go": "package stray\n",
	} {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packages, err := goPackagePaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || !packages["."] {
		t.Fatalf("packages = %+v, want only module root", packages)
	}
}

func goPackagePaths(root string) (map[string]bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("go", "list", "-f", "{{if .GoFiles}}{{.Dir}}{{end}}", "./...")
	cmd.Dir = absRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list Go packages: %w: %s", err, out)
	}
	packages := map[string]bool{}
	for _, dir := range strings.Split(string(out), "\n") {
		if dir == "" {
			continue
		}
		rel, err := filepath.Rel(absRoot, dir)
		if err != nil {
			return nil, err
		}
		packages[filepath.ToSlash(rel)] = true
	}
	return packages, nil
}

func TestTestingMatrixDocumentsCoverageTracking(t *testing.T) {
	doc := readTestingMatrix(t)
	for _, want := range []string{
		"## Line coverage tracking",
		"`make cover`",
		"Remaining 0% functions: none",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("TESTING.md missing coverage-tracking anchor %q", want)
		}
	}
}

func TestTestingMatrixSeparatesPrimaryAndFullChainGates(t *testing.T) {
	doc := readTestingMatrix(t)
	for _, want := range []string{
		"CI combines `make check` with a separate `make test-full-chain-e2e` job",
		"| Enforced minimum | 81.8% |",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("TESTING.md missing accurate gate contract %q", want)
		}
	}
	for _, stale := range []string{
		"`make check` runs each non-live evidence kind once",
		"| Current | 81.8% |",
	} {
		if strings.Contains(doc, stale) {
			t.Fatalf("TESTING.md retains stale gate contract %q", stale)
		}
	}
}

func TestTestingMatrixDocumentsQualityGates(t *testing.T) {
	doc := readTestingMatrix(t)
	for _, want := range []string{
		"`make test-unit`",
		"`make test-go-check`",
		"`make test-smoke`",
		"`make test-integration`",
		"`make test-e2e`",
		"`make test-dogfood-readonly`",
		"`make test-dogfood-live`",
		"`scripts/test-dogfood-live.sh`",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("TESTING.md missing quality gate %s", want)
		}
	}
	if _, err := os.Stat("../../scripts/test-dogfood-live.sh"); err != nil {
		t.Fatalf("missing dogfood script: %v", err)
	}
}

func readTestingMatrix(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../TESTING.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func commandPaths(root *cobra.Command) []string {
	paths := []string{root.CommandPath()}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			if child.Hidden {
				continue
			}
			paths = append(paths, child.CommandPath())
			walk(child)
		}
	}
	walk(root)
	return paths
}
