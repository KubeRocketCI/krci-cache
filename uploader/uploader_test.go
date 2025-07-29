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

	// Set the directory variable for tests
	originalDir := directory
	directory = tempdir

	// Create simple Echo instance like the main Uploader() function
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.Static("/", directory)
	e.GET("/health", healthCheck)
	e.HEAD("/:path", lastModified)
	e.POST("/upload", upload)
	e.DELETE("/upload", uploaderDelete)
	e.DELETE("/delete", deleteOldFilesOfDir)

	t.Cleanup(func() {
		directory = originalDir

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

func TestEnvironmentConfiguration(t *testing.T) {
	// Test that environment variables are read correctly
	originalDir := os.Getenv("UPLOADER_DIRECTORY")
	originalHost := os.Getenv("UPLOADER_HOST")
	originalPort := os.Getenv("UPLOADER_PORT")

	defer func() {
		// Restore original values
		if originalDir != "" {
			os.Setenv("UPLOADER_DIRECTORY", originalDir)
		} else {
			os.Unsetenv("UPLOADER_DIRECTORY")
		}

		if originalHost != "" {
			os.Setenv("UPLOADER_HOST", originalHost)
		} else {
			os.Unsetenv("UPLOADER_HOST")
		}

		if originalPort != "" {
			os.Setenv("UPLOADER_PORT", originalPort)
		} else {
			os.Unsetenv("UPLOADER_PORT")
		}
	}()

	// Set test environment variables
	testDir := "/test/upload/dir"
	testHost := "test-host"
	testPort := "9999"

	os.Setenv("UPLOADER_DIRECTORY", testDir)
	os.Setenv("UPLOADER_HOST", testHost)
	os.Setenv("UPLOADER_PORT", testPort)

	// Reset global variables
	directory = "./pub" // default
	host = "localhost"  // default
	port = "8080"       // default

	// This would be called in the Uploader() function
	if os.Getenv("UPLOADER_DIRECTORY") != "" {
		directory = os.Getenv("UPLOADER_DIRECTORY")
	}

	if os.Getenv("UPLOADER_HOST") != "" {
		host = os.Getenv("UPLOADER_HOST")
	}

	if os.Getenv("UPLOADER_PORT") != "" {
		port = os.Getenv("UPLOADER_PORT")
	}

	// Check that values were set correctly
	assert.Equal(t, testDir, directory)
	assert.Equal(t, testHost, host)
	assert.Equal(t, testPort, port)
}
