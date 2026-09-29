package doctor

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// Private modes prevent diagnostic evidence from reaching other local users.
	reportDirectoryMode  = 0o700
	reportFileMode       = 0o600
	reportNonprivateMode = 0o077
	// Writable ancestors permit replacement unless the system enforces sticky ownership.
	reportAncestorWriteMode = 0o022
	reportSystemUID         = 0
	// A small history bounds sensitive evidence retained on disk.
	reportRetention = 5
	// Random names avoid overwriting reports from simultaneous invocations.
	reportRandomBytes = 16
	reportPrefix      = "doctor-"
	reportSuffix      = ".json"
	reportRootPrefix  = "serverpro-"
	reportDirectory   = "doctor"
	// Descriptor traversal must never follow a substituted symbolic link.
	reportDirectoryFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
)

// Save keeps diagnostic evidence private without repairing untrusted paths.
func (r Report) Save(namespace, server string) (string, error) {
	for _, name := range []string{namespace, server} {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
			return "", fmt.Errorf("invalid report path component %q", name)
		}
	}
	// macOS exposes its temporary directory through platform-owned aliases.
	// Resolve only the temp base; report directories must never permit aliases.
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("resolve report temporary directory: %w", err)
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return "", err
	}
	fd, err := unix.Open(string(filepath.Separator), reportDirectoryFlags, 0)
	if err != nil {
		return "", err
	}
	if err := validateReportAncestor(fd); err != nil {
		_ = unix.Close(fd)
		return "", err
	}
	// Keep all traversal relative to verified descriptors, including the temp base.
	for _, part := range strings.Split(strings.TrimPrefix(base, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next, openErr := unix.Openat(fd, part, reportDirectoryFlags, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return "", fmt.Errorf("open report temporary directory: %w", openErr)
		}
		fd = next
		if err := validateReportAncestor(fd); err != nil {
			_ = unix.Close(fd)
			return "", err
		}
	}
	defer func() { _ = unix.Close(fd) }()
	path := base
	for _, part := range []string{fmt.Sprintf("%s%d", reportRootPrefix, os.Getuid()), reportDirectory, namespace, server} {
		if err := unix.Mkdirat(fd, part, reportDirectoryMode); err != nil && !errors.Is(err, unix.EEXIST) {
			return "", err
		}
		next, err := unix.Openat(fd, part, reportDirectoryFlags, 0)
		if err != nil {
			return "", fmt.Errorf("open private report directory: %w", err)
		}
		var info unix.Stat_t
		err = unix.Fstat(next, &info)
		if err == nil && (int64(info.Uid) != int64(os.Getuid()) || info.Mode&reportNonprivateMode != 0) {
			err = fmt.Errorf("report directory %q must be owned by the current user and private", part)
		}
		if err != nil {
			_ = unix.Close(next)
			return "", err
		}
		_ = unix.Close(fd)
		fd = next
		path = filepath.Join(path, part)
	}
	// Serialize publication and pruning so concurrent writers cannot over-prune history.
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock report directory: %w", err)
	}
	defer func() { _ = unix.Flock(fd, unix.LOCK_UN) }()
	token := make([]byte, reportRandomBytes)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	name := reportPrefix + hex.EncodeToString(token) + reportSuffix
	fileFD, err := unix.Openat(fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, reportFileMode)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fileFD), name)
	writeErr := r.Write(file)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = unix.Unlinkat(fd, name, 0)
		return "", fmt.Errorf("write doctor report: %w", err)
	}
	pruneReports(fd, name)
	return filepath.Join(path, name), nil
}

// validateReportAncestor prevents other users from replacing trusted path components.
func validateReportAncestor(fd int) error {
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return err
	}
	owned := info.Uid == reportSystemUID || int64(info.Uid) == int64(os.Getuid())
	protected := info.Mode&reportAncestorWriteMode == 0 || (info.Uid == reportSystemUID && info.Mode&unix.S_ISVTX != 0)
	if !owned || !protected {
		return fmt.Errorf("report temporary ancestor must have trusted ownership and permissions")
	}
	return nil
}

// pruneReports never lets optional history cleanup invalidate a saved report.
func pruneReports(fd int, keep string) {
	listingFD, err := unix.Openat(fd, ".", reportDirectoryFlags, 0)
	if err != nil {
		return
	}
	listing := os.NewFile(uintptr(listingFD), reportDirectory)
	entries, err := listing.ReadDir(-1)
	_ = listing.Close()
	if err != nil {
		return
	}
	type candidate struct {
		name     string
		modified time.Time
		info     unix.Stat_t
	}
	var candidates []candidate
	for _, entry := range entries {
		name := entry.Name()
		if name == keep || !strings.HasPrefix(name, reportPrefix) || !strings.HasSuffix(name, reportSuffix) {
			continue
		}
		token := strings.TrimSuffix(strings.TrimPrefix(name, reportPrefix), reportSuffix)
		decoded, err := hex.DecodeString(token)
		if err != nil || len(decoded) != reportRandomBytes {
			continue
		}
		var info unix.Stat_t
		if unix.Fstatat(fd, name, &info, unix.AT_SYMLINK_NOFOLLOW) != nil || !privateReportFile(info) {
			continue
		}
		fileFD, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		file := os.NewFile(uintptr(fileFD), name)
		stat, err := file.Stat()
		_ = file.Close()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{name, stat.ModTime(), info})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modified.Equal(candidates[j].modified) {
			return candidates[i].name > candidates[j].name
		}
		return candidates[i].modified.After(candidates[j].modified)
	})
	for i := reportRetention - 1; i < len(candidates); i++ {
		item := candidates[i]
		var info unix.Stat_t
		// Revalidation excludes replacements observed since the history scan.
		if unix.Fstatat(fd, item.name, &info, unix.AT_SYMLINK_NOFOLLOW) == nil && privateReportFile(info) && info.Dev == item.info.Dev && info.Ino == item.info.Ino {
			_ = unix.Unlinkat(fd, item.name, 0)
		}
	}
}

// Hard-linked files may belong to another purpose despite a matching basename.
func privateReportFile(info unix.Stat_t) bool {
	return info.Mode&unix.S_IFMT == unix.S_IFREG && int64(info.Uid) == int64(os.Getuid()) && info.Mode&reportNonprivateMode == 0 && info.Nlink == 1
}
