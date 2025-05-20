package uploader

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
)

func UntarGz(dst string, r io.Reader) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	return extractTar(dst, tar.NewReader(gzr))
}

func extractTar(dst string, tr *tar.Reader) error {
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header == nil {
			continue
		}

		if err := handleHeader(dst, tr, header); err != nil {
			return err
		}
	}
}

func handleHeader(dst string, tr *tar.Reader, header *tar.Header) error {
	target := filepath.Join(dst, header.Name)

	switch header.Typeflag {
	case tar.TypeDir:
		return createDir(target)
	case tar.TypeReg:
		return writeFile(target, tr, os.FileMode(header.Mode))
	default:
		// пропустить другие типы (симлинки, fifo и т.д.)
		return nil
	}
}

func createDir(path string) error {
	if _, err := os.Stat(path); err != nil {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, r)
	return err
}
