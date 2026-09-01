package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	operationLockProbe       = 50 * time.Millisecond
	operationLockDeadline    = 100 * time.Millisecond
	testTokenRelativeTailnet = "-"
)

func TestLockServerOperationRejectsNonDirectoryParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LockServerOperation(context.Background(), filepath.Join(parent, "state.json")); err == nil {
		t.Fatal("lock accepted non-directory parent")
	}
}

func TestLockServerOperationRejectsSymlinkedAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(link, "nested", "state.json")
	if _, err := LockServerOperation(context.Background(), statePath); err == nil {
		t.Fatal("lock accepted symlinked ancestor")
	}
	if _, err := os.Stat(filepath.Join(outside, "nested", "state.json.operation.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock escaped through symlinked ancestor: %v", err)
	}
}

func TestLockServerOperationWaitsForLocalArtifactCleanup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := stateTestPath(t, "state.json")
	unlockCleanup, err := LockLocalArtifactCleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationLockDeadline)
	defer cancel()
	if _, err := LockServerOperation(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		unlockCleanup()
		t.Fatalf("server operation guard error = %v", err)
	}
	unlockCleanup()
}

func TestLockServerOperationSerializesSameState(t *testing.T) {
	path := stateTestPath(t, "state.json")
	unlockFirst, err := LockServerOperation(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	attempted := make(chan struct{})
	acquired := make(chan struct{})
	releaseSecond := make(chan struct{})
	go func() {
		close(attempted)
		unlockSecond, lockErr := LockServerOperation(context.Background(), path)
		if lockErr != nil {
			return
		}
		close(acquired)
		<-releaseSecond
		unlockSecond()
	}()
	<-attempted
	select {
	case <-acquired:
		unlockFirst()
		close(releaseSecond)
		t.Fatal("same-server operation lock was acquired concurrently")
	case <-time.After(operationLockProbe):
	}
	unlockFirst()
	select {
	case <-acquired:
		close(releaseSecond)
	case <-time.After(time.Second):
		t.Fatal("waiting server operation did not acquire released lock")
	}
}

func TestLockServerWorkflowHonorsContextAndReleasesPartialAcquisition(t *testing.T) {
	registryPath := stateTestPath(t, "registry.json")
	statePath := stateTestPath(t, "state.json")
	unlockServer, err := LockServerOperation(context.Background(), statePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationLockDeadline)
	defer cancel()
	if _, err := LockServerWorkflow(ctx, registryPath, "demo", statePath); !errors.Is(err, context.DeadlineExceeded) {
		unlockServer()
		t.Fatalf("workflow lock error = %v", err)
	}
	unlockServer()

	probeCtx, probeCancel := context.WithTimeout(context.Background(), operationLockDeadline)
	defer probeCancel()
	unlockNamespace, err := LockNamespaceOperationExclusive(probeCtx, registryPath, "demo")
	if err != nil {
		t.Fatalf("partial namespace lock was not released: %v", err)
	}
	unlockNamespace()
}

func TestLockTailnetPolicyHonorsContext(t *testing.T) {
	registryPath := stateTestPath(t, "registry.json")
	unlock, err := LockTailnetPolicy(context.Background(), registryPath, "example.ts.net")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), operationLockDeadline)
	defer cancel()
	if _, err := LockTailnetPolicy(ctx, registryPath, "example.ts.net"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("tailnet lock error = %v", err)
	}
}

func TestLockNamespaceOperationAllowsConcurrentServerWork(t *testing.T) {
	registryPath := stateTestPath(t, "registry.json")
	unlockFirst, err := LockNamespaceOperation(context.Background(), registryPath, "demo")
	if err != nil {
		t.Fatal(err)
	}
	defer unlockFirst()
	acquired := make(chan func(), 1)
	go func() {
		unlockSecond, lockErr := LockNamespaceOperation(context.Background(), registryPath, "demo")
		if lockErr == nil {
			acquired <- unlockSecond
		}
	}()
	select {
	case unlockSecond := <-acquired:
		unlockSecond()
	case <-time.After(time.Second):
		t.Fatal("shared namespace operations blocked each other")
	}
}

