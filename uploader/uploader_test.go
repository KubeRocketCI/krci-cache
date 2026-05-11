// Package uploader provides HTTP upload server functionality with support for file uploads and tar.gz extraction.
package uploader

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestServer creates a minimal test server for the simplified implementation
func setupTestServer(t *testing.T) (*echo.Echo, string) {
	// Create temp directory
	tempdir, err := os.MkdirTemp("", "uploader-test-*")
	require.NoError(t, err)

	originalDir := directory

	require.NoError(t, setUploadDirectory(tempdir))

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.Static("/", directory)
	e.GET("/health", healthCheck)
	e.HEAD("/:path", lastModified)
	e.POST("/upload", upload)
	e.DELETE("/upload", uploaderDelete)
	e.DELETE("/delete", deleteOldFilesOfDir)

	t.Cleanup(func() {
		_ = setUploadDirectory(originalDir)

		os.RemoveAll(tempdir)
	})

	return e, tempdir
}

func TestHealthCheck(t *testing.T) {
	e, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := healthCheck(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"healthy"`)
}

func TestUploadSimpleFile(t *testing.T) {
	e, tempdir := setupTestServer(t)

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file part
	part, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("test content"))
	require.NoError(t, err)

	// Add path field
	err = writer.WriteField("path", "test.txt")
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()

	// Execute request
	e.ServeHTTP(rec, req)

	// Check response
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), "File has been uploaded to")

	// Verify file was created
	filePath := filepath.Join(tempdir, "test.txt")
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))
}

func TestUploadDirectoryTraversal(t *testing.T) {
	e, _ := setupTestServer(t)

	// Create multipart form with malicious path
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file part
	part, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("malicious content"))
	require.NoError(t, err)

	// Add malicious path field
	err = writer.WriteField("path", "../../../../../../etc/passwd")
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()

	// Execute request
	e.ServeHTTP(rec, req)

	// Should be forbidden
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
	e, tempdir := setupTestServer(t)

	// Create a test file
	testFile := filepath.Join(tempdir, "delete-me.txt")
	err := os.WriteFile(testFile, []byte("delete this"), 0644)
	require.NoError(t, err)

	// Create delete request
	body := strings.NewReader("path=delete-me.txt")
	req := httptest.NewRequest(http.MethodDelete, "/upload", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()

	// Execute request
	e.ServeHTTP(rec, req)

	// Check response
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "has been deleted")

	// Verify file was deleted
	_, err = os.Stat(testFile)
	assert.True(t, os.IsNotExist(err))
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
