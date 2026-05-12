package uploader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publishDir's retry loop fires on ENOTEMPTY/EEXIST/ENOTDIR — race outcomes
// of the move-aside-then-rename sequence on non-Linux fallback. We trigger
// it deterministically by stripping the staging dir so the move-aside step
// silently no-ops via fs.ErrNotExist; the subsequent rename(stage, dst)
// then hits a still-existing non-empty dst and returns the race error.
func TestPublishDirHonorsContextCancelMidRetry(t *testing.T) {
	if runtime.GOOS == "linux" {
		// On Linux, swapPaths uses RENAME_EXCHANGE which atomically swaps
		// in one syscall — the retry loop is never entered. The retry-with-
		// cancel path is exercised on the !linux fallback below.
		t.Skip("retry loop only fires on the non-Linux fallback path")
	}

	base := t.TempDir()
	require.NoError(t, setUploadDirectory(base))
	// Intentionally do NOT call setupStagingDir(): without absStagePath on
	// disk, reserveStagingName produces a path whose parent is missing, so
	// the move-aside rename returns ENOENT and the code treats it as
	// "nothing to move". rename(stage, dst) then races against a non-empty
	// dst and reliably returns ENOTEMPTY.

	dst := filepath.Join(base, "dst")
	require.NoError(t, os.MkdirAll(dst, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "marker"), []byte("x"), 0o644))

	stage := filepath.Join(base, "stage")
	require.NoError(t, os.MkdirAll(stage, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stage, "y"), []byte("y"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())

	// Fire well before the cumulative backoff (1+2+4+8+16+32+64+128 = 255ms)
	// would otherwise complete on its own.
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := publishDir(ctx, dst, stage)
	elapsed := time.Since(start)

	require.Truef(t, errors.Is(err, context.Canceled),
		"expected context.Canceled, got %v", err)

	// Without honoring ctx, publishDir would burn ~255ms in time.Sleep.
	// 100ms is comfortably above 5ms scheduling jitter and well below 255ms.
	assert.Lessf(t, elapsed, 100*time.Millisecond,
		"publishDir did not exit promptly on cancel: took %v", elapsed)
}

// Pre-canceled context: tryPublishDir's ctx.Err() guard short-circuits
// before any syscalls fire, so publishDir returns immediately without
// even attempting the rename.
func TestPublishDirHonorsCanceledContextImmediately(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, setUploadDirectory(base))
	require.NoError(t, setupStagingDir())

	dst := filepath.Join(base, "dst")
	stage := filepath.Join(base, "stage")
	require.NoError(t, os.MkdirAll(stage, 0o755))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := publishDir(ctx, dst, stage)
	require.Truef(t, errors.Is(err, context.Canceled),
		"expected context.Canceled, got %v", err)

	// The successful-publish path would have moved stage onto dst; verify
	// it didn't.
	_, statErr := os.Stat(dst)
	assert.Truef(t, errors.Is(statErr, os.ErrNotExist),
		"publish should not have happened, but dst exists")
}
