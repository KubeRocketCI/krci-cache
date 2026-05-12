package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Builds the same route table as Uploader() so tests exercise production dispatch.
func concurrencyServer(t *testing.T) (*echo.Echo, string) {
	t.Helper()

	tempdir := withStagingDir(t)
	require.NoError(t, setupStagingDir())

	e := echo.New()
	e.HideBanner = true
	registerRoutes(e, http.FileServer(hideStagingFS{root: http.Dir(tempdir)}))

	return e, tempdir
}

func buildUploadRequest(t *testing.T, path string, content []byte, tarFlag string) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	part, err := w.CreateFormFile("file", filepath.Base(path))
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)

	require.NoError(t, w.WriteField("path", path))

	if tarFlag != "" {
		require.NoError(t, w.WriteField("targz", tarFlag))
	}

	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	return req
}

// Production clients (curl -F) send DELETE bodies as multipart; net/http
// silently ignores urlencoded bodies on DELETE.
func buildDeleteRequest(t *testing.T, url string, fields map[string]string) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	for k, v := range fields {
		require.NoError(t, w.WriteField(k, v))
	}

	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodDelete, url, body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	return req
}

func TestConcurrentWritersSameFile(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	const writers = 16

	// 256 KiB is large enough that direct os.Create would interleave Writes.
	const size = 256 * 1024

	// Single-byte-repeat per writer makes torn writes trivially detectable.
	contents := make([][]byte, writers)
	for i := range contents {
		b := make([]byte, size)
		for j := range b {
			b[j] = byte('A' + i)
		}

		contents[i] = b
	}

	var wg sync.WaitGroup

	wg.Add(writers)

	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()

			req := buildUploadRequest(t, "race.bin", contents[i], "")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusCreated, rec.Code, "writer %d failed: %s", i, rec.Body.String())
		}(i)
	}

	wg.Wait()

	got, err := os.ReadFile(filepath.Join(tempdir, "race.bin"))
	require.NoError(t, err)
	require.Len(t, got, size, "final file must be exactly one writer's content size")

	// Every byte must equal the first byte: confirms no interleaving.
	first := got[0]
	for i, b := range got {
		require.Equalf(t, first, b, "byte %d differs from byte 0 (0x%02x vs 0x%02x) — torn write!", i, b, first)
	}

	// And the chosen content must match exactly one of the writers' inputs.
	chosen := -1

	for i, c := range contents {
		if bytes.Equal(c, got) {
			chosen = i
			break
		}
	}

	require.NotEqualf(t, -1, chosen, "final file matches no writer's input — should be impossible")
	t.Logf("winner: writer %d", chosen)
}

func TestReaderDuringWriter(t *testing.T) {
	e, _ := concurrencyServer(t)

	old := bytes.Repeat([]byte("O"), 4096)
	newer := bytes.Repeat([]byte("N"), 4096)
	oldHash := sha256.Sum256(old)
	newHash := sha256.Sum256(newer)

	// Seed an "old" version first.
	{
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, buildUploadRequest(t, "live.bin", old, ""))
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	// Slow-upload the new version.
	var (
		writerDone sync.WaitGroup
		reads      atomic.Int64
		torn       atomic.Int64
	)

	writerDone.Add(1)

	go func() {
		defer writerDone.Done()

		body := &bytes.Buffer{}
		w := multipart.NewWriter(body)
		part, err := w.CreateFormFile("file", "live.bin")
		require.NoError(t, err)

		// Write into the multipart body slowly so the publish takes
		// measurable wall-clock time — that gives the reader goroutine
		// plenty of overlapping requests to expose torn reads.
		for _, c := range newer {
			_, _ = part.Write([]byte{c})

			time.Sleep(20 * time.Microsecond)
		}

		require.NoError(t, w.WriteField("path", "live.bin"))
		require.NoError(t, w.Close())

		req := httptest.NewRequest(http.MethodPost, "/upload", body)
		req.Header.Set("Content-Type", w.FormDataContentType())

		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}()

	// Spam GETs while the writer runs. Every 200 OK must hash to either
	// oldHash or newHash; anything else means the reader saw a partial
	// publish (the invariant we're protecting).
	stop := make(chan struct{})

	go func() {
		writerDone.Wait()
		// Let a few more reads happen after publish lands.
		time.Sleep(5 * time.Millisecond)
		close(stop)
	}()

	for {
		select {
		case <-stop:
			t.Logf("reads=%d torn=%d", reads.Load(), torn.Load())
			require.Zero(t, torn.Load(), "reader observed partial content")

			return
		default:
		}

		req := httptest.NewRequest(http.MethodGet, "/live.bin", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			continue
		}

		reads.Add(1)

		got := sha256.Sum256(rec.Body.Bytes())
		if got != oldHash && got != newHash {
			torn.Add(1)
			t.Errorf("torn read: hash=%s len=%d", hex.EncodeToString(got[:]), rec.Body.Len())
		}
	}
}