func TestLockNamespaceOperationExclusiveBlocksOnlyMatchingNamespace(t *testing.T) {
	registryPath := stateTestPath(t, "registry.json")
	unlockExclusive, err := LockNamespaceOperationExclusive(context.Background(), registryPath, "demo")
	if err != nil {
		t.Fatal(err)
	}
	otherAcquired := make(chan func(), 1)
	go func() {
		unlockOther, lockErr := LockNamespaceOperation(context.Background(), registryPath, "other")
		if lockErr == nil {
			otherAcquired <- unlockOther
		}
	}()
	select {
	case unlockOther := <-otherAcquired:
		unlockOther()
	case <-time.After(time.Second):
		unlockExclusive()
		t.Fatal("different namespace operation blocked")
	}

	matchingAcquired := make(chan func(), 1)
	go func() {
		unlockMatching, lockErr := LockNamespaceOperation(context.Background(), registryPath, "demo")
		if lockErr == nil {
			matchingAcquired <- unlockMatching
		}
	}()
	select {
	case unlockMatching := <-matchingAcquired:
		unlockMatching()
		unlockExclusive()
		t.Fatal("matching namespace operation bypassed exclusive lock")
	case <-time.After(operationLockProbe):
	}
	unlockExclusive()
	select {
	case unlockMatching := <-matchingAcquired:
		unlockMatching()
	case <-time.After(time.Second):
		t.Fatal("matching namespace operation did not resume")
	}
}

func TestLockNamespaceOperationRejectsMissingIdentity(t *testing.T) {
	registryPath := stateTestPath(t, "registry.json")
	for name, lock := range map[string]func(context.Context, string, string) (func(), error){
		"shared":    LockNamespaceOperation,
		"exclusive": LockNamespaceOperationExclusive,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := lock(context.Background(), registryPath, " "); err == nil {
				t.Fatal("missing namespace identity accepted")
			}
		})
	}
}

func TestLockTailnetPolicySerializesOnlyMatchingTailnet(t *testing.T) {
	registryPath := stateTestPath(t, "registry.json")
	unlockFirst, err := LockTailnetPolicy(context.Background(), registryPath, "tailnet-a")
	if err != nil {
		t.Fatal(err)
	}
	otherAcquired := make(chan func(), 1)
	go func() {
		unlockOther, lockErr := LockTailnetPolicy(context.Background(), registryPath, "tailnet-b")
		if lockErr == nil {
			otherAcquired <- unlockOther
		}
	}()
	select {
	case unlockOther := <-otherAcquired:
		unlockOther()
	case <-time.After(time.Second):
		unlockFirst()
		t.Fatal("different tailnet lock blocked")
	}

	acquired := make(chan struct{})
	releaseSecond := make(chan struct{})
	go func() {
		unlockSecond, lockErr := LockTailnetPolicy(context.Background(), registryPath, "tailnet-a")
		if lockErr != nil {
			return
		}
		close(acquired)
		<-releaseSecond
		unlockSecond()
	}()
	select {
	case <-acquired:
		unlockFirst()
		close(releaseSecond)
		t.Fatal("same-tailnet policy lock was acquired concurrently")
	case <-time.After(operationLockProbe):
	}
	unlockFirst()
	select {
	case <-acquired:
		close(releaseSecond)
	case <-time.After(time.Second):
		t.Fatal("waiting tailnet policy operation did not acquire released lock")
	}
}

func TestLockTailnetPolicyTokenRelativeIdentityBlocksExplicitTailnet(t *testing.T) {
	registryPath := stateTestPath(t, "registry.json")
	unlockRelative, err := LockTailnetPolicy(context.Background(), registryPath, testTokenRelativeTailnet)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan func(), 1)
	go func() {
		unlockExplicit, lockErr := LockTailnetPolicy(context.Background(), registryPath, "example.ts.net")
		if lockErr == nil {
			acquired <- unlockExplicit
		}
	}()
	select {
	case unlockExplicit := <-acquired:
		unlockExplicit()
		unlockRelative()
		t.Fatal("token-relative policy lock did not block explicit tailnet")
	case <-time.After(operationLockProbe):
	}
	unlockRelative()
	select {
	case unlockExplicit := <-acquired:
		unlockExplicit()
	case <-time.After(time.Second):
		t.Fatal("explicit tailnet did not acquire released global policy guard")
	}
}

func TestLockTailnetPolicyRejectsMissingIdentity(t *testing.T) {
	if _, err := LockTailnetPolicy(context.Background(), stateTestPath(t, "registry.json"), ""); err == nil {
		t.Fatal("missing tailnet identity accepted")
	}
}
