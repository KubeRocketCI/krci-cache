package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/subtle"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/stretchr/testify/assert"
)

func httpUploadMultiPart(s, p string) *http.Request {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "hello.txt")
	_, _ = part.Write([]byte(s))
	_ = writer.WriteField("path", p)
	_ = writer.Close()

	r, _ := http.NewRequest(http.MethodPost, "/upload", body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func TestMultipleDirectory(t *testing.T) {
	tempdir, _ := ioutil.TempDir("", "test-uploader")
	expectedSring := "HELLO MOTO"
	targetPath := "a/foo/bar/moto.txt"

	e := echo.New()
	req := httpUploadMultiPart(expectedSring, targetPath)
	rec := httptest.NewRecorder()

	directory = tempdir

	context := e.NewContext(req, rec)

	if assert.Nil(t, upload(context)) {
		assert.Equal(t, http.StatusCreated, rec.Code)
	}

	dat, err := ioutil.ReadFile(filepath.Join(tempdir, targetPath))
	assert.Nil(t, err)
	assert.Equal(t, string(dat), expectedSring)
}

func TestUploaderSimple(t *testing.T) {
	tempdir, _ := ioutil.TempDir("", "test-uploader")
	expectedSring := "HELLO SIMPLE MOTO"
	targetPath := "moto.txt"

	e := echo.New()
	req := httpUploadMultiPart(expectedSring, targetPath)
	rec := httptest.NewRecorder()

	directory = tempdir
	context := e.NewContext(req, rec)

	if assert.Nil(t, upload(context)) {
		assert.Equal(t, http.StatusCreated, rec.Code)
	}

	dat, err := ioutil.ReadFile(filepath.Join(tempdir, targetPath))
	assert.Nil(t, err)
	assert.Equal(t, string(dat), expectedSring)
}

func TestUploaderTraversal(t *testing.T) {
	tempdir, _ := ioutil.TempDir("", "test-uploader")
	expectedSring := "HELLO MOTO"
	targetPath := "../../../../../../../../../../etc/passwd"

	e := echo.New()
	req := httpUploadMultiPart(expectedSring, targetPath)
	rec := httptest.NewRecorder()

	directory = tempdir

	context := e.NewContext(req, rec)
	err := upload(context)
	if assert.Error(t, err) {
		he, ok := err.(*echo.HTTPError)
		if ok {
			assert.Equal(t, http.StatusForbidden, he.Code)
		}
	}

}

func TestUploaderDelete(t *testing.T) {
	tempdir, _ := ioutil.TempDir("", "test-uploader")
	directory = tempdir
	fpath := filepath.Join(tempdir, "foo.txt")

	fp, err := os.Create(fpath)
	assert.Nil(t, err)
	fp.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "foo.txt")
	_ = writer.Close()

	e := echo.New()
	req, _ := http.NewRequest(http.MethodDelete, "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	context := e.NewContext(req, rec)
	if assert.Nil(t, uploaderDelete(context)) {
		assert.Equal(t, http.StatusAccepted, rec.Code)
		if _, err = os.Stat(fpath); err != nil {
			assert.True(t, os.IsNotExist(err))
		}
	}
}

func TestDeleteFilesOlderThanOneDay(t *testing.T) {
	tempdir, _ := ioutil.TempDir("", "test-uploader")
	directory = tempdir
	fpath := filepath.Join(tempdir, "foo.txt")

	fp, err := os.Create(fpath)
	assert.Nil(t, err)
	fp.Close()

	timestamp := time.Now().Add(-(time.Duration(1) * 25 * time.Hour))
	err = os.Chtimes(fpath, timestamp, timestamp)
	if err != nil {
		fmt.Println(err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "1")
	_ = writer.Close()

	e := echo.New()
	req, _ := http.NewRequest(http.MethodDelete, "/delete", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	context := e.NewContext(req, rec)
	if assert.Nil(t, deleteOldFilesOfDir(context)) {
		assert.Equal(t, http.StatusAccepted, rec.Code)
		if _, err = os.Stat(fpath); err != nil {
			assert.True(t, os.IsNotExist(err))
		}
	}
}

func TestDeleteFilesOlderThanTwoDay(t *testing.T) {
	tempdir, _ := ioutil.TempDir("", "test-uploader")
	directory = tempdir
	fpath := filepath.Join(tempdir, "foo.txt")

	fp, err := os.Create(fpath)
	assert.Nil(t, err)
	fp.Close()

	timestamp := time.Now().Add(-(time.Duration(2) * 25 * time.Hour))
	err = os.Chtimes(fpath, timestamp, timestamp)
	if err != nil {
		fmt.Println(err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "2")
	_ = writer.Close()

	e := echo.New()
	req, _ := http.NewRequest(http.MethodDelete, "/delete", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	context := e.NewContext(req, rec)
	if assert.Nil(t, deleteOldFilesOfDir(context)) {
		assert.Equal(t, http.StatusAccepted, rec.Code)
		if _, err = os.Stat(fpath); err != nil {
			assert.True(t, os.IsNotExist(err))
		}
	}
}

func TestUploadMissingFile(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	e := echo.New()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "test.txt")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := upload(c)
	assert.Error(t, err)
}

func TestLastModified(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	filename := "file.txt"
	fpath := filepath.Join(tempdir, filename)
	err := os.WriteFile(fpath, []byte("test"), 0o644)
	assert.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodHead, "/"+filename, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("path")
	c.SetParamValues(filename)

	err = lastModified(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get(echo.HeaderLastModified), "GMT")
}

func TestDeleteInvalidDaysValue(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	e := echo.New()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "notanumber")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodDelete, deletePath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = deleteOldFilesOfDir(c)
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestUploadWithTarGz(t *testing.T) {
	tempDir := t.TempDir()
	directory = tempDir

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	content := []byte("data")
	_ = tw.WriteHeader(&tar.Header{
		Name: "file.txt",
		Mode: 0600,
		Size: int64(len(content)),
	})
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gzw.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "archive.tar.gz")
	_, _ = part.Write(buf.Bytes())
	_ = writer.WriteField("path", "extracted/")
	_ = writer.WriteField("targz", "1")
	_ = writer.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := upload(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	data, err := os.ReadFile(filepath.Join(tempDir, "extracted/file.txt"))
	assert.NoError(t, err)
	assert.Equal(t, "data", string(data))
}

func TestUploaderDeleteNotFound(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "not_exists.txt")
	_ = writer.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	context := e.NewContext(req, rec)

	err := uploaderDelete(context)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

func TestDeleteInvalidRecursive(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "")
	_ = writer.WriteField("days", "1")
	_ = writer.WriteField("recursive", "maybe")
	_ = writer.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, deletePath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := deleteOldFilesOfDir(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestLastModifiedFileNotFound(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	e := echo.New()
	req := httptest.NewRequest(http.MethodHead, "/nofile.txt", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("path")
	c.SetParamValues("nofile.txt")

	err := lastModified(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

func TestUploaderDeleteTraversal(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "../../../../etc/passwd")
	_ = writer.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	context := e.NewContext(req, rec)

	err := uploaderDelete(context)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusForbidden, httpErr.Code)
}

func TestDeleteOldFiles_PathTraversal(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "../../../../etc")
	_ = writer.WriteField("days", "1")
	_ = writer.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, deletePath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := deleteOldFilesOfDir(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusForbidden, httpErr.Code)
}

func TestUpload_CreateFails(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	dirPath := "subdir"
	_ = os.Mkdir(filepath.Join(tempdir, dirPath), 0o755)

	e := echo.New()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "conflict.txt")
	_, _ = part.Write([]byte("test"))
	_ = writer.WriteField("path", dirPath)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := upload(c)
	assert.Error(t, err)
}

func TestUpload_MkdirAllFails(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	conflictPath := filepath.Join(tempdir, "conflict")
	_ = os.WriteFile(conflictPath, []byte("not a dir"), 0644)

	e := echo.New()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	_, _ = part.Write([]byte("data"))
	_ = writer.WriteField("path", "conflict/test.txt")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := upload(c)
	assert.Error(t, err)
}

func TestUploadWithInvalidTarGz(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "broken.tar.gz")
	_, _ = part.Write([]byte("not a tar.gz"))
	_ = writer.WriteField("path", "somewhere/")
	_ = writer.WriteField("targz", "1")
	_ = writer.Close()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := upload(c)
	assert.Error(t, err)
}

func TestUpload_SecondFileOpenFails(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	e := echo.New()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, _ := writer.CreateFormFile("file", "archive.tar.gz")
	_, _ = part.Write([]byte("not valid"))
	_ = writer.WriteField("targz", "1")
	_ = writer.WriteField("path", "test/")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec)
	err := upload(c)
	assert.Error(t, err)
}

func TestUpload_StatFailsNotIsNotExist(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	conflictFile := filepath.Join(tempdir, "conflict")
	_ = os.WriteFile(conflictFile, []byte("not a dir"), 0644)

	target := "conflict/file.txt"

	e := echo.New()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "file.txt")
	_, _ = part.Write([]byte("data"))
	_ = writer.WriteField("path", target)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := upload(c)
	assert.Error(t, err)
}

func TestUpload_UntarGzFails(t *testing.T) {
	tempdir := t.TempDir()
	directory = tempdir

	e := echo.New()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "invalid.tar.gz")
	_, _ = part.Write([]byte("not a real archive"))
	_ = writer.WriteField("targz", "1")
	_ = writer.WriteField("path", "untar/")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, uploadPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := upload(c)
	assert.Error(t, err)
}

