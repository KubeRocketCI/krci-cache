package uploader

import (
	"bytes"
	"encoding/json"
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

// Test constants
const (
	// Expected content strings
	expectedSimpleContent = "HELLO"
	expectedLongContent   = "HELLO SIMPLE MOTO"
	expectedLargeContent  = "This is a content that exceeds 10 bytes"
	expectedTestContent   = "test content"

	// Target paths
	targetSimplePath    = "moto.txt"
	targetNestedPath    = "a/foo/bar/moto.txt"
	targetLargePath     = "large.txt"
	targetTestPath      = "foo.txt"
	targetTraversalPath = "../../../../../../../../../../etc/passwd"
	targetExtractedPath = "extracted"

	// Test configuration values
	testHost               = "localhost"
	testPort               = "8080"
	testTimeout            = 30 * time.Second
	testMaxConcurrentLimit = 1
	testMaxSizeLimit       = 10
	testVersion            = "1.0.0"
	testHealthyStatus      = "healthy"

	// Test environment values
	testEnvHost            = "test-host"
	testEnvPort            = "9999"
	testEnvDir             = "/test/dir"
	testEnvMaxSize         = "1048576" // 1MB
	testEnvMaxConcurrent   = "5"
	testEnvTimeout         = "45s"
	testCredentials        = "testuser:testpass"
	testInvalidCredentials = "invalidcredentials"

	// File names and extensions
	testFileName    = "hello.txt"
	testTarGzName   = "test.tar.gz"
	testContentType = "Content-Type"
)

func createTestRequest(content, path string) *http.Request {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", testFileName)
	_, _ = part.Write([]byte(content))
	_ = writer.WriteField("path", path)
	_ = writer.Close()

	r, _ := http.NewRequest(http.MethodPost, "/upload", body)
	r.Header.Set(testContentType, writer.FormDataContentType())

	return r
}

func createTestConfig(tempdir string) Config {
	return Config{
		Host:                 testHost,
		Port:                 testPort,
		Directory:            tempdir,
		MaxUploadSize:        0, // unlimited for tests
		MaxConcurrentUploads: 10,
		RequestTimeout:       testTimeout,
		ReadTimeout:          testTimeout,
		WriteTimeout:         testTimeout,
		ShutdownTimeout:      testTimeout,
	}
}

func createTestConfigWithLimits(tempdir string, maxSize int64, maxConcurrent int) Config {
	config := createTestConfig(tempdir)
	config.MaxUploadSize = maxSize
	config.MaxConcurrentUploads = maxConcurrent

	return config
}

func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	tempdir, err := os.MkdirTemp("", "test-uploader")
	require.NoError(t, err)

	config := createTestConfig(tempdir)
	server := NewServer(config)

	return server, tempdir
}

func setupTestServerWithLimits(t *testing.T, maxSize int64, maxConcurrent int) (*Server, string) {
	t.Helper()

	tempdir, err := os.MkdirTemp("", "test-uploader-limits")
	require.NoError(t, err)

	config := createTestConfigWithLimits(tempdir, maxSize, maxConcurrent)
	server := NewServer(config)

	return server, tempdir
}

func assertJSONResponse(t *testing.T, rec *httptest.ResponseRecorder, expectedPath string) {
	t.Helper()

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["message"], expectedPath)
	assert.Equal(t, expectedPath, response["path"])
}

func assertHTTPError(t *testing.T, err error, expectedCode int) {
	t.Helper()
	require.Error(t, err)
	he, ok := err.(*echo.HTTPError)
	require.True(t, ok)
	assert.Equal(t, expectedCode, he.Code)
}

func TestHealthCheck(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.healthCheck(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, testHealthyStatus, response["status"])
	assert.NotEmpty(t, response["timestamp"])
	assert.Equal(t, testVersion, response["version"])
}

func TestMultipleDirectory(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	req := createTestRequest(expectedSimpleContent, targetNestedPath)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	assertJSONResponse(t, rec, targetNestedPath)

	// Verify file content
	data, err := os.ReadFile(filepath.Join(tempdir, targetNestedPath))
	require.NoError(t, err)
	assert.Equal(t, expectedSimpleContent, string(data))
}

func TestUploaderSimple(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	req := createTestRequest(expectedLongContent, targetSimplePath)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	assertJSONResponse(t, rec, targetSimplePath)

	// Verify file content
	data, err := os.ReadFile(filepath.Join(tempdir, targetSimplePath))
	require.NoError(t, err)
	assert.Equal(t, expectedLongContent, string(data))
}

func TestUploadSizeLimit(t *testing.T) {
	server, tempdir := setupTestServerWithLimits(t, testMaxSizeLimit, 10)
	defer os.RemoveAll(tempdir)

	req := createTestRequest(expectedLargeContent, targetLargePath)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	assertHTTPError(t, err, http.StatusRequestEntityTooLarge)
}

