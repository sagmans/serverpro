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
// other's fields to whole-file replacement.
func withCredentialWriteLock(path string, write func() error) error {
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

// writeCredentialSet publishes a complete set as one atomic private file.
func writeCredentialSet(path string, creds Set) error {
	return privatefile.AtomicWriteJSON(path, creds, privatefile.WriteOptions{TempPattern: ".credentials-*.tmp", Sync: true, BeforeRename: func() error {
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
