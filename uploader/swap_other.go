//go:build !linux

package uploader

// swapPaths is unavailable on non-Linux platforms; publishDir falls back to
// the two-step rename path. Present so tests run on macOS/Windows.
func swapPaths(_, _ string) error {
	return errSwapUnsupported
}