func TestConcurrentUploadLimit(t *testing.T) {
	server, tempdir := setupTestServerWithLimits(t, 0, testMaxConcurrentLimit)
	defer os.RemoveAll(tempdir)

	// Fill the semaphore
	server.uploadSem <- struct{}{}
	defer func() { <-server.uploadSem }() // Ensure cleanup

	req := createTestRequest(expectedLongContent, targetSimplePath)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	assertHTTPError(t, err, http.StatusTooManyRequests)
}

func TestUploaderTraversal(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	req := createTestRequest(expectedLongContent, targetTraversalPath)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestUploaderDelete(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	// Create test file
	fpath := filepath.Join(tempdir, targetTestPath)
	fp, err := os.Create(fpath)
	require.NoError(t, err)
	require.NoError(t, fp.Close())

	// Create delete request
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", targetTestPath)
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodDelete, "/upload", body)
	req.Header.Set(testContentType, writer.FormDataContentType())

	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err = server.deleteFile(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	// Verify JSON response
	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["message"], targetTestPath)
	assert.Equal(t, targetTestPath, response["path"])

	// Verify file is deleted
	_, err = os.Stat(fpath)
	assert.True(t, os.IsNotExist(err))
}

func TestDeleteFilesOlderThanOneDay(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	// Create and age test file
	fpath := filepath.Join(tempdir, targetTestPath)
	fp, err := os.Create(fpath)
	require.NoError(t, err)
	require.NoError(t, fp.Close())

	// Set file timestamp to 25 hours ago
	timestamp := time.Now().Add(-25 * time.Hour)
	require.NoError(t, os.Chtimes(fpath, timestamp, timestamp))

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "1")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodDelete, "/delete", body)
	req.Header.Set(testContentType, writer.FormDataContentType())

	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err = server.deleteOldFiles(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	// Verify JSON response
	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["message"], "deleted")
	assert.Equal(t, float64(1), response["count"]) // JSON numbers are float64

	// Verify file is deleted
	_, err = os.Stat(fpath)
	assert.True(t, os.IsNotExist(err))
}

func TestDeleteFilesOlderThanTwoDay(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	// Create and age test file
	fpath := filepath.Join(tempdir, targetTestPath)
	fp, err := os.Create(fpath)
	require.NoError(t, err)
	require.NoError(t, fp.Close())

	// Set file timestamp to 49 hours ago (2+ days)
	timestamp := time.Now().Add(-49 * time.Hour)
	require.NoError(t, os.Chtimes(fpath, timestamp, timestamp))

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "2")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodDelete, "/delete", body)
	req.Header.Set(testContentType, writer.FormDataContentType())

	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err = server.deleteOldFiles(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	// Verify JSON response
	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["message"], "deleted")
	assert.Equal(t, float64(1), response["count"])

	// Verify file is deleted
	_, err = os.Stat(fpath)
	assert.True(t, os.IsNotExist(err))
}

func TestConfigLoadFromEnv(t *testing.T) {
	// Save and restore environment variables
	envVars := map[string]string{
		"UPLOADER_HOST":                   os.Getenv("UPLOADER_HOST"),
		"UPLOADER_PORT":                   os.Getenv("UPLOADER_PORT"),
		"UPLOADER_DIRECTORY":              os.Getenv("UPLOADER_DIRECTORY"),
		"UPLOADER_MAX_UPLOAD_SIZE":        os.Getenv("UPLOADER_MAX_UPLOAD_SIZE"),
		"UPLOADER_MAX_CONCURRENT_UPLOADS": os.Getenv("UPLOADER_MAX_CONCURRENT_UPLOADS"),
		"UPLOADER_REQUEST_TIMEOUT":        os.Getenv("UPLOADER_REQUEST_TIMEOUT"),
	}
	defer func() {
		for key, value := range envVars {
			os.Setenv(key, value)
		}
	}()

	// Set test values
	testEnvVars := map[string]string{
		"UPLOADER_HOST":                   testEnvHost,
		"UPLOADER_PORT":                   testEnvPort,
		"UPLOADER_DIRECTORY":              testEnvDir,
		"UPLOADER_MAX_UPLOAD_SIZE":        testEnvMaxSize,
		"UPLOADER_MAX_CONCURRENT_UPLOADS": testEnvMaxConcurrent,
		"UPLOADER_REQUEST_TIMEOUT":        testEnvTimeout,
	}
	for key, value := range testEnvVars {
		os.Setenv(key, value)
	}

	config := LoadConfig()

	assert.Equal(t, testEnvHost, config.Host)
	assert.Equal(t, testEnvPort, config.Port)
	assert.Equal(t, testEnvDir, config.Directory)
	assert.Equal(t, int64(1048576), config.MaxUploadSize)
	assert.Equal(t, 5, config.MaxConcurrentUploads)
	assert.Equal(t, 45*time.Second, config.RequestTimeout)
}

