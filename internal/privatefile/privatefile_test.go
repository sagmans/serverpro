package privatefile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLockSerializesExclusiveUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resource.lock")
	unlockFirst, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan func(), 1)
	go func() {
		unlockSecond, lockErr := Lock(path)
		if lockErr == nil {
			acquired <- unlockSecond
		}
	}()
	select {
	case unlockSecond := <-acquired:
		unlockSecond()
		unlockFirst()
		t.Fatal("exclusive lock was acquired concurrently")
	case <-time.After(50 * time.Millisecond):
	}
	unlockFirst()
	select {
	case unlockSecond := <-acquired:
		unlockSecond()
	case <-time.After(time.Second):
		t.Fatal("waiting lock did not acquire released resource")
	}
}

func TestLockModesCoordinateAndHonorContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.guard")
	unlockShared, err := LockSharedContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := LockExclusiveContext(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		unlockShared()
		t.Fatalf("exclusive guard error = %v", err)
	}
	unlockShared()
	unlockExclusive, err := LockExclusiveContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	unlockExclusive()
}

func TestLockContextRejectsSymlinkedAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LockExclusiveContext(context.Background(), filepath.Join(link, "resource.lock")); err == nil {
		t.Fatal("lock accepted symlinked ancestor")
	}
}

func TestRemoveDurablyRejectsSymlinkedAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim")
	if err := os.WriteFile(victim, []byte("keep"), DefaultFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveDurably(filepath.Join(root, "linked", "victim")); err == nil {
		t.Fatal("durable removal accepted symlinked ancestor")
	}
	if body, err := os.ReadFile(victim); err != nil || string(body) != "keep" {
		t.Fatalf("outside file changed: body=%q err=%v", body, err)
	}
}

func TestRemoveTreeDurablyDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim")
	if err := os.WriteFile(victim, []byte("keep"), DefaultFileMode); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(root, "tree")
	if err := os.MkdirAll(filepath.Join(tree, "nested"), DefaultDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "nested", "local"), []byte("remove"), DefaultFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tree, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveTreeDurably(tree); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tree); !os.IsNotExist(err) {
		t.Fatalf("tree remains: %v", err)
	}
	if body, err := os.ReadFile(victim); err != nil || string(body) != "keep" {
		t.Fatalf("outside file changed: body=%q err=%v", body, err)
	}
}

func TestAtomicWriteJSONWritesPrivateFileAndDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data.json")
	if err := AtomicWriteJSON(path, map[string]string{"name": "prod"}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != DefaultFileMode {
		t.Fatalf("file mode = %o", fileInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != DefaultDirMode {
		t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
	}
}

func TestAtomicWriteSyncIncludesParentDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	dir := filepath.Join(t.TempDir(), "durable")
	path := filepath.Join(dir, "data.json")
	err := AtomicWrite(path, []byte(`{"name":"prod"}`), WriteOptions{Sync: true, BeforeRename: func() error {
		return os.Chmod(dir, 0o300)
	}})
	defer func() { _ = os.Chmod(dir, DefaultDirMode) }()
	if err == nil {
		t.Fatal("sync write did not sync the unreadable parent directory")
	}
}

func TestSyncDirectory(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		if err := syncDirectory(t.TempDir()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing")
		if err := syncDirectory(path); err == nil || !os.IsNotExist(err) {
			t.Fatalf("missing directory error=%v", err)
		}
	})
}

func TestRemoveDurablySyncsParentDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte("data"), DefaultFileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, DefaultDirMode) }()
	if err := RemoveDurably(path); err == nil {
		t.Fatal("durable remove did not sync the unreadable parent directory")
	}
}

func TestReadJSONRejectsWorldReadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(`{"name":"prod"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	err := ReadJSON(path, &out, "test")
	if err == nil || !strings.Contains(err.Error(), "group/world accessible") {
		t.Fatalf("expected private-file error, got %v", err)
	}
}

func TestResolveUnderRootAcceptsPathInsideRootAndRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "servers", "web", "credentials.json")
	got, err := ResolveUnderRoot(inside, root, "credentials")
	if err != nil {
		t.Fatalf("path inside root should resolve: %v", err)
	}
	if got != inside {
		t.Fatalf("resolved = %q, want %q", got, inside)
	}
	for _, bad := range []string{
		filepath.Join(filepath.Dir(root), "escape.json"),
		root,
	} {
		if _, err := ResolveUnderRoot(bad, root, "credentials"); err == nil {
			t.Fatalf("expected refusal for %q", bad)
		}
	}
}

func TestResolveUnderRootRequiresAbsolutePath(t *testing.T) {
	if _, err := ResolveUnderRoot("relative/path.json", t.TempDir(), "credentials"); err == nil ||
		!strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("expected absolute-path requirement, got %v", err)
	}
}

func TestRejectSymlinkPathRejectsAncestor(t *testing.T) {
	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	err := RejectSymlinkPath(filepath.Join(link, "data.json"), home, "test", "save")
	if err == nil || !strings.Contains(err.Error(), "symlink test path") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
}
