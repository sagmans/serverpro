package credentials

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	// keyEnvVar lets CI and ephemeral hosts inject the key without writing it
	// anywhere; serverpro reads it and never persists it.
	keyEnvVar = "SERVERPRO_CREDENTIAL_KEY"
	// keyCommandEnvVar names a shell command whose stdout is the key, for a
	// secret manager CLI (1Password, Vault, pass) to supply it on demand.
	keyCommandEnvVar = "SERVERPRO_CREDENTIAL_KEY_COMMAND"
	// keyCommandShell runs the command string the way an operator wrote it.
	keyCommandShell = "/bin/sh"
	// keyCommandTimeout bounds the command so a stuck secret manager cannot
	// hang an agent run.
	keyCommandTimeout = 30 * time.Second
	// keyCommandWaitDelay bounds the wait for output pipes after the kill.
	// WHY: a child the shell started can keep stdout open after the shell
	// dies, and Wait would otherwise block on it past the timeout.
	keyCommandWaitDelay = 2 * time.Second
)

// envKeyStore supplies the key from the environment. It is read-only by
// design: CI keys stay ephemeral and are managed by the CI secret store.
type envKeyStore struct{}

// Describe never includes the value, only where it came from.
func (envKeyStore) Describe() string { return "environment variable " + keyEnvVar }

// Kind marks an injected, read-only key.
func (envKeyStore) Kind() string { return keyStoreEnv }

// Load returns the injected key, trimmed of the newline shells often add.
func (envKeyStore) Load() (string, error) {
	value := strings.TrimSpace(os.Getenv(keyEnvVar))
	if value == "" {
		return "", ErrKeyNotFound
	}
	return value, nil
}

// Create, Replace, and Delete refuse: the CI secret store owns this key, and
// a local copy would outlive the job it was injected for.
func (envKeyStore) Create(string) error  { return ErrKeyStoreReadOnly }
func (envKeyStore) Replace(string) error { return ErrKeyStoreReadOnly }
func (envKeyStore) Delete() error        { return ErrKeyStoreReadOnly }

// commandKeyStore runs an operator-configured command for the key on every
// load, so the key never has to live on this host's disk.
type commandKeyStore struct {
	command string
}

// Describe names the mechanism, not the command text, because the command
// may embed a vault path the operator does not want in error logs.
func (commandKeyStore) Describe() string { return "key command from " + keyCommandEnvVar }

// Kind marks a read-only key supplied by an external secret manager.
func (commandKeyStore) Kind() string { return keyStoreCommand }

// Load runs the command with a deadline. Its stderr is discarded from the
// error, since secret manager CLIs can echo sensitive context there.
func (s commandKeyStore) Load() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, keyCommandShell, "-c", s.command)
	// Run the command in its own process group and kill the whole group on
	// timeout, so a secret manager the shell spawned cannot outlive it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = keyCommandWaitDelay
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stdin = nil
	// ErrWaitDelay means the command exited 0 but a child it left running
	// (gpg-agent, a vault helper) still held stdout; the key was printed.
	if err := cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: key command exceeded %s", ErrKeyStoreUnavailable, keyCommandTimeout)
		}
		return "", fmt.Errorf("%w: key command failed: %w", ErrKeyStoreUnavailable, err)
	}
	value := strings.TrimSpace(stdout.String())
	if value == "" {
		return "", fmt.Errorf("%w: key command printed nothing", ErrKeyStoreUnavailable)
	}
	return value, nil
}

// Create, Replace, and Delete refuse: the external secret manager owns this
// key, and serverpro has no safe way to write back into it.
func (commandKeyStore) Create(string) error  { return ErrKeyStoreReadOnly }
func (commandKeyStore) Replace(string) error { return ErrKeyStoreReadOnly }
func (commandKeyStore) Delete() error        { return ErrKeyStoreReadOnly }