func TestConfigDefaults(t *testing.T) {
	config := LoadConfig()

	assert.Equal(t, testHost, config.Host)
	assert.Equal(t, testPort, config.Port)
	assert.Equal(t, "./pub", config.Directory)
	assert.Equal(t, int64(0), config.MaxUploadSize) // unlimited by default
	assert.Equal(t, 10, config.MaxConcurrentUploads)
	assert.Equal(t, testTimeout, config.RequestTimeout)
}

func TestPathSafety(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	testCases := []struct {
		name     string
		path     string
		expected bool
	}{
		{"safe file", filepath.Join(tempdir, "safe.txt"), true},
		{"safe subdirectory", filepath.Join(tempdir, "sub", "safe.txt"), true},
		{"system file", "/etc/passwd", false},
		{"traversal attempt", "../../../etc/passwd", false},
		{"outside directory", tempdir + "/../outside.txt", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := server.isPathSafe(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestLastModified(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	// Create a test file
	testFile := filepath.Join(tempdir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

	// Test HEAD request for existing file
	req, _ := http.NewRequest(http.MethodHead, "/test.txt", nil)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)
	context.SetParamNames("path")
	context.SetParamValues("test.txt")

	err := server.lastModified(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Last-Modified"))
	assert.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))
}

func TestLastModifiedNotFound(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	// Test HEAD request for non-existing file
	req, _ := http.NewRequest(http.MethodHead, "/nonexistent.txt", nil)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)
	context.SetParamNames("path")
	context.SetParamValues("nonexistent.txt")

	err := server.lastModified(context)
	assert.Error(t, err)
}

func TestLastModifiedPathTraversal(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	// Test HEAD request with path traversal
	req, _ := http.NewRequest(http.MethodHead, "/../../etc/passwd", nil)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)
	context.SetParamNames("path")
	context.SetParamValues("../../etc/passwd")

	err := server.lastModified(context)
	assertHTTPError(t, err, http.StatusForbidden)
}

func TestUploadTarGz(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	// Create test tar.gz data
	tarGzData := createTestTarGz(t)

	// Create multipart request with tar.gz
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", testTarGzName)
	_, _ = part.Write(tarGzData)
	_ = writer.WriteField("path", targetExtractedPath)
	_ = writer.WriteField("targz", "true")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set(testContentType, writer.FormDataContentType())

	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.upload(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	assertJSONResponse(t, rec, targetExtractedPath)

	// Verify extracted files
	extractedFile := filepath.Join(tempdir, targetExtractedPath, "test.txt")
	assert.FileExists(t, extractedFile)

	content, err := os.ReadFile(extractedFile)
	require.NoError(t, err)
	assert.Equal(t, expectedTestContent, string(content))
}

func TestSetupAuthWithCredentials(t *testing.T) {
	// Save and restore original env value
	original := os.Getenv("UPLOADER_UPLOAD_CREDENTIALS")
	defer os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", original)

	// Set test credentials
	os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", testCredentials)

	config := createTestConfig("/tmp")
	server := NewServer(config)

	// Verify server was set up correctly
	assert.NotNil(t, server)
	assert.NotNil(t, server.echo)
}

func TestSetupAuthSkipForGET(t *testing.T) {
	// Save and restore original env value
	original := os.Getenv("UPLOADER_UPLOAD_CREDENTIALS")
	defer os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", original)

	// Set test credentials
	os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", testCredentials)

	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	// Test GET request to health endpoint (should skip auth)
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.healthCheck(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSetupAuthInvalidCredentials(t *testing.T) {
	// Save and restore original env value
	original := os.Getenv("UPLOADER_UPLOAD_CREDENTIALS")
	defer os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", original)

	// Set invalid credentials (missing colon)
	os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", testInvalidCredentials)

	config := createTestConfig("/tmp")
	server := NewServer(config)

	// Server should still be created, but auth won't be properly configured
	assert.NotNil(t, server)
}

func TestDeleteOldFilesNoFiles(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "1")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodDelete, "/delete", body)
	req.Header.Set(testContentType, writer.FormDataContentType())

	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.deleteOldFiles(context)
	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	// Verify JSON response
	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["message"], "NO Old Files")
	assert.Equal(t, float64(0), response["count"])
}

func TestDeleteOldFilesInvalidRecursive(t *testing.T) {
	server, tempdir := setupTestServer(t)
	defer os.RemoveAll(tempdir)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "1")
	_ = writer.WriteField("recursive", "invalid")
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodDelete, "/delete", body)
	req.Header.Set(testContentType, writer.FormDataContentType())

	rec := httptest.NewRecorder()
	context := server.echo.NewContext(req, rec)

	err := server.deleteOldFiles(context)
	assertHTTPError(t, err, http.StatusBadRequest)
}
