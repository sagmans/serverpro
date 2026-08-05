package internal_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pathEnvironmentPrefix  = "PATH="
	policyScriptsDirectory = "scripts"
	policyShellName        = "bash"
	repositoryParent       = ".."
)

var (
	ciPolicyScripts = []string{"test-toolchain-policy.sh", "test-release-workflow.sh"}
	ciPolicyTools   = []string{"dirname", "grep"}
)

func TestPolicyScriptsUseRunnerProvidedTools(t *testing.T) {
	root, err := filepath.Abs(repositoryParent)
	if err != nil {
		t.Fatal(err)
	}
	shell, err := exec.LookPath(policyShellName)
	if err != nil {
		t.Fatal(err)
	}
	toolPath := t.TempDir()
	for _, name := range ciPolicyTools {
		source, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(source, filepath.Join(toolPath, name)); err != nil {
			t.Fatal(err)
		}
	}
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, pathEnvironmentPrefix) {
			env = append(env, item)
		}
	}
	// An allowlisted PATH proves policy scripts do not inherit optional developer tools.
	env = append(env, pathEnvironmentPrefix+toolPath)
	for _, name := range ciPolicyScripts {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(shell, filepath.Join(root, policyScriptsDirectory, name))
			cmd.Dir = root
			cmd.Env = env
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("policy script failed: %v\n%s", err, output)
			}
		})
	}
}
