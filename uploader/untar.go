package uploader

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
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

	// Cache of parent dirs already ensured during this extraction. Bounded
	// by archive contents (~50B * unique-dir-count) and freed on return.
	// Eliminates the per-entry MkdirAll fan-out that costs ~1ms/call on NFS.
	ensuredDirs := make(map[string]struct{})

	return extractArchive(tr, absDst, &totalWritten, ensuredDirs)
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
		log.Printf("warning: failed to close gzip reader: %v", closeErr)
	}
}

// extractArchive processes the tar archive entries
func extractArchive(tr *tar.Reader, absDst string, totalWritten *int64, ensuredDirs map[string]struct{}) error {
	// Pre-seed: absDst is created by the caller of UntarGz before extraction,
	// so files whose parent is the root skip a redundant MkdirAll.
	ensuredDirs[absDst] = struct{}{}

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

		if header.Size > MaxFileSize {
			return fmt.Errorf("file %s exceeds maximum size limit (%d bytes)", header.Name, MaxFileSize)
		}

		target := filepath.Join(absDst, header.Name)
		if !isPathSafe(target, absDst) {
			return fmt.Errorf("unsafe path detected: %s", header.Name)
		}

		if err := processEntry(header, target, tr, totalWritten, ensuredDirs); err != nil {
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
func processEntry(header *tar.Header, target string, tr *tar.Reader, totalWritten *int64, ensuredDirs map[string]struct{}) error {
	switch header.Typeflag {
	case tar.TypeDir:
		if err := handleDirectory(target, header); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", header.Name, err)
		}

		ensuredDirs[target] = struct{}{}

	case tar.TypeReg:
		written, err := handleRegularFile(target, header, tr, *totalWritten, ensuredDirs)
		if err != nil {
			return fmt.Errorf("failed to extract file %s: %w", header.Name, err)
		}

		*totalWritten += written

	case tar.TypeSymlink, tar.TypeLink:
		return fmt.Errorf("symlinks and hard links are not allowed: %s", header.Name)

	default:
		log.Printf("warning: skipping unsupported file type %d for %s", header.Typeflag, header.Name)
	}

	return nil
}

// isPathSafe reports whether absTarget equals absRoot or is a strict descendant.
// Both arguments must already be absolute; the trailing separator in the prefix
// check rejects sibling-prefix escapes (e.g. "/data" vs "/data-evil").
func isPathSafe(absTarget, absRoot string) bool {
	return absTarget == absRoot || strings.HasPrefix(absTarget, absRoot+string(filepath.Separator))
}

// handleDirectory creates a directory with proper permissions.
// MkdirAll is idempotent on an existing directory and surfaces a clear
// ENOTDIR if the path exists as a file — no separate Stat needed.
func handleDirectory(target string, header *tar.Header) error {
	mode := os.FileMode(header.Mode) & os.ModePerm
	if mode == 0 {
		mode = 0755
	}

	return os.MkdirAll(target, mode)
}

// handleRegularFile extracts a regular file with proper resource management
// Returns the actual number of bytes written
func handleRegularFile(target string, header *tar.Header, tr *tar.Reader, currentTotal int64, ensuredDirs map[string]struct{}) (int64, error) {
	parent := filepath.Dir(target)
	if _, ok := ensuredDirs[parent]; !ok {
		if err := os.MkdirAll(parent, 0755); err != nil {
			return 0, fmt.Errorf("failed to create parent directory: %w", err)
		}

		ensuredDirs[parent] = struct{}{}
	}

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
			log.Printf("warning: failed to close file %s: %v", target, closeErr)
		}
	}()

	trackingWriter := &trackingWriter{
		writer:       f,
		currentTotal: currentTotal,
	}

	written, err := io.Copy(trackingWriter, tr)
	if err != nil {
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
	if err := validateTotalSize(tw.currentTotal+tw.written, int64(len(p))); err != nil {
		return 0, err
	}

	n, err := tw.writer.Write(p)
	tw.written += int64(n)

	return n, err
}
