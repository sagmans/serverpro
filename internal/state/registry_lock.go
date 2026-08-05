package state

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sagmans/serverpro/internal/filedescriptor"
)

const (
	registryLockName             = ".registry.lock"
	stateLockSuffix              = ".lock"
	serverOperationLockSuffix    = ".operation.lock"
	namespaceOperationLockPrefix = ".namespace-"
	tailnetPolicyLockPrefix      = ".tailnet-policy-"
	tailnetPolicyGlobalLockName  = ".tailnet-policy-global.operation.lock"
	tokenRelativeTailnetIdentity = "-"
	operationLockRetryInterval   = 10 * time.Millisecond
)

func lockRegistry(path string) (func(), error) {
	return lockFile(filepath.Join(filepath.Dir(path), registryLockName))
}

func lockState(path string) (func(), error) {
	return lockFile(path + stateLockSuffix)
}

// LockServerOperation serializes create, import, and delete workflows that can
// otherwise make overlapping provider mutations from the same local authority.
func LockServerOperation(ctx context.Context, statePath string) (func(), error) {
	return lockFileModeContext(ctx, statePath+serverOperationLockSuffix, syscall.LOCK_EX)
}

// LockServerWorkflow owns the namespace-before-server ordering shared by
// create, import, and single-server delete workflows.
func LockServerWorkflow(ctx context.Context, registryPath, namespace, statePath string) (func(), error) {
	unlockNamespace, err := LockNamespaceOperation(ctx, registryPath, namespace)
	if err != nil {
		return nil, err
	}
	unlockServer, err := LockServerOperation(ctx, statePath)
	if err != nil {
		unlockNamespace()
		return nil, err
	}
	return func() {
		unlockServer()
		unlockNamespace()
	}, nil
}

// LockNamespaceOperation allows independent server workflows while excluding
// namespace-wide deletion from the same local authority scope.
func LockNamespaceOperation(ctx context.Context, registryPath, namespace string) (func(), error) {
	return lockNamespaceOperation(ctx, registryPath, namespace, syscall.LOCK_SH)
}

// LockNamespaceOperationExclusive excludes every cooperating workflow that can
// publish or destroy authority inside one namespace.
func LockNamespaceOperationExclusive(ctx context.Context, registryPath, namespace string) (func(), error) {
	return lockNamespaceOperation(ctx, registryPath, namespace, syscall.LOCK_EX)
}

func lockNamespaceOperation(ctx context.Context, registryPath, namespace string, mode int) (func(), error) {
	identity := strings.TrimSpace(namespace)
	if identity == "" {
		return nil, fmt.Errorf("namespace identity required")
	}
	digest := sha256.Sum256([]byte(identity))
	name := fmt.Sprintf("%s%x%s", namespaceOperationLockPrefix, digest, serverOperationLockSuffix)
	return lockFileModeContext(ctx, filepath.Join(filepath.Dir(registryPath), name), mode)
}

// LockTailnetPolicy serializes local evidence publication with destructive
// policy reconciliation. Hashing keeps operator-controlled identity inside the
// managed lock directory.
func LockTailnetPolicy(ctx context.Context, registryPath, tailnet string) (func(), error) {
	identity := strings.TrimSpace(tailnet)
	if identity == "" {
		return nil, fmt.Errorf("tailnet identity required")
	}
	globalPath := filepath.Join(filepath.Dir(registryPath), tailnetPolicyGlobalLockName)
	if identity == tokenRelativeTailnetIdentity {
		// Token-relative operations may target any tailnet, so they exclude every
		// explicit policy operation until a stable identity is available.
		return lockFileModeContext(ctx, globalPath, syscall.LOCK_EX)
	}
	unlockGlobal, err := lockFileModeContext(ctx, globalPath, syscall.LOCK_SH)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(identity))
	name := fmt.Sprintf("%s%x%s", tailnetPolicyLockPrefix, digest, serverOperationLockSuffix)
	unlockIdentity, err := lockFileModeContext(ctx, filepath.Join(filepath.Dir(registryPath), name), syscall.LOCK_EX)
	if err != nil {
		unlockGlobal()
		return nil, err
	}
	return func() {
		unlockIdentity()
		unlockGlobal()
	}, nil
}

func lockFile(lockPath string) (func(), error) {
	return lockFileMode(lockPath, syscall.LOCK_EX)
}

func lockFileMode(lockPath string, mode int) (func(), error) {
	f, fd, err := openLockFile(lockPath)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(fd, mode); err != nil {
		_ = f.Close()
		return nil, err
	}
	return lockFileRelease(f, fd), nil
}

func lockFileModeContext(ctx context.Context, lockPath string, mode int) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, fd, err := openLockFile(lockPath)
	if err != nil {
		return nil, err
	}
	retry := time.NewTicker(operationLockRetryInterval)
	defer retry.Stop()
	for {
		err = syscall.Flock(fd, mode|syscall.LOCK_NB)
		if err == nil {
			return lockFileRelease(f, fd), nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-retry.C:
		}
	}
}

func openLockFile(lockPath string) (*os.File, int, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, 0, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, 0, err
	}
	fd, err := filedescriptor.Int(f)
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, fd, nil
}

func lockFileRelease(f *os.File, fd int) func() {
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
	}
}