// Entries all start with `tag` so concurrent-extract tests can identify the winner.
func makeTarGz(t *testing.T, tag string, fileCount int) []byte {
	t.Helper()

	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for i := 0; i < fileCount; i++ {
		content := []byte(fmt.Sprintf("%s/file-%d-content", tag, i))
		hdr := &tar.Header{
			Name:     fmt.Sprintf("%s-%d.txt", tag, i),
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

func TestConcurrentTarExtractSameDir(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	const filesPerArchive = 30

	arcA := makeTarGz(t, "A", filesPerArchive)
	arcB := makeTarGz(t, "B", filesPerArchive)

	var wg sync.WaitGroup

	wg.Add(2)

	for _, arc := range [][]byte{arcA, arcB} {
		go func(content []byte) {
			defer wg.Done()

			req := buildUploadRequest(t, "release", content, "true")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		}(arc)
	}

	wg.Wait()

	entries, err := os.ReadDir(filepath.Join(tempdir, "release"))
	require.NoError(t, err)
	require.Len(t, entries, filesPerArchive, "directory must hold exactly one archive's entries")

	// Either every entry is from A or every entry is from B. Mixed = bug.
	prefix := strings.SplitN(entries[0].Name(), "-", 2)[0]
	require.Contains(t, []string{"A", "B"}, prefix)

	for _, e := range entries {
		require.Truef(t, strings.HasPrefix(e.Name(), prefix+"-"),
			"entry %s does not match winner prefix %s — interleaved extract!", e.Name(), prefix)
	}

	t.Logf("winner: archive %s", prefix)
}

func TestStagingDirHiddenFromStaticServe(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	stage := filepath.Join(tempdir, stagingDir)
	require.NoError(t, os.MkdirAll(stage, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stage, "secret.txt"), []byte("nope"), 0o644))

	// GET .tmp/secret.txt
	{
		req := httptest.NewRequest(http.MethodGet, "/.tmp/secret.txt", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	}

	// HEAD on a single-segment .tmp also rejected.
	{
		req := httptest.NewRequest(http.MethodHead, "/.tmp", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code, "HEAD on .tmp must be rejected by safeJoin")
	}
}

// Without this guard, a client could DELETE /upload path=.tmp/up-XXXX to
// disrupt another client's in-flight upload.
func TestStagingDirRejectedFromMutatingHandlers(t *testing.T) {
	e, _ := concurrencyServer(t)

	// POST /upload with path inside .tmp
	{
		req := buildUploadRequest(t, ".tmp/evil.txt", []byte("x"), "")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	}

	// DELETE /upload with path inside .tmp (multipart, matching production)
	{
		req := buildDeleteRequest(t, "/upload",
			map[string]string{"path": ".tmp/anything"})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

func TestDeleteOldFilesRecursiveDir(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	// Make a non-empty directory whose mtime is well in the past.
	stale := filepath.Join(tempdir, "stale-dir")
	require.NoError(t, os.MkdirAll(stale, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "inside.txt"), []byte("x"), 0o644))

	old := time.Now().Add(-30 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	req := buildDeleteRequest(t, "/delete", map[string]string{
		"path":      "",
		"days":      "1",
		"recursive": "true",
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	_, err := os.Stat(stale)
	require.Truef(t, os.IsNotExist(err), "stale dir should be removed, got err=%v", err)
}

func TestDeleteOldFilesSkipsStagingDir(t *testing.T) {
	e, tempdir := concurrencyServer(t)

	stage := filepath.Join(tempdir, stagingDir)
	require.NoError(t, os.MkdirAll(stage, 0o755))

	// Make staging look "old".
	old := time.Now().Add(-365 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(stage, old, old))

	req := buildDeleteRequest(t, "/delete", map[string]string{
		"path":      "",
		"days":      "0",
		"recursive": "true",
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	_, err := os.Stat(stage)
	require.NoError(t, err, "staging dir must survive the sweep")
}