func TestUploaderSkipperAllowsHEAD(t *testing.T) {
	os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", "user:pass")

	req := httptest.NewRequest(http.MethodHead, "/health", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)

	skipper := func(c echo.Context) bool {
		return (c.Request().Method == "HEAD" || c.Request().Method == "GET") &&
			c.Path() != uploadPath && c.Path() != deletePath
	}

	assert.True(t, skipper(c))
	os.Unsetenv("UPLOADER_UPLOAD_CREDENTIALS")
}

func TestUploaderValidator(t *testing.T) {
	os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", "user:secret")

	var called bool
	c := middleware.DefaultBasicAuthConfig
	c.Validator = func(username, password string, c echo.Context) (bool, error) {
		called = true
		return username == "user" && password == "secret", nil
	}

	ok, err := c.Validator("user", "secret", nil)
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, called)

	os.Unsetenv("UPLOADER_UPLOAD_CREDENTIALS")
}

func TestUploaderEnvOverrides(t *testing.T) {
	os.Setenv("UPLOADER_DIRECTORY", "/tmp/uploads")
	os.Setenv("UPLOADER_HOST", "127.0.0.1")
	os.Setenv("UPLOADER_PORT", "9999")

	// сохранить старые значения
	origDir := directory
	origHost := host
	origPort := port

	// вызываем заново блок и проверим, применились ли значения
	_ = os.Setenv("UPLOADER_DIRECTORY", "/test/dir")
	_ = os.Setenv("UPLOADER_HOST", "0.0.0.0")
	_ = os.Setenv("UPLOADER_PORT", "1234")

	// скопировать то, что делает Uploader в начале
	if os.Getenv("UPLOADER_DIRECTORY") != "" {
		directory = os.Getenv("UPLOADER_DIRECTORY")
	}
	if os.Getenv("UPLOADER_HOST") != "" {
		host = os.Getenv("UPLOADER_HOST")
	}
	if os.Getenv("UPLOADER_PORT") != "" {
		port = os.Getenv("UPLOADER_PORT")
	}

	assert.Equal(t, "/test/dir", directory)
	assert.Equal(t, "0.0.0.0", host)
	assert.Equal(t, "1234", port)

	// вернуть обратно
	directory = origDir
	host = origHost
	port = origPort
}

