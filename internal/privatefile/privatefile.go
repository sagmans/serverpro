package privatefile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sagmans/serverpro/internal/filedescriptor"
	"golang.org/x/sys/unix"
)

const (
	DefaultFileMode   os.FileMode = 0o600
	DefaultDirMode    os.FileMode = 0o700
	lockRetryInterval             = 10 * time.Millisecond
)

type WriteOptions struct {
	FileMode     os.FileMode
	DirMode      os.FileMode
	TempPattern  string
	Sync         bool
	BeforeRename func() error
}

// Lock coordinates readers that must compare and publish one private file as a
// single authority decision.
func Lock(path string) (func(), error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DefaultDirMode); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, DefaultDirMode); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, DefaultFileMode)
	if err != nil {
		return nil, err
	}
	fd, err := filedescriptor.Int(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func LockSharedContext(ctx context.Context, path string) (func(), error) {
	return lockContext(ctx, path, syscall.LOCK_SH)
}

func LockExclusiveContext(ctx context.Context, path string) (func(), error) {
	return lockContext(ctx, path, syscall.LOCK_EX)
}

func lockContext(ctx context.Context, path string, mode int) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, fd, err := openLockFile(path)
	if err != nil {
		return nil, err
	}
	retry := time.NewTicker(lockRetryInterval)
	defer retry.Stop()
	for {
		err = syscall.Flock(fd, mode|syscall.LOCK_NB)
		if err == nil {
			return lockRelease(file, fd), nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-retry.C:
		}
	}
}

func openLockFile(path string) (*os.File, int, error) {
	parent, name, err := OpenParentDirectory(path, true)
	if err != nil {
		return nil, 0, err
	}
	fd, openErr := unix.Openat(int(parent.Fd()), name, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(DefaultFileMode))
	closeErr := parent.Close()
	if openErr != nil {
		return nil, 0, openErr
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, 0, fmt.Errorf("open lock file %s", path)
	}
	if closeErr != nil {
		_ = file.Close()
		return nil, 0, closeErr
	}
	fileDescriptor, err := filedescriptor.Int(file)
	if err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	return file, fileDescriptor, nil
}

func OpenParentDirectory(path string, create bool) (*os.File, string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	current, err := os.Open(string(os.PathSeparator))
	if err != nil {
		return nil, "", err
	}
	parent := filepath.Dir(absolutePath)
	firstComponent := true
	for _, component := range strings.Split(strings.TrimPrefix(parent, string(os.PathSeparator)), string(os.PathSeparator)) {
		if component == "" || component == "." {
			continue
		}
		if create {
			mkdirErr := unix.Mkdirat(int(current.Fd()), component, uint32(DefaultDirMode))
			if mkdirErr != nil && !errors.Is(mkdirErr, syscall.EEXIST) {
				_ = current.Close()
				return nil, "", mkdirErr
			}
		}
		flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC
		// macOS can expose the root-owned first component through a platform alias.
		if !firstComponent {
			flags |= unix.O_NOFOLLOW
		}
		fd, openErr := unix.Openat(int(current.Fd()), component, flags, 0)
		if openErr != nil {
			_ = current.Close()
			return nil, "", openErr
		}
		next := os.NewFile(uintptr(fd), component)
		if next == nil {
			_ = unix.Close(fd)
			_ = current.Close()
			return nil, "", fmt.Errorf("open path component %s", component)
		}
		if err := current.Close(); err != nil {
			_ = next.Close()
			return nil, "", err
		}
		current = next
		firstComponent = false
	}
	return current, filepath.Base(absolutePath), nil
}

func lockRelease(file *os.File, fd int) func() {
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = file.Close()
	}
}

func AtomicWrite(path string, body []byte, opt WriteOptions) error {
	fileMode := opt.FileMode
	if fileMode == 0 {
		fileMode = DefaultFileMode
	}
	dirMode := opt.DirMode
	if dirMode == 0 {
		dirMode = DefaultDirMode
	}
	tempPattern := opt.TempPattern
	if tempPattern == "" {
		tempPattern = ".private-*.tmp"
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if opt.Sync {
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if opt.BeforeRename != nil {
		if err := opt.BeforeRename(); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	if !opt.Sync {
		return nil
	}
	return syncDirectory(dir)
}

// RemoveDurably publishes deletion before returning so registry removal cannot
// be reported successful while only residing in the filesystem cache.
func RemoveDurably(path string) error {
	parent, name, err := OpenParentDirectory(path, false)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	if err := removeAt(parent, name, false); err != nil {
		return err
	}
	return parent.Sync()
}

func RemoveTreeDurably(path string) error {
	parent, name, err := OpenParentDirectory(path, false)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	if err := removeAt(parent, name, true); err != nil {
		return err
	}
	return parent.Sync()
}

func removeAt(parent *os.File, name string, recursive bool) error {
	var info unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(int(parent.Fd()), name, 0)
	}
	if !recursive {
		return unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), name)
	if dir == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open directory %s", name)
	}
	names, err := dir.Readdirnames(-1)
	if err != nil {
		_ = dir.Close()
		return err
	}
	for _, child := range names {
		if err := removeAt(dir, child, true); err != nil {
			_ = dir.Close()
			return err
		}
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	if err := dir.Close(); err != nil {
		return err
	}
	return unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}

func AtomicWriteJSON(path string, value any, opt WriteOptions) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(path, body, opt)
}

func ReadJSON(path string, out any, kind string) error {
	if err := EnsurePrivateDir(path, kind); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s file is group/world accessible: %s", kind, path)
	}
	body, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func EnsurePrivateDir(path, kind string) error {
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("cannot verify %s dir %s: %w", kind, dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s dir %s must not be a symlink", kind, dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s dir %s must not be group/world accessible", kind, dir)
	}
	return nil
}

func ResolveUnderRoot(path, root, kind string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s path must be absolute: %s", kind, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("refuse %s path outside serverpro config root: %s", kind, absPath)
	}
	return absPath, nil
}

func RejectSymlinkPath(absPath, home, kind, action string) error {
	absHome, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absHome, absPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("refuse %s path outside home: %s", kind, absPath)
	}
	current := absHome
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return symlinkPathError(kind, action, current)
		}
	}
	return nil
}

func symlinkPathError(kind, action, path string) error {
	if action == "" {
		return fmt.Errorf("refuse symlink %s path: %s", kind, path)
	}
	return fmt.Errorf("refuse to %s symlink %s path: %s", action, kind, path)
}
