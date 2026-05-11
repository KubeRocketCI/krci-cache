// Package uploader provides HTTP upload server functionality with support for file uploads and tar.gz extraction.
package uploader

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var (
	host      = "localhost"
	port      = "8080"
	directory = "./pub"
	// absRootDir is the resolved absolute form of `directory`, cached at startup
	// so the per-request hot path (safeJoin) does no Abs syscalls. Always update
	// it via setUploadDirectory to keep the two vars consistent.
	absRootDir string
)

// setUploadDirectory updates both `directory` and the cached `absRootDir` so
// safeJoin sees a consistent view. Returns an error if the path cannot be
// resolved to an absolute form.
func setUploadDirectory(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("invalid upload directory %q: %w", dir, err)
	}

	directory = dir
	absRootDir = abs

	return nil
}

// safeJoin resolves rel inside the upload directory and returns its absolute
// path. The trailing-separator check in isPathSafe rejects sibling-prefix
// escapes such as "/data" vs "/data-evil".
func safeJoin(rel string) (string, error) {
	abspath := filepath.Join(absRootDir, rel)

	if !isPathSafe(abspath, absRootDir) {
		return "", echo.NewHTTPError(http.StatusForbidden, "DENIED: path escapes upload directory")
	}

	return abspath, nil
}

func upload(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return err
	}

	untargz := c.FormValue("targz")
	path := c.FormValue("path")

	if path == "" {
		path = file.Filename
	}

	abspath, err := safeJoin(path)
	if err != nil {
		return err
	}

	if untargz == "true" {
		if err := extractTarGz(file, abspath); err != nil {
			return err
		}
	} else {
		if err := saveRegularFile(file, abspath); err != nil {
			return err
		}
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":  fmt.Sprintf("File has been uploaded to %s", path),
		"filename": file.Filename,
		"path":     path,
		"size":     file.Size,
	})
}

func extractTarGz(file *multipart.FileHeader, abspath string) error {
	if err := os.MkdirAll(abspath, 0o755); err != nil {
		return err
	}

	src, err := file.Open()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			log.Printf("error closing tar source file: %v", closeErr)
		}
	}()

	return UntarGz(abspath, src)
}

func saveRegularFile(file *multipart.FileHeader, abspath string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			log.Printf("error closing source file: %v", closeErr)
		}
	}()

	if err := os.MkdirAll(filepath.Dir(abspath), 0o755); err != nil {
		return err
	}

	dst, err := os.Create(abspath)
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := dst.Close(); closeErr != nil {
			log.Printf("error closing destination file: %v", closeErr)
		}
	}()

	_, err = io.Copy(dst, src)

	return err
}

func uploaderDelete(c echo.Context) error {
	path := c.FormValue("path")

	abspath, err := safeJoin(path)
	if err != nil {
		return err
	}

	if _, err := os.Stat(abspath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return echo.NewHTTPError(http.StatusNotFound, "Could not find your file")
		}

		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Could not stat your file: %s", err.Error()))
	}

	if err := os.RemoveAll(abspath); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Could not delete your file: %s", err.Error()))
	}

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message": fmt.Sprintf("File %s has been deleted", path),
		"path":    path,
	})
}

func healthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
	})
}

func lastModified(c echo.Context) error {
	abspath, err := safeJoin(c.Param("path"))
	if err != nil {
		return err
	}

	info, err := os.Stat(abspath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return echo.NotFoundHandler(c)
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to stat file")
	}

	c.Response().Header().Set(echo.HeaderLastModified, info.ModTime().UTC().Format(http.TimeFormat))

	return c.NoContent(http.StatusOK)
}

func deleteOldFilesOfDir(c echo.Context) error {
	path := c.FormValue("path")
	days, _ := strconv.Atoi(c.FormValue("days"))
	recursive := c.FormValue("recursive") == "true"

	abspath, err := safeJoin(path)
	if err != nil {
		return err
	}

	files, err := findFilesOlderThanXDays(abspath, days, recursive)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return echo.NotFoundHandler(c)
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find old files")
	}

	if len(files) == 0 {
		return c.JSON(http.StatusAccepted, map[string]interface{}{
			"message": "No old files found to delete",
			"path":    path,
			"days":    days,
			"count":   0,
		})
	}

	deletedCount := 0

	for _, file := range files {
		filePath := filepath.Join(abspath, file.Name())
		if err := os.Remove(filePath); err != nil {
			log.Printf("failed to delete file %s: %v", file.Name(), err)
			continue
		}

		deletedCount++
	}

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":       "Old files deleted successfully",
		"path":          path,
		"days":          days,
		"count":         deletedCount,
		"deleted_count": deletedCount,
	})
}

func isOlderThanXDays(t time.Time, days int) bool {
	return time.Since(t) > (time.Duration(days) * 24 * time.Hour)
}

