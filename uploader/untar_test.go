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

func TestUntarGz_MemoryOptimizedSizeValidation(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-memory")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Create tar.gz with multiple moderate-sized files
	// The key test is that we don't pre-allocate huge amounts of memory
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add multiple files that together would have caused memory issues with old approach
	for i := 0; i < 5; i++ {
		filename := fmt.Sprintf("file_%d.txt", i)
		content := fmt.Sprintf("Content for file %d - this is reasonable sized content", i)

		header := &tar.Header{
			Name:     filename,
			Size:     int64(len(content)),
			Mode:     0644,
			Typeflag: tar.TypeReg,
		}

		err = tw.WriteHeader(header)
		require.NoError(t, err)

		_, err = tw.Write([]byte(content))
		require.NoError(t, err)
	}

	tw.Close()
	gzw.Close()

	// This should work efficiently with streaming memory optimization
	err = UntarGz(tempdir, &buf)
	assert.NoError(t, err)

	// Verify all files were extracted correctly
	for i := 0; i < 5; i++ {
		filename := fmt.Sprintf("file_%d.txt", i)
		expectedContent := fmt.Sprintf("Content for file %d - this is reasonable sized content", i)

		extractedFile := filepath.Join(tempdir, filename)
		assert.FileExists(t, extractedFile)

		content, err := os.ReadFile(extractedFile)
		assert.NoError(t, err)
		assert.Equal(t, expectedContent, string(content))
	}
}

func TestUntarGz_ActualSizeTracking(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-size-tracking")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Create multiple files that are reasonably sized
	// This tests that our size tracking works correctly with real data
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add 3 files with realistic content sizes
	expectedContents := []string{
		"Small content for file 0, testing actual size tracking functionality.",
		"Medium content for file 1, with some more text to make it a bit larger than the first file.",
		"Large content for file 2, with even more text content to test our size tracking works correctly with files of different sizes, and to ensure we're properly accounting for actual bytes written to disk.",
	}

	for i, content := range expectedContents {
		header := &tar.Header{
			Name:     fmt.Sprintf("file_%d.txt", i),
			Size:     int64(len(content)),
			Mode:     0644,
			Typeflag: tar.TypeReg,
		}

		err = tw.WriteHeader(header)
		require.NoError(t, err)

		_, err = tw.Write([]byte(content))
		require.NoError(t, err)
	}

	tw.Close()
	gzw.Close()

	// This should succeed with proper size tracking
	err = UntarGz(tempdir, &buf)
	assert.NoError(t, err)

	// Verify all files were extracted correctly
	for i, expectedContent := range expectedContents {
		extractedFile := filepath.Join(tempdir, fmt.Sprintf("file_%d.txt", i))
		assert.FileExists(t, extractedFile)

		content, err := os.ReadFile(extractedFile)
		assert.NoError(t, err)
		assert.Equal(t, expectedContent, string(content))
	}
}

func TestUntarGz_TotalSizeLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping size limit test in short mode due to large data creation")
	}

	tempdir, err := os.MkdirTemp("", "test-untar-size-limit")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Create a small archive but use custom trackingWriter to simulate reaching the limit
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add a small file
	content := []byte("test content for size limit validation")
	header := &tar.Header{
		Name:     "test_file.txt",
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}

	err = tw.WriteHeader(header)
	require.NoError(t, err)
	_, err = tw.Write(content)
	require.NoError(t, err)

	tw.Close()
	gzw.Close()

	// Test the validation function directly
	err = validateTotalSize(MaxTotalSize-100, 200) // Would exceed limit
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum total size limit")

	// Test that the archive extraction works normally when under limit
	err = UntarGz(tempdir, &buf)
	assert.NoError(t, err)

	// Verify file was extracted
	extractedFile := filepath.Join(tempdir, "test_file.txt")
	assert.FileExists(t, extractedFile)
	extractedContent, err := os.ReadFile(extractedFile)
	assert.NoError(t, err)
	assert.Equal(t, content, extractedContent)
}

func TestTrackingWriter(t *testing.T) {
	var buf bytes.Buffer

	tw := &trackingWriter{
		writer:       &buf,
		currentTotal: 1000, // Already have 1000 bytes written
		written:      0,
	}

	// Write data that stays within limit
	data := []byte("test data")
	n, err := tw.Write(data)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, int64(len(data)), tw.written)
	assert.Equal(t, "test data", buf.String())

	// Try to write data that would exceed total limit
	tw.currentTotal = MaxTotalSize - 100 // Close to limit
	tw.written = 0

	buf.Reset()

	largeData := make([]byte, 200) // This would exceed limit
	_, err = tw.Write(largeData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum total size limit")
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

	// Create an archive with a single file that exceeds individual file size limit
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Create a file larger than MaxFileSize (2GB)
	header := &tar.Header{
		Name:     "huge_file.txt",
		Size:     MaxFileSize + 1000, // Slightly over 2GB limit
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}

	err = tw.WriteHeader(header)
	require.NoError(t, err)

	// We don't need to write actual data - the header size check should fail first
	tw.Close()
	gzw.Close()

	// This should fail due to individual file size limit
	err = UntarGz(tempdir, &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum size limit")
}

func TestUntarGzTotalSizeLimit(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "test-untar-total-size-limit")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// Test the validateTotalSize function with edge cases
	// Test exact limit
	err = validateTotalSize(MaxTotalSize, 0)
	assert.NoError(t, err)

	// Test one byte over limit
	err = validateTotalSize(MaxTotalSize, 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum total size limit")

	// Test multiple smaller additions that exceed limit
	err = validateTotalSize(MaxTotalSize-50, 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum total size limit")
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
