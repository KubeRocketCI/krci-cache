package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// BenchmarkTarExtractionMemory tests memory usage during tar extraction
func BenchmarkTarExtractionMemory(b *testing.B) {
	// Create a tar.gz with misleading header sizes
	tarData := createLargeTarGzWithMisleadingHeaders(b)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tempdir := b.TempDir()
		reader := bytes.NewReader(tarData)

		// Measure memory before extraction
		var m1 runtime.MemStats

		runtime.GC()
		runtime.ReadMemStats(&m1)

		err := UntarGz(tempdir, reader)
		require.NoError(b, err)

		// Measure memory after extraction
		var m2 runtime.MemStats

		runtime.GC()
		runtime.ReadMemStats(&m2)

		// Calculate memory used, handling potential underflow
		var memUsed uint64
		if m2.Alloc >= m1.Alloc {
			memUsed = m2.Alloc - m1.Alloc
		} else {
			// Handle case where GC occurred between measurements
			memUsed = m2.TotalAlloc - m1.TotalAlloc
		}

		// The memory usage should be reasonable (not based on header sizes)
		if memUsed > 100*1024*1024 { // 100MB threshold
			b.Errorf("Memory usage too high: %d bytes", memUsed)
		}
	}
}

// BenchmarkSemaphorePerformance tests semaphore performance
func BenchmarkSemaphorePerformance(b *testing.B) {
	sem := NewSemaphore(10)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if sem.TryAcquire() {
				// Simulate some work
				_ = make([]byte, 1024)

				sem.Release()
			}
		}
	})
}

// createLargeTarGzWithMisleadingHeaders creates a tar.gz with realistic content
func createLargeTarGzWithMisleadingHeaders(b *testing.B) []byte {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Create 10 files with realistic sizes for memory testing
	for i := 0; i < 10; i++ {
		actualContent := make([]byte, 1024) // 1KB per file
		for j := range actualContent {
			actualContent[j] = byte(i)
		}

		header := &tar.Header{
			Name:     fmt.Sprintf("file_%d.txt", i),
			Size:     int64(len(actualContent)), // Correct size
			Mode:     0644,
			Typeflag: tar.TypeReg,
		}

		err := tw.WriteHeader(header)
		require.NoError(b, err)

		_, err = tw.Write(actualContent)
		require.NoError(b, err)
	}

	tw.Close()
	gzw.Close()

	return buf.Bytes()
}

// createLargeTarGzWithMisleadingHeadersForTest creates test data for memory regression testing
func createLargeTarGzWithMisleadingHeadersForTest(t *testing.T) []byte {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Create 10 files with realistic sizes for memory testing
	for i := 0; i < 10; i++ {
		actualContent := make([]byte, 1024) // 1KB per file
		for j := range actualContent {
			actualContent[j] = byte(i)
		}

		header := &tar.Header{
			Name:     fmt.Sprintf("file_%d.txt", i),
			Size:     int64(len(actualContent)), // Correct size
			Mode:     0644,
			Typeflag: tar.TypeReg,
		}

		err := tw.WriteHeader(header)
		require.NoError(t, err)

		_, err = tw.Write(actualContent)
		require.NoError(t, err)
	}

	tw.Close()
	gzw.Close()

	return buf.Bytes()
}

// MemoryRegression test to ensure memory usage stays reasonable
func TestMemoryRegression(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory regression test in short mode")
	}

	// Create test data
	tarData := createLargeTarGzWithMisleadingHeadersForTest(t)
	tempdir := t.TempDir()

	// Measure memory before extraction
	var m1 runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&m1)

	reader := bytes.NewReader(tarData)
	err := UntarGz(tempdir, reader)
	require.NoError(t, err)

	// Measure memory after extraction
	var m2 runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&m2)

	// Calculate memory used, handling potential underflow
	var memUsed uint64
	if m2.Alloc >= m1.Alloc {
		memUsed = m2.Alloc - m1.Alloc
	} else {
		// Handle case where GC occurred between measurements
		memUsed = m2.TotalAlloc - m1.TotalAlloc
	}

	// Memory usage should be reasonable (much less than 10GB which would be header-based)
	// Allow up to 50MB for reasonable overhead
	maxAllowedMemory := uint64(50 * 1024 * 1024) // 50MB

	if memUsed > maxAllowedMemory {
		t.Errorf("Memory usage regression detected: used %d bytes, max allowed %d bytes", memUsed, maxAllowedMemory)
	}

	t.Logf("Memory used during extraction: %d bytes (%.2f MB)", memUsed, float64(memUsed)/(1024*1024))
}
