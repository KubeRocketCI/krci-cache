package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func createTestTarGz(t *testing.T, files map[string]string) io.Reader {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0600,
			Size: int64(len(content)),
		}
		assert.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(content))
		assert.NoError(t, err)
	}

	assert.NoError(t, tw.Close())
	assert.NoError(t, gzw.Close())

	return &buf
}

func TestUntarGz_SingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	data := map[string]string{
		"hello.txt": "Hello from tar.gz!",
	}

	r := createTestTarGz(t, data)
	err := UntarGz(tmpDir, r)
	assert.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "hello.txt"))
	assert.NoError(t, err)
	assert.Equal(t, "Hello from tar.gz!", string(content))
}

func TestUntarGz_CreateDir(t *testing.T) {
	tmpDir := t.TempDir()
	data := map[string]string{
		"dir1/dir2/test.txt": "Nested content",
	}

	r := createTestTarGz(t, data)
	err := UntarGz(tmpDir, r)
	assert.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "dir1/dir2/test.txt"))
	assert.NoError(t, err)
	assert.Equal(t, "Nested content", string(content))
}

func TestUntarGz_InvalidGzip(t *testing.T) {
	tmpDir := t.TempDir()
	invalidData := []byte("not a gzip")
	err := UntarGz(tmpDir, bytes.NewReader(invalidData))
	assert.Error(t, err)
}

func TestExtractTar_BadTar(t *testing.T) {
	tmpDir := t.TempDir()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	_, _ = gzw.Write([]byte("not a tar"))
	_ = gzw.Close()

	err := UntarGz(tmpDir, &buf)
	assert.Error(t, err)
}

func TestExtractTar_HeaderNil(t *testing.T) {
	tmpDir := t.TempDir()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	_ = tw.Flush()
	_ = tw.Close()
	_ = gzw.Close()

	err := UntarGz(tmpDir, &buf)
	assert.NoError(t, err)
}

func TestHandleHeader_UnknownTypeflag(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	content := []byte("dummy")
	hdr := &tar.Header{
		Name:     "ignored.bin",
		Typeflag: 0x7F,
		Mode:     0600,
		Size:     int64(len(content)),
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gzw.Close()

	err := UntarGz(tmpDir, &buf)
	assert.NoError(t, err)
}

func TestWriteFile_ErrorOnMkdirAll(t *testing.T) {
	tmpDir := t.TempDir()

	conflictPath := filepath.Join(tmpDir, "conflict")
	err := os.WriteFile(conflictPath, []byte("not a dir"), 0644)
	assert.NoError(t, err)

	path := filepath.Join(conflictPath, "file.txt")
	err = writeFile(path, bytes.NewReader([]byte("data")), 0600)
	assert.Error(t, err)
}

func TestCreateDir_ErrorOnMkdirAll(t *testing.T) {
	tmpDir := t.TempDir()

	conflictPath := filepath.Join(tmpDir, "foo")
	err := os.WriteFile(conflictPath, []byte("not a dir"), 0644)
	assert.NoError(t, err)

	err = createDir(filepath.Join(conflictPath, "bar"))
	assert.Error(t, err)
}

type badReader struct{}

func (b *badReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func TestWriteFile_CopyFails(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "fail.txt")

	err := writeFile(path, &badReader{}, 0600)
	assert.Error(t, err)
}
