package privatefile

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	DefaultFileMode os.FileMode = 0o600
	DefaultDirMode  os.FileMode = 0o700
)

type WriteOptions struct {
	FileMode     os.FileMode
	DirMode      os.FileMode
	TempPattern  string
	Sync         bool
	BeforeRename func() error
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
	return os.Rename(tmpPath, path)
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
