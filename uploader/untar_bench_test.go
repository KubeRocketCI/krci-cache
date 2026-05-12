// Tar-extraction microbenchmarks for the dir-cache change in extractArchive.
// Per-entry savings come from skipping a redundant MkdirAll on a parent dir
// already ensured by a prior entry, so the absolute win scales with the
// number of regular-file entries that share a parent.
//
// To compare against a baseline commit:
//
//	git worktree add /tmp/baseline <rev>
//	cp uploader/untar_bench_test.go /tmp/baseline/uploader/
//	go test -C /tmp/baseline -run='^$' -bench=BenchmarkUntarGz -benchtime=3x ./uploader/
//	go test -run='^$' -bench=BenchmarkUntarGz -benchtime=3x ./uploader/
//
// On Linux ext4/xfs the win per skipped MkdirAll is ~3-5us; on NFS it's ~1ms.
package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"testing"
)

// Single-dir archive: all entries share one parent, so the dir cache turns
// N-1 redundant MkdirAll calls into N-1 map hits.
func BenchmarkUntarGzManyFilesSingleDir(b *testing.B) {
	for _, entries := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("entries=%d", entries), func(b *testing.B) {
			arc := buildSingleDirArchive(b, entries)
			runUntarBench(b, arc)
		})
	}
}

// Wide archive: each entry has its own parent dir, so the cache only saves
// the per-entry parent lookup against the implicit-root entry — minimal win.
// Establishes the lower bound and guards against regression on this shape.
func BenchmarkUntarGzManyFilesUniqueDirs(b *testing.B) {
	for _, entries := range []int{100, 1000} {
		b.Run(fmt.Sprintf("entries=%d", entries), func(b *testing.B) {
			arc := buildUniqueDirArchive(b, entries)
			runUntarBench(b, arc)
		})
	}
}

func runUntarBench(b *testing.B, arc []byte) {
	b.Helper()

	b.SetBytes(int64(len(arc)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		dst, err := os.MkdirTemp("", "untar-bench-*")
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()

		if err := UntarGz(dst, bytes.NewReader(arc)); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()

		_ = os.RemoveAll(dst)
	}
}

func buildSingleDirArchive(b *testing.B, entries int) []byte {
	b.Helper()

	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	const payloadSize = 64

	payload := make([]byte, payloadSize)

	for i := 0; i < entries; i++ {
		hdr := &tar.Header{
			Name:     fmt.Sprintf("flat/entry-%05d.bin", i),
			Mode:     0o644,
			Size:     int64(payloadSize),
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

func buildUniqueDirArchive(b *testing.B, entries int) []byte {
	b.Helper()

	var buf bytes.Buffer

	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	const payloadSize = 64

	payload := make([]byte, payloadSize)

	for i := 0; i < entries; i++ {
		hdr := &tar.Header{
			Name:     fmt.Sprintf("dir-%05d/file.bin", i),
			Mode:     0o644,
			Size:     int64(payloadSize),
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
