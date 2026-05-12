// To compare against a baseline commit, copy this file into a worktree of
// the baseline and run the benchmark on both:
//
//	git worktree add /tmp/baseline <rev>
//	cp uploader/upload_bench_test.go /tmp/baseline/uploader/
//	go test -C /tmp/baseline -run='^$' -bench=. -benchtime=5x ./uploader/
//	go test -run='^$' -bench=. -benchtime=5x ./uploader/
//
// On macOS APFS the streaming/spool gap is small (~1.3x) because APFS CoW
// makes the baseline's second copy nearly free. On Linux ext4/xfs (production)
// copy_file_range does a real byte copy unless reflink is enabled, so the
// streaming win is ~1.5-2x — local Mac numbers are conservative.
package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/labstack/echo/v4"
)

func benchEnvStandalone(b *testing.B) *echo.Echo {
	b.Helper()

	// Restore TMPDIR before each cleanup: we point it at the bench's
	// .tmp below, and a stale value after the dir is removed makes the
	// next benchmark's MkdirTemp fail with ENOENT.
	originalTMPDIR, hadTMPDIR := os.LookupEnv("TMPDIR")

	tempdir, err := os.MkdirTemp("", "uploader-bench-*")
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() {
		if hadTMPDIR {
			_ = os.Setenv("TMPDIR", originalTMPDIR)
		} else {
			_ = os.Unsetenv("TMPDIR")
		}

		_ = os.RemoveAll(tempdir)
	})

	originalDir := directory

	if err := setUploadDirectory(tempdir); err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = setUploadDirectory(originalDir) })

	if err := setupStagingDir(); err != nil {
		b.Fatal(err)
	}

	// Match production: Uploader() sets TMPDIR=absStagePath so the multipart
	// spool lands on the same FS as the staging dir (enables copy_file_range).
	// The streaming branch ignores this, but the baseline needs it for a fair bench.
	_ = os.Setenv("TMPDIR", absStagePath)

	e := echo.New()
	e.HideBanner = true
	registerRoutes(e, http.FileServer(hideStagingFS{root: http.Dir(tempdir)}))

	return e
}

func BenchmarkUpload100MBRegularFile(b *testing.B) {
	const size = 100 * 1024 * 1024

	e := benchEnvStandalone(b)

	content := bytes.Repeat([]byte{'x'}, size)

	b.SetBytes(int64(size))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		body := &bytes.Buffer{}
		w := multipart.NewWriter(body)

		if err := w.WriteField("path", fmt.Sprintf("bench-%d.bin", i)); err != nil {
			b.Fatal(err)
		}

		part, err := w.CreateFormFile("file", "bench.bin")
		if err != nil {
			b.Fatal(err)
		}

		if _, err := part.Write(content); err != nil {
			b.Fatal(err)
		}

		if err := w.Close(); err != nil {
			b.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/upload", body)
		req.Header.Set("Content-Type", w.FormDataContentType())

		b.StartTimer()

		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		b.StopTimer()

		if rec.Code != http.StatusCreated {
			b.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
		}
	}
}

func BenchmarkUpload10MBTarExtract(b *testing.B) {
	e := benchEnvStandalone(b)

	const entries = 100

	const entrySize = 100 * 1024

	arc := makeArchiveStandalone(b, entries, entrySize)

	b.SetBytes(int64(len(arc)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		body := &bytes.Buffer{}
		w := multipart.NewWriter(body)

		if err := w.WriteField("path", fmt.Sprintf("bench-tar-%d", i)); err != nil {
			b.Fatal(err)
		}

		if err := w.WriteField("targz", "true"); err != nil {
			b.Fatal(err)
		}

		part, err := w.CreateFormFile("file", "bench.tar.gz")
		if err != nil {
			b.Fatal(err)
		}

		if _, err := part.Write(arc); err != nil {
			b.Fatal(err)
		}

		if err := w.Close(); err != nil {
			b.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/upload", body)
		req.Header.Set("Content-Type", w.FormDataContentType())

		b.StartTimer()

		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		b.StopTimer()

		if rec.Code != http.StatusCreated {
			b.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
		}
	}
}

func makeArchiveStandalone(b *testing.B, entries, entrySize int) []byte {
	b.Helper()

	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	payload := make([]byte, entrySize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	for i := 0; i < entries; i++ {
		hdr := &tar.Header{
			Name:     fmt.Sprintf("entry-%05d.bin", i),
			Mode:     0o644,
			Size:     int64(entrySize),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			b.Fatal(err)
		}

		if _, err := tw.Write(payload); err != nil {
			b.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		b.Fatal(err)
	}

	if err := gw.Close(); err != nil {
		b.Fatal(err)
	}

	return buf.Bytes()
}
