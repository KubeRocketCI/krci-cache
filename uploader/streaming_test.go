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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pins the early-rejection optimization: countingReader proves the handler
// returned 403 having read far less than the 8 MiB body. Without this guard
// a regression to "read body, then validate" would still pass a 403 check.
func TestUploadEarlyRejectionFieldsFirst(t *testing.T) {
	e, _ := concurrencyServer(t)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	require.NoError(t, w.WriteField("path", "../../escape.bin"))

	part, err := w.CreateFormFile("file", "evil.bin")
	require.NoError(t, err)

	const fileSize = 8 * 1024 * 1024

	_, err = part.Write(bytes.Repeat([]byte("X"), fileSize))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	counter := &countingReader{r: bytes.NewReader(body.Bytes())}

	req := httptest.NewRequest(http.MethodPost, "/upload", counter)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.ContentLength = int64(body.Len())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// 64 KiB covers multipart read-ahead buffering and is well below 8 MiB,
	// so early-reject vs full-spool is cleanly distinguished.
	const earlyRejectBudget = 64 * 1024
	assert.Lessf(t, counter.n, int64(earlyRejectBudget),
		"early rejection should leave most of the %d-byte body unread; consumed %d bytes",
		body.Len(), counter.n)
}

// Covers the stream-extract path (fields-first); TestExtractTarGz_* covers
// the file-first fallback.
func TestUploadStreamExtractFieldsFirst(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	arc := makeTarGz(t, "S", 5)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	require.NoError(t, w.WriteField("path", "stream-dir"))
	require.NoError(t, w.WriteField("targz", "true"))

	part, err := w.CreateFormFile("file", "stream.tar.gz")
	require.NoError(t, err)
	_, err = part.Write(arc)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equalf(t, http.StatusCreated, rec.Code, "upload failed: %s", rec.Body.String())

	entries, err := os.ReadDir(filepath.Join(tempdir, "stream-dir"))
	require.NoError(t, err)
	require.Len(t, entries, 5)

	for _, ent := range entries {
		require.Truef(t, strings.HasPrefix(ent.Name(), "S-"), "unexpected entry %s", ent.Name())
	}
}

func TestUploadRejectsOversizedField(t *testing.T) {
	e, _ := concurrencyServer(t)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	// path field just past the 1 MiB cap.
	require.NoError(t, w.WriteField("path", strings.Repeat("a", maxUploadFieldSize+1)))

	part, err := w.CreateFormFile("file", "x.bin")
	require.NoError(t, err)
	_, err = part.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "exceeds")
}

// Pins the contract: a second `file` part is rejected, not silently overwritten.
func TestUploadRejectsMultipleFileParts(t *testing.T) {
	e, _ := concurrencyServer(t)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	require.NoError(t, w.WriteField("path", "out.bin"))

	part1, err := w.CreateFormFile("file", "a.bin")
	require.NoError(t, err)
	_, err = part1.Write([]byte("aaa"))
	require.NoError(t, err)

	part2, err := w.CreateFormFile("file", "b.bin")
	require.NoError(t, err)
	_, err = part2.Write([]byte("bbb"))
	require.NoError(t, err)

	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadRejectsNonMultipart(t *testing.T) {
	e, _ := concurrencyServer(t)

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "application/octet-stream")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
