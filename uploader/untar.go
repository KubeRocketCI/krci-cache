package uploader

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Package uploader provides HTTP upload server functionality with support for file uploads and tar.gz extraction.

const (
	// MaxFileSize limits individual file size to prevent zip bombs (2GB)
	MaxFileSize = 2 * 1024 * 1024 * 1024
	// MaxTotalSize limits total extraction size to prevent disk exhaustion (8GB)
	MaxTotalSize = 8 * 1024 * 1024 * 1024
)

// UntarGz safely extracts a tar.gz archive to the destination directory
// with security protections against path traversal, symlink attacks, and resource exhaustion
func UntarGz(dst string, r io.Reader) error {
	absDst, gzr, err := setupExtraction(dst, r)
	if err != nil {
		return err
	}
	defer closeGzipReader(gzr)

	tr := tar.NewReader(gzr)

	// Track actual bytes written instead of header-declared sizes
	var totalWritten int64

	return extractArchive(tr, absDst, &totalWritten)
}

// setupExtraction prepares the destination and gzip reader
func setupExtraction(dst string, r io.Reader) (string, *gzip.Reader, error) {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get absolute path for destination: %w", err)
	}

	gzr, err := gzip.NewReader(r)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}

	return absDst, gzr, nil
}

// closeGzipReader safely closes the gzip reader
func closeGzipReader(gzr *gzip.Reader) {
	if closeErr := gzr.Close(); closeErr != nil {
		fmt.Printf("warning: failed to close gzip reader: %v\n", closeErr)
	}
}

// extractArchive processes the tar archive entries
func extractArchive(tr *tar.Reader, absDst string, totalWritten *int64) error {
	for {
		header, err := tr.Next()

		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return fmt.Errorf("failed to read tar header: %w", err)
		case header == nil:
			continue
		}

		// Pre-validate individual file size limit
		if header.Size > MaxFileSize {
			return fmt.Errorf("file %s exceeds maximum size limit (%d bytes)", header.Name, MaxFileSize)
		}

		target := filepath.Join(absDst, header.Name)
		if !isPathSafe(target, absDst) {
			return fmt.Errorf("unsafe path detected: %s", header.Name)
		}

		if err := processEntry(header, target, tr, totalWritten); err != nil {
			return err
		}
	}
}

// validateTotalSize checks if total written bytes exceed the limit
func validateTotalSize(totalWritten int64, additionalBytes int64) error {
	if totalWritten+additionalBytes > MaxTotalSize {
		return fmt.Errorf("archive exceeds maximum total size limit (%d bytes)", MaxTotalSize)
	}

	return nil
}

// processEntry handles different tar entry types
func processEntry(header *tar.Header, target string, tr *tar.Reader, totalWritten *int64) error {
	switch header.Typeflag {
	case tar.TypeDir:
		if err := handleDirectory(target, header); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", header.Name, err)
		}

	case tar.TypeReg:
		written, err := handleRegularFile(target, header, tr, *totalWritten)
		if err != nil {
			return fmt.Errorf("failed to extract file %s: %w", header.Name, err)
		}

		*totalWritten += written

	case tar.TypeSymlink, tar.TypeLink:
		return fmt.Errorf("symlinks and hard links are not allowed: %s", header.Name)

	default:
		fmt.Printf("warning: skipping unsupported file type %d for %s\n", header.Typeflag, header.Name)
	}

	return nil
}

// isPathSafe checks if the target path is within the destination directory
func isPathSafe(target, dst string) bool {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	// Ensure the target is within the destination directory
	return strings.HasPrefix(absTarget, dst+string(filepath.Separator)) || absTarget == dst
}

// handleDirectory creates a directory with proper permissions
func handleDirectory(target string, header *tar.Header) error {
	// Check if directory already exists
	if info, err := os.Stat(target); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("path exists but is not a directory: %s", target)
		}

		return nil // Directory already exists
	}

	// Create directory with permissions from tar header, but ensure minimum safety
	mode := os.FileMode(header.Mode) & os.ModePerm
	if mode == 0 {
		mode = 0755 // Default safe permissions
	}

	return os.MkdirAll(target, mode)
}

// handleRegularFile extracts a regular file with proper resource management
// Returns the actual number of bytes written
func handleRegularFile(target string, header *tar.Header, tr *tar.Reader, currentTotal int64) (int64, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return 0, fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Create file with permissions from tar header
	mode := os.FileMode(header.Mode) & os.ModePerm
	if mode == 0 {
		mode = 0644 // Default safe permissions for files
	}

	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}

	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Printf("warning: failed to close file %s: %v\n", target, closeErr)
		}
	}()

	// Use a tracking writer to monitor actual bytes written
	trackingWriter := &trackingWriter{
		writer:       f,
		currentTotal: currentTotal,
	}

	// Copy directly from tar reader with streaming size validation
	written, err := io.Copy(trackingWriter, tr)
	if err != nil {
		// Clean up partial file on error
		_ = os.Remove(target)
		return 0, fmt.Errorf("failed to write file content: %w", err)
	}

	// Verify file size is reasonable (allow some flexibility for compression)
	if written > header.Size*2 { // Allow up to 2x header size for safety
		_ = os.Remove(target)
		return 0, fmt.Errorf("file %s wrote more bytes (%d) than reasonable limit based on header (%d)", header.Name, written, header.Size)
	}

	return written, nil
}

// trackingWriter wraps an io.Writer to track total bytes written and enforce limits
type trackingWriter struct {
	writer       io.Writer
	currentTotal int64
	written      int64
}

func (tw *trackingWriter) Write(p []byte) (int, error) {
	// Check if writing this chunk would exceed total size limit
	if err := validateTotalSize(tw.currentTotal+tw.written, int64(len(p))); err != nil {
		return 0, err
	}

	n, err := tw.writer.Write(p)
	tw.written += int64(n)

	return n, err
}