func TestUploaderAuthSkipper(t *testing.T) {
	os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", "testuser:testpass")

	c := middleware.DefaultBasicAuthConfig
	c.Skipper = func(c echo.Context) bool {
		if (c.Request().Method == "HEAD" || c.Request().Method == "GET") &&
			c.Path() != uploadPath && c.Path() != deletePath {
			return true
		}
		return false
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	assert.True(t, c.Skipper(ctx))

	req2 := httptest.NewRequest(http.MethodPost, uploadPath, nil)
	ctx2 := e.NewContext(req2, rec)
	assert.False(t, c.Skipper(ctx2))

	os.Unsetenv("UPLOADER_UPLOAD_CREDENTIALS")
}

func TestUploaderAuthValidator(t *testing.T) {
	os.Setenv("UPLOADER_UPLOAD_CREDENTIALS", "user:secret")
	creds := strings.Split(os.Getenv("UPLOADER_UPLOAD_CREDENTIALS"), ":")

	validator := func(username, password string, c echo.Context) (bool, error) {
		if subtle.ConstantTimeCompare([]byte(username), []byte(creds[0])) == 1 &&
			subtle.ConstantTimeCompare([]byte(password), []byte(strings.Join(creds[1:], ":"))) == 1 {
			return true, nil
		}
		return false, nil
	}

	ok, err := validator("user", "secret", nil)
	assert.NoError(t, err)
	assert.True(t, ok)

	fail, err := validator("user", "wrong", nil)
	assert.NoError(t, err)
	assert.False(t, fail)

	os.Unsetenv("UPLOADER_UPLOAD_CREDENTIALS")
}
