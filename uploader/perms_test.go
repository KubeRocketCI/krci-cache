package uploader

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const skipOnWindows = "windows"

func withStagingDir(t *testing.T) string {
	t.Helper()
	return withStagingDirAt(t, t.TempDir())
}

func withStagingDirAt(t *testing.T, dir string) string {
	t.Helper()

	originalDir := directory

	require.NoError(t, setUploadDirectory(dir))

	t.Cleanup(func() {
		_ = setUploadDirectory(originalDir)
	})

	return dir
}

func TestSetupStagingDir_CreatesMissing(t *testing.T) {
	dir := withStagingDir(t)

	require.NoError(t, setupStagingDir())

	info, err := os.Stat(filepath.Join(dir, stagingDir))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestSetupStagingDir_WidensRestrictivePerms(t *testing.T) {
	if runtime.GOOS == skipOnWindows {
		t.Skip("Unix perms only")
	}

	dir := withStagingDir(t)

	stage := filepath.Join(dir, stagingDir)
	require.NoError(t, os.MkdirAll(stage, 0o700))

	require.NoError(t, setupStagingDir())

	info, err := os.Stat(stage)
	require.NoError(t, err)
	assert.Equalf(t, os.FileMode(0o755), info.Mode().Perm(),
		"setupStagingDir must widen owned .tmp/ to 0755")
}

func TestSetupStagingDir_FailsOnUnwritableParent(t *testing.T) {
	if runtime.GOOS == skipOnWindows {
		t.Skip("Unix perms only")
	}

	if os.Geteuid() == 0 {
		t.Skip("root bypasses DAC perms; can't simulate unwritable parent")
	}

	parent := t.TempDir()
	require.NoError(t, os.Chmod(parent, 0o500)) // r-x: we can stat, can't write

	t.Cleanup(func() {
		// Restore so t.TempDir cleanup can remove it.
		_ = os.Chmod(parent, 0o700)
	})

	withStagingDirAt(t, parent)

	err := setupStagingDir()
	require.Error(t, err)
	assert.Contains(t, err.Error(), parent, "error must name the offending directory")
	assert.Contains(t, err.Error(), "UPLOADER_DIRECTORY", "error must reference the env var operators set")
}

func TestSweepStagingOrphans_RemovesOnlyKnownPrefixes(t *testing.T) {
	dir := withStagingDir(t)
	stage := filepath.Join(dir, stagingDir)
	require.NoError(t, os.MkdirAll(stage, 0o755))

	orphans := []string{"up-aaaa", "tar-bbbb", "old-cccc"}
	keepers := []string{"my-debug-file.txt", "manual-backup", "README"}

	for _, n := range orphans {
		require.NoError(t, os.WriteFile(filepath.Join(stage, n), []byte("x"), 0o644))
	}

	for _, n := range keepers {
		require.NoError(t, os.WriteFile(filepath.Join(stage, n), []byte("y"), 0o644))
	}

	sweepStagingOrphans(stage)

	for _, n := range orphans {
		_, err := os.Stat(filepath.Join(stage, n))
		assert.Truef(t, errors.Is(err, os.ErrNotExist), "orphan %s should be removed", n)
	}

	for _, n := range keepers {
		_, err := os.Stat(filepath.Join(stage, n))
		assert.NoErrorf(t, err, "non-orphan %s must be preserved", n)
	}
}

func TestPublishFile_ProducesMode0644(t *testing.T) {
	if runtime.GOOS == skipOnWindows {
		t.Skip("Unix perms only")
	}

	dir := withStagingDir(t)
	require.NoError(t, setupStagingDir())

	require.NoError(t, publishFile(filepath.Join(dir, "out.bin"), bytes.NewReader([]byte("hello"))))

	info, err := os.Stat(filepath.Join(dir, "out.bin"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestExtractTarGz_PublishedDirIs0755(t *testing.T) {
	if runtime.GOOS == skipOnWindows {
		t.Skip("Unix perms only")
	}

	e, tempdir := concurrencyServer(t)

	req := buildUploadRequest(t, "out-dir", makeTarGz(t, "X", 1), "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equalf(t, http.StatusCreated, rec.Code, "upload failed: %s", rec.Body.String())

	info, err := os.Stat(filepath.Join(tempdir, "out-dir"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}
