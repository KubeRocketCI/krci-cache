//go:build linux

package uploader

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// Atomically exchanges a and b via renameat2(RENAME_EXCHANGE). Returns
// errSwapUnsupported when the kernel (<3.15) or filesystem (NFS) rejects
// the flag so the caller can fall back to a two-step rename.
func swapPaths(a, b string) error {
	err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE)
	if err == nil {
		return nil
	}

	if errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return errSwapUnsupported
	}

	return err
}
