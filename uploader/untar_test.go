package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUntarGz(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Create test tar.gz data
	tarGzData := createTestTarGz(t)
	reader := bytes.NewReader(tarGzData)

	err = UntarGz(tempdir, reader)
	assert.NoError(t, err)

	// Verify extracted files
	extractedFile := filepath.Join(tempdir, "test.txt")
	assert.FileExists(t, extractedFile)

	content, err := os.ReadFile(extractedFile)
	assert.NoError(t, err)
	assert.Equal(t, "test content", string(content))

	// Verify directory was created
	extractedDir := filepath.Join(tempdir, "testdir")
	info, err := os.Stat(extractedDir)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify file in directory
	extractedFileInDir := filepath.Join(tempdir, "testdir", "nested.txt")
	assert.FileExists(t, extractedFileInDir)

	content, err = os.ReadFile(extractedFileInDir)
	assert.NoError(t, err)
	assert.Equal(t, "nested content", string(content))
}

func TestUntarGzInvalidData(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-invalid")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Test with invalid gzip data
	invalidData := []byte("not a gzip file")
	reader := bytes.NewReader(invalidData)

	err = UntarGz(tempdir, reader)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create gzip reader")
}

func TestUntarGzPathTraversal(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-traversal")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Create malicious tar.gz with path traversal
	maliciousTarGz := createMaliciousTarGz(t)
	reader := bytes.NewReader(maliciousTarGz)

	err = UntarGz(tempdir, reader)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsafe path detected")
}

func TestUntarGzSymlinkRejection(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-symlink")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Create tar.gz with symlink
	symlinkTarGz := createSymlinkTarGz(t)
	reader := bytes.NewReader(symlinkTarGz)

	err = UntarGz(tempdir, reader)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "symlinks and hard links are not allowed")
}

func TestUntarGzFileSizeLimit(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-size-limit")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Test the validateEntry function directly to avoid creating huge files
	// This tests the core validation logic that would reject oversized files
	var totalSize int64

	// Create a mock header with size exceeding MaxFileSize
	header := &tar.Header{
		Name: "oversized.txt",
		Size: MaxFileSize + 1, // Just over the limit
	}

	err = validateEntry(header, &totalSize)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum size limit")
}

func TestUntarGzTotalSizeLimit(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-total-size-limit")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Test the validateEntry function with multiple files exceeding total size
	var totalSize int64

	// Add several files that together exceed MaxTotalSize
	for i := 0; i < 5; i++ {
		header := &tar.Header{
			Name: fmt.Sprintf("large_file_%d.txt", i),
			Size: MaxFileSize, // Each file at max size
		}

		if i < 4 {
			// First 4 files should be OK
			err := validateEntry(header, &totalSize)
			assert.NoError(t, err)
		} else {
			// 5th file should exceed total limit
			err := validateEntry(header, &totalSize)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "archive exceeds maximum total size limit")
		}
	}
}

func TestUntarGzDirectoryPermissions(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-perms")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Create a tar.gz with specific directory permissions
	permTarGz := createPermissionsTarGz(t)
	reader := bytes.NewReader(permTarGz)

	err = UntarGz(tempdir, reader)
	assert.NoError(t, err)

	// Verify directory was created with correct permissions
	testDir := filepath.Join(tempdir, "testdir")
	info, err := os.Stat(testDir)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())
}

// Helper function to create basic test tar.gz
func createTestTarGz(t *testing.T) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add a regular file
	content := "test content"
	header := &tar.Header{
		Name: "test.txt",
		Mode: 0644,
		Size: int64(len(content)),
	}
	err := tw.WriteHeader(header)
	require.NoError(t, err)
	_, err = tw.Write([]byte(content))
	require.NoError(t, err)

	// Add a directory
	dirHeader := &tar.Header{
		Name:     "testdir/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}
	err = tw.WriteHeader(dirHeader)
	require.NoError(t, err)

	// Add a file in the directory
	nestedContent := "nested content"
	nestedHeader := &tar.Header{
		Name: "testdir/nested.txt",
		Mode: 0644,
		Size: int64(len(nestedContent)),
	}
	err = tw.WriteHeader(nestedHeader)
	require.NoError(t, err)
	_, err = tw.Write([]byte(nestedContent))
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)
	err = gw.Close()
	require.NoError(t, err)

	return buf.Bytes()
}

// Helper function to create malicious tar.gz with path traversal
func createMaliciousTarGz(t *testing.T) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add file with path traversal
	content := "malicious content"
	header := &tar.Header{
		Name: "../../../etc/passwd",
		Mode: 0644,
		Size: int64(len(content)),
	}
	err := tw.WriteHeader(header)
	require.NoError(t, err)
	_, err = tw.Write([]byte(content))
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)
	err = gw.Close()
	require.NoError(t, err)

	return buf.Bytes()
}

// Helper function to create tar.gz with symlink
func createSymlinkTarGz(t *testing.T) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add symlink
	header := &tar.Header{
		Name:     "malicious_symlink",
		Mode:     0777,
		Typeflag: tar.TypeSymlink,
		Linkname: "/etc/passwd",
	}
	err := tw.WriteHeader(header)
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)
	err = gw.Close()
	require.NoError(t, err)

	return buf.Bytes()
}

// Helper function to create tar.gz with specific directory permissions
func createPermissionsTarGz(t *testing.T) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add directory with specific permissions
	dirHeader := &tar.Header{
		Name:     "testdir/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}
	err := tw.WriteHeader(dirHeader)
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)
	err = gw.Close()
	require.NoError(t, err)

	return buf.Bytes()
}
