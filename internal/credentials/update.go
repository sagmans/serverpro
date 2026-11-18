package credentials

import (
	"context"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
)

// credentialsWriterLockSuffix names the exclusive writer lock beside the
// credential file, mirroring how config writes serialize on <path>.lock. WHY a
// second lock: the shared local-artifact guard only excludes namespace cleanup,
// while cooperating workflows hold that same guard shared for their entire run
// (state.lockLocalArtifact), so acquiring it exclusively here would deadlock
// against every create/import/delete workflow.
const credentialsWriterLockSuffix = ".lock"

// writeTargetPath resolves the one credential location every writer targets.
// Centralizing it keeps load/merge/write on the identical validated path.
func writeTargetPath(cfg config.Config) (string, error) {
	if err := validateConfigScope(cfg); err != nil {
		return "", err
	}
	path := config.Expand(cfg.Credentials.JSONPath)
	if path == "" {
		return "", fmt.Errorf("credentials JSON path required")
	}
	path, err := safeCredentialPath(cfg.Namespace, cfg.Server, path, "save")
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkInCredentialAbsPath(path, "save"); err != nil {
		return "", err
	}
	return path, nil
}

// withCredentialWriteLock serializes local writers that otherwise lose each
// other's fields to whole-file replacement. Lock order is fixed for every
// path: key lifecycle, then the namespace guard, then the file. WHY: rotation
// holds the lifecycle lock while it takes each file lock, so a writer that
// took the file lock first and then waited on the lifecycle lock would
// deadlock against it. The key is created before any lock is held, and the
// shared lifecycle lock keeps rotation and restore from changing the key
// between reading the recipients and publishing the file.
func withCredentialWriteLock(path string, write func() error) error {
	if _, err := loadOrCreateIdentity(); err != nil {
		return err
	}
	unlockKey, err := lockKeyLifecycleShared()
	if err != nil {
		return err
	}
	defer unlockKey()
	return withCredentialFileLock(path, write)
}

// withCredentialFileLock takes the guard and per-file locks without the
// lifecycle lock. Only rotation calls it directly, because it already holds
// the lifecycle lock exclusively.
func withCredentialFileLock(path string, write func() error) error {
	unlockGuard, err := privatefile.LockSharedContext(context.Background(), config.LocalArtifactGuardPath())
	if err != nil {
		return err
	}
	defer unlockGuard()
	unlockWriter, err := privatefile.Lock(path + credentialsWriterLockSuffix)
	if err != nil {
		return err
	}
	defer unlockWriter()
	return write()
}

// writeCredentialSet publishes a complete set as one atomic private file. The
// body is encrypted before it reaches the temp file, so plaintext secrets never
// touch the disk; the rename replaces any legacy plaintext file in one step.
func writeCredentialSet(path string, creds Set) error {
	body, err := sealCredentialSet(creds, nil)
	if err != nil {
		return err
	}
	return publishSealedCredentials(path, body)
}

// publishSealedCredentials is the one place sealed bytes reach the disk, so
// saves, migration, and rotation share the symlink re-check and durability.
func publishSealedCredentials(path string, sealed []byte) error {
	return privatefile.AtomicWrite(path, sealed, privatefile.WriteOptions{TempPattern: ".credentials-*.tmp", Sync: true, BeforeRename: func() error {
		return rejectSymlinkInCredentialAbsPath(path, "save")
	}})
}

// Update applies a partial change to the stored set under the writer lock.
// WHY: callers used to load a snapshot, prompt, and save it whole, which rolled
// a concurrent run's freshly rotated credential back to the stale snapshot;
// re-reading inside the lock keeps every other field as the operator last left it.
func Update(cfg config.Config, mutate func(*Set) error) error {
	path, err := writeTargetPath(cfg)
	if err != nil {
		return err
	}
	return withCredentialWriteLock(path, func() error {
		current, err := LoadPartial(cfg)
		if err != nil {
			return err
		}
		if err := mutate(&current); err != nil {
			return err
		}
		current.Namespace = cfg.Namespace
		current.Server = cfg.Server
		if err := current.ValidateTarget(cfg.Namespace, cfg.Server); err != nil {
			return err
		}
		return writeCredentialSet(path, current)
	})
}
