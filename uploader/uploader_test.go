package uploader

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthCheck(t *testing.T) {
	e, _ := concurrencyServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := healthCheck(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"healthy"`)
}

func TestUploadSimpleFile(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	req := buildUploadRequest(t, "test.txt", []byte("test content"), "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), "File has been uploaded to")

	content, err := os.ReadFile(filepath.Join(tempdir, "test.txt"))
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))
}

func TestUploadDirectoryTraversal(t *testing.T) {
	e, _ := concurrencyServer(t)

	req := buildUploadRequest(t, "../../../../../../etc/passwd", []byte("malicious content"), "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "DENIED")
}

// TestUploadSiblingPrefixTraversal guards the bug where HasPrefix without the
// path separator allowed escape to a sibling directory whose name happens to
// begin with the upload dir name (e.g. upload dir "/data" matched "/data-evil").
func TestUploadSiblingPrefixTraversal(t *testing.T) {
	parent, err := os.MkdirTemp("", "uploader-prefix-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(parent) })

	uploadDir := filepath.Join(parent, "data")
	siblingDir := filepath.Join(parent, "data-evil")

	require.NoError(t, os.Mkdir(uploadDir, 0o755))
	require.NoError(t, os.Mkdir(siblingDir, 0o755))

	originalDir := directory

	require.NoError(t, setUploadDirectory(uploadDir))

	t.Cleanup(func() {
		_ = setUploadDirectory(originalDir)
	})

	// Production handler requires the staging dir; streamed uploads land
	// there before path validation runs.
	require.NoError(t, setupStagingDir())

	e := echo.New()
	e.POST("/upload", upload)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "pwned.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("should not land in sibling"))
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("path", "../data-evil/pwned.txt"))
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "must reject sibling-prefix escape")

	_, statErr := os.Stat(filepath.Join(siblingDir, "pwned.txt"))
	assert.True(t, os.IsNotExist(statErr), "file must not have been written to sibling dir")
}

func TestDeleteFile(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	testFile := filepath.Join(tempdir, "delete-me.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("delete this"), 0644))

	req := buildDeleteRequest(t, "/upload", map[string]string{"path": "delete-me.txt"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "has been deleted")

	_, err := os.Stat(testFile)
	assert.True(t, os.IsNotExist(err))
}

// TestDeleteRejectsEmptyPath pins down the security guard that prevents a
// missing or unparseable form body from accidentally wiping the cache root.
// Without the guard, the previous behavior was: empty path -> safeJoin
// returns the upload directory itself -> RemoveAll deletes everything.
func TestDeleteRejectsEmptyPath(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	// Seed a file so we'd notice if the root got wiped.
	require.NoError(t, os.WriteFile(filepath.Join(tempdir, "guard.txt"), []byte("keep"), 0o644))

	req := buildDeleteRequest(t, "/upload", map[string]string{"path": ""})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	_, err := os.Stat(filepath.Join(tempdir, "guard.txt"))
	assert.NoError(t, err, "upload root must not be wiped by an empty-path DELETE")
}

// saveServerGlobals snapshots the package-level `directory`, `host`, and
// `port` (which `loadConfig` mutates as side effects) and registers a cleanup
// that restores them. Restoring `directory` via `setUploadDirectory` keeps
// `absRootDir` consistent with it.
func saveServerGlobals(t *testing.T) {
	t.Helper()

	originalDir, originalHost, originalPort := directory, host, port

	t.Cleanup(func() {
		_ = setUploadDirectory(originalDir)
		host = originalHost
		port = originalPort
	})
}

func TestLoadConfig(t *testing.T) {
	saveServerGlobals(t)

	tempdir := t.TempDir()

	t.Setenv("UPLOADER_DIRECTORY", tempdir)
	t.Setenv("UPLOADER_HOST", "test-host")
	t.Setenv("UPLOADER_PORT", "9999")
	t.Setenv("UPLOADER_UPLOAD_CREDENTIALS", "user:pass")
	t.Setenv("UPLOADER_MAX_UPLOAD_SIZE", "100M")
	t.Setenv("UPLOADER_SHUTDOWN_TIMEOUT", "30s")

	cfg, err := loadConfig()
	require.NoError(t, err)

	assert.Equal(t, tempdir, directory)
	assert.Equal(t, "test-host", host)
	assert.Equal(t, "9999", port)

	expectedAbs, err := filepath.Abs(tempdir)
	require.NoError(t, err)
	assert.Equal(t, expectedAbs, absRootDir)

	assert.Equal(t, "user:pass", cfg.credentials)
	assert.Equal(t, "100M", cfg.maxUploadSize)
	assert.Equal(t, 30*time.Second, cfg.shutdownTimeout)
}

func TestLoadConfigRejectsBadCredentials(t *testing.T) {
	saveServerGlobals(t)

	t.Setenv("UPLOADER_UPLOAD_CREDENTIALS", "missing-colon")

	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UPLOADER_UPLOAD_CREDENTIALS")
}

func TestLoadConfigRejectsBadShutdownTimeout(t *testing.T) {
	saveServerGlobals(t)

	t.Setenv("UPLOADER_SHUTDOWN_TIMEOUT", "not-a-duration")

	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UPLOADER_SHUTDOWN_TIMEOUT")
}
