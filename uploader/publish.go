package uploader

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Hidden by hideStagingFS so clients can't observe in-flight uploads.
const (
	stagingDir       = ".tmp"
	stagingDirPrefix = stagingDir + "/"
)

// Returned by swapPaths on non-Linux or when the filesystem (NFS, old kernels)
// rejects RENAME_EXCHANGE; callers fall back to the two-step rename.
var errSwapUnsupported = errors.New("atomic path exchange not supported")

func isStagingPath(p string) bool {
	trimmed := strings.TrimPrefix(filepath.ToSlash(p), "/")
	return trimmed == stagingDir || strings.HasPrefix(trimmed, stagingDirPrefix)
}

func reserveStagingName(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to generate staging name: %w", err)
	}

	return filepath.Join(absStagePath, prefix+hex.EncodeToString(b[:])), nil
}

// Only entries with these prefixes are swept on startup; anything else in
// .tmp/ is assumed to be human-placed and preserved.
var stagingArtifactPrefixes = []string{"up-", "tar-", "old-"}

func looksLikeStagingArtifact(name string) bool {
	for _, p := range stagingArtifactPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return false
}

func ensureParentDir(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create parent: %w", err)
	}

	return nil
}

// Fail fast at startup so misconfigured pods crash with a clear log instead
// of 500ing on the first upload. Error messages name UPLOADER_DIRECTORY and
// the relevant k8s knobs so operators can diagnose without reading code.
func setupStagingDir() error {
	stage := absStagePath

	if err := os.MkdirAll(stage, 0o755); err != nil {
		return fmt.Errorf("create staging dir %s: %w "+
			"(verify UPLOADER_DIRECTORY is writable by the pod user; in Kubernetes "+
			"set securityContext.fsGroup to match runAsGroup so the PVC is chowned correctly)",
			stage, err)
	}

	const wantedStageMode = os.FileMode(0o755)

	if info, err := os.Stat(stage); err == nil {
		if info.Mode().Perm() != wantedStageMode {
			if chmodErr := os.Chmod(stage, wantedStageMode); chmodErr != nil {
				log.Printf("staging dir %s has perms %#o; chmod to %#o failed: %v "+
					"(likely owned by a different UID — the write probe below "+
					"will fail with a clearer error if uploads can't proceed)",
					stage, info.Mode().Perm(), wantedStageMode, chmodErr)
			}
		}
	}

	sentinel := filepath.Join(stage, ".krci-cache-write-probe")
	if err := os.WriteFile(sentinel, nil, 0o600); err != nil {
		return fmt.Errorf("staging dir %s exists but is not writable: %w "+
			"(check that the pod's runAsUser owns it, or that fsGroup matches; "+
			"if readOnlyRootFilesystem=true, ensure UPLOADER_DIRECTORY points at a writable volume)",
			stage, err)
	}

	_ = os.Remove(sentinel)

	sweepStagingOrphans(stage)

	return nil
}

func sweepStagingOrphans(stage string) {
	entries, err := os.ReadDir(stage)
	if err != nil {
		log.Printf("staging sweep: read %s: %v", stage, err)
		return
	}

	removed := 0

	for _, e := range entries {
		if !looksLikeStagingArtifact(e.Name()) {
			continue
		}

		full := filepath.Join(stage, e.Name())
		if err := os.RemoveAll(full); err != nil {
			log.Printf("staging sweep: failed to remove %s: %v", full, err)
			continue
		}

		removed++
	}

	if removed > 0 {
		log.Printf("staging sweep: reclaimed %d orphaned entries from previous runs", removed)
	}
}

// Atomically replaces dst with the bytes from r via temp file + rename(2).
// Concurrent publishers race on the rename; last write wins, no torn bytes.
func publishFile(dst string, r io.Reader) error {
	if err := ensureParentDir(dst); err != nil {
		return err
	}

	f, err := os.CreateTemp(absStagePath, "up-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}

	tmpPath := f.Name()
	committed := false

	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	// CreateTemp's 0600 default would break sidecars/backups running as other UIDs.
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}

	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return fmt.Errorf("write temp: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("publish rename: %w", err)
	}

	committed = true

	return nil
}

// Replaces dst with stage atomically. Last-publisher-wins; uses
// RENAME_EXCHANGE on Linux and a two-step rename elsewhere. The two-step
// path retries on ENOTEMPTY/EEXIST when a concurrent publisher slots in
// between our move-aside and our rename.
func publishDir(dst, stage string) error {
	if err := ensureParentDir(dst); err != nil {
		return err
	}

	const maxAttempts = 8

	backoff := time.Millisecond

	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := tryPublishDir(dst, stage)
		if err == nil {
			return nil
		}

		if !isPublishRaceError(err) {
			return err
		}

		lastErr = err

		time.Sleep(backoff)
		backoff *= 2
	}

	return fmt.Errorf("publish gave up after %d attempts: %w", maxAttempts, lastErr)
}

func tryPublishDir(dst, stage string) error {
	switch err := swapPaths(stage, dst); {
	case err == nil:
		go removeAllLogged(stage)
		return nil
	case errors.Is(err, errSwapUnsupported), errors.Is(err, syscall.ENOENT):
		// Fall through to two-step rename: dst doesn't exist yet, or the
		// kernel/filesystem doesn't support RENAME_EXCHANGE.
	default:
		return err
	}

	aside, err := reserveStagingName("old-")
	if err != nil {
		return err
	}

	if err := os.Rename(dst, aside); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("move aside: %w", err)
		}

		aside = ""
	}

	if err := os.Rename(stage, dst); err != nil {
		if aside != "" {
			if restoreErr := os.Rename(aside, dst); restoreErr != nil {
				log.Printf("publishDir: failed to restore %s: %v (publish err: %v)", dst, restoreErr, err)
			}
		}

		return err
	}

	if aside != "" {
		go removeAllLogged(aside)
	}

	return nil
}

func isPublishRaceError(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) ||
		errors.Is(err, syscall.EEXIST) ||
		errors.Is(err, syscall.ENOTDIR)
}

func removeAllLogged(path string) {
	if err := os.RemoveAll(path); err != nil {
		log.Printf("publish cleanup: failed to remove %s: %v", path, err)
	}
}
