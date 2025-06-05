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

func httpUploadMultiPart(s, p string) *http.Request {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "hello.txt")
	_, _ = part.Write([]byte(s))
	_ = writer.WriteField("path", p)
	_ = writer.Close()

	r, _ := http.NewRequest(http.MethodPost, "/upload", body)
	r.Header.Set("Content-Type", writer.FormDataContentType())

	return r
}

func setupTestServer(t *testing.T) (*Server, string) {
	tempdir, err := os.MkdirTemp("", "test-uploader")
	require.NoError(t, err)

	config := Config{
		Host:      "localhost",
		Port:      "8080",
		Directory: tempdir,
	}

	server := NewServer(config)

	return server, tempdir
}

func TestMultipleDirectory(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	expectedString := "HELLO MOTO"
	targetPath := "a/foo/bar/moto.txt"

	req := httpUploadMultiPart(expectedString, targetPath)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	dat, err := os.ReadFile(filepath.Join(tempdir, targetPath))
	assert.NoError(t, err)
	assert.Equal(t, expectedString, string(dat))
}

func TestUploaderSimple(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	expectedString := "HELLO SIMPLE MOTO"
	targetPath := "moto.txt"

	req := httpUploadMultiPart(expectedString, targetPath)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	dat, err := os.ReadFile(filepath.Join(tempdir, targetPath))
	assert.NoError(t, err)
	assert.Equal(t, expectedString, string(dat))
}

func TestUploaderTraversal(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	expectedString := "HELLO MOTO"
	targetPath := "../../../../../../../../../../etc/passwd"

	req := httpUploadMultiPart(expectedString, targetPath)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	assert.Error(t, err)

	he, ok := err.(*echo.HTTPError)
	if assert.True(t, ok) {
		assert.Equal(t, http.StatusForbidden, he.Code)
	}
}

func TestUploaderDelete(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	fpath := filepath.Join(tempdir, "foo.txt")
	fp, err := os.Create(fpath)
	require.NoError(t, err)
	fp.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "foo.txt")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodDelete, "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()

	context := server.echo.NewContext(req, rec)
	err = server.deleteFile(context)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	_, err = os.Stat(fpath)
	assert.True(t, os.IsNotExist(err))
}

func TestDeleteFilesOlderThanOneDay(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	fpath := filepath.Join(tempdir, "foo.txt")
	fp, err := os.Create(fpath)
	require.NoError(t, err)
	fp.Close()

	// Set file timestamp to 25 hours ago
	timestamp := time.Now().Add(-(time.Duration(1) * 25 * time.Hour))
	err = os.Chtimes(fpath, timestamp, timestamp)
	require.NoError(t, err)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "1")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodDelete, "/delete", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()

	context := server.echo.NewContext(req, rec)
	err = server.deleteOldFiles(context)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	_, err = os.Stat(fpath)
	assert.True(t, os.IsNotExist(err))
}

func TestDeleteFilesOlderThanTwoDay(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	fpath := filepath.Join(tempdir, "foo.txt")
	fp, err := os.Create(fpath)
	require.NoError(t, err)
	fp.Close()

	// Set file timestamp to 49 hours ago (2+ days)
	timestamp := time.Now().Add(-(time.Duration(2) * 25 * time.Hour))
	err = os.Chtimes(fpath, timestamp, timestamp)
	require.NoError(t, err)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "2")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodDelete, "/delete", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()

	context := server.echo.NewContext(req, rec)
	err = server.deleteOldFiles(context)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	_, err = os.Stat(fpath)
	assert.True(t, os.IsNotExist(err))
}

func TestConfigLoadFromEnv(t *testing.T) {
	// Save original env values
	originalHost := os.Getenv("UPLOADER_HOST")
	originalPort := os.Getenv("UPLOADER_PORT")
	originalDir := os.Getenv("UPLOADER_DIRECTORY")

	// Set test env values
	os.Setenv("UPLOADER_HOST", "test-host")
	os.Setenv("UPLOADER_PORT", "9999")
	os.Setenv("UPLOADER_DIRECTORY", "/test/dir")

	// Load config
	config := LoadConfig()

	// Verify config loaded from env
	assert.Equal(t, "test-host", config.Host)
	assert.Equal(t, "9999", config.Port)
	assert.Equal(t, "/test/dir", config.Directory)

	// Restore original env values
	os.Setenv("UPLOADER_HOST", originalHost)
	os.Setenv("UPLOADER_PORT", originalPort)
	os.Setenv("UPLOADER_DIRECTORY", originalDir)
}

func TestPathSafety(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"Safe relative path", filepath.Join(tempdir, "file.txt"), true},
		{"Safe nested path", filepath.Join(tempdir, "subdir/file.txt"), true},
		{"Unsafe traversal", "/etc/passwd", false},
		{"Unsafe relative traversal", "../../etc/passwd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := server.isPathSafe(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
