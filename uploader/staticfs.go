package uploader

import (
	"net/http"
	"os"
)

// Hides the staging dir from static serving and refuses directory opens
// (so http.FileServer cannot render listings).
type hideStagingFS struct {
	root http.FileSystem
}

func (fs hideStagingFS) Open(name string) (http.File, error) {
	if isStagingPath(name) {
		return nil, os.ErrNotExist
	}

	f, err := fs.root.Open(name)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	if info.IsDir() {
		_ = f.Close()
		return nil, os.ErrNotExist
	}

	return f, nil
}