func findFilesOlderThanXDays(dir string, days int, recursive bool) (files []os.FileInfo, err error) {
	tmpfiles, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, file := range tmpfiles {
		info, err := file.Info()
		if err != nil {
			continue
		}

		if info.Mode().IsRegular() || (recursive && info.IsDir()) {
			if isOlderThanXDays(info.ModTime(), days) {
				files = append(files, info)
			}
		}
	}

	return files, nil
}

// Default server tunables. Only header- and idle-level timeouts are set; body
// read/write timeouts are deliberately left at zero so they cannot truncate
// legitimate multi-GB cache transfers under slow clients or slow disks.
const (
	defaultMaxUploadSize   = "8GB"
	defaultShutdownTimeout = 10 * time.Minute
	readHeaderTimeout      = 10 * time.Second
	idleTimeout            = 120 * time.Second
	healthPath             = "/health"
)

type serverConfig struct {
	maxUploadSize   string
	shutdownTimeout time.Duration
	credentials     string
}

func loadConfig() (serverConfig, error) {
	dir := directory
	if v := os.Getenv("UPLOADER_DIRECTORY"); v != "" {
		dir = v
	}

	if v := os.Getenv("UPLOADER_HOST"); v != "" {
		host = v
	}

	if v := os.Getenv("UPLOADER_PORT"); v != "" {
		port = v
	}

	cfg := serverConfig{
		maxUploadSize:   defaultMaxUploadSize,
		shutdownTimeout: defaultShutdownTimeout,
	}

	if v := os.Getenv("UPLOADER_UPLOAD_CREDENTIALS"); v != "" {
		if _, _, ok := strings.Cut(v, ":"); !ok {
			return cfg, fmt.Errorf("UPLOADER_UPLOAD_CREDENTIALS must use 'username:password' format")
		}

		cfg.credentials = v
	}

	if v := os.Getenv("UPLOADER_MAX_UPLOAD_SIZE"); v != "" {
		cfg.maxUploadSize = v
	}

	if v := os.Getenv("UPLOADER_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid UPLOADER_SHUTDOWN_TIMEOUT %q: %w", v, err)
		}

		cfg.shutdownTimeout = d
	}

	if err := setUploadDirectory(dir); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// registerAuth mirrors go-simple-uploader: only mutating endpoints require creds.
// rawCreds is assumed validated by loadConfig (contains ":"). The expected
// username/password are converted to bytes once so the per-request validator is
// allocation-free.
func registerAuth(e *echo.Echo, rawCreds string) {
	if rawCreds == "" {
		return
	}

	user, pass, _ := strings.Cut(rawCreds, ":")
	expectedUser := []byte(user)
	expectedPass := []byte(pass)

	c := middleware.DefaultBasicAuthConfig
	c.Skipper = func(ctx echo.Context) bool {
		if ctx.Path() == healthPath {
			return true
		}

		method := ctx.Request().Method

		return method == http.MethodHead || method == http.MethodGet
	}
	c.Validator = func(username, password string, _ echo.Context) (bool, error) {
		if subtle.ConstantTimeCompare([]byte(username), expectedUser) == 1 &&
			subtle.ConstantTimeCompare([]byte(password), expectedPass) == 1 {
			return true, nil
		}

		return false, nil
	}
	e.Use(middleware.BasicAuthWithConfig(c))
}

func runWithGracefulShutdown(e *echo.Echo, addr string, shutdownTimeout time.Duration) error {
	serverErr := make(chan error, 1)

	go func() {
		if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}

		serverErr <- nil
	}()

	stop := make(chan os.Signal, 1)

	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case err := <-serverErr:
		return err
	case sig := <-stop:
		log.Printf("received signal %s, shutting down (timeout=%s)", sig, shutdownTimeout)

		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		return e.Shutdown(ctx)
	}
}

// Uploader starts the upload server and blocks until SIGINT/SIGTERM.
func Uploader() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	e := echo.New()
	e.HideBanner = true
	e.Server.ReadHeaderTimeout = readHeaderTimeout
	e.Server.IdleTimeout = idleTimeout

	// Recover first so it catches panics in any later middleware. Logger wraps
	// BodyLimit so 413 rejections are still logged.
	e.Use(middleware.Recover())
	e.Use(middleware.Logger())
	e.Use(middleware.BodyLimit(cfg.maxUploadSize))
	registerAuth(e, cfg.credentials)

	e.Static("/", directory)
	e.GET(healthPath, healthCheck)
	e.HEAD("/:path", lastModified)
	e.POST("/upload", upload)
	e.DELETE("/upload", uploaderDelete)
	e.DELETE("/delete", deleteOldFilesOfDir)

	addr := fmt.Sprintf("%s:%s", host, port)
	log.Printf("krci-cache listening on %s (directory=%s, max_upload=%s, shutdown_timeout=%s)",
		addr, directory, cfg.maxUploadSize, cfg.shutdownTimeout)

	return runWithGracefulShutdown(e, addr, cfg.shutdownTimeout)
}
