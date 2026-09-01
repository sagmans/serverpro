package state

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
)

const (
	registryLockName             = ".registry.lock"
	stateLockSuffix              = ".lock"
	serverOperationLockSuffix    = ".operation.lock"
	namespaceOperationLockPrefix = ".namespace-"
	tailnetPolicyLockPrefix      = ".tailnet-policy-"
	tailnetPolicyGlobalLockName  = ".tailnet-policy-global.operation.lock"
	tokenRelativeTailnetIdentity = "-"
)

func lockRegistry(path string) (func(), error) {
	return lockFile(filepath.Join(filepath.Dir(path), registryLockName))
}

func lockState(path string) (func(), error) {
	return lockLocalArtifact(context.Background(), path+stateLockSuffix)
}

// LockServerOperation serializes create, import, and delete workflows that can
// otherwise make overlapping provider mutations from the same local authority.
func LockServerOperation(ctx context.Context, statePath string) (func(), error) {
	return lockLocalArtifact(ctx, statePath+serverOperationLockSuffix)
}

func LockLocalArtifactCleanup(ctx context.Context) (func(), error) {
	return privatefile.LockExclusiveContext(ctx, config.LocalArtifactGuardPath())
}

func lockLocalArtifact(ctx context.Context, path string) (func(), error) {
	unlockGuard, err := privatefile.LockSharedContext(ctx, config.LocalArtifactGuardPath())
	if err != nil {
		return nil, err
	}
	unlockArtifact, err := privatefile.LockExclusiveContext(ctx, path)
	if err != nil {
		unlockGuard()
		return nil, err
	}
	return func() {
		unlockArtifact()
		unlockGuard()
	}, nil
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
	return lockFileModeContext(context.Background(), lockPath, mode)
}

func lockFileModeContext(ctx context.Context, lockPath string, mode int) (func(), error) {
	if mode == syscall.LOCK_SH {
		return privatefile.LockSharedContext(ctx, lockPath)
	}
	return privatefile.LockExclusiveContext(ctx, lockPath)
}
