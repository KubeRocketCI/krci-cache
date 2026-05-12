// Package uploader provides HTTP upload server functionality with support for file uploads and tar.gz extraction.
package uploader

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
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
	// Resolved absolute form of `directory`, cached so the per-request hot
	// path does no Abs syscalls. Always update via setUploadDirectory.
	absRootDir string
	// Staging dir; must live on the same filesystem as absRootDir for
	// rename(2) to be atomic.
	absStagePath string
)

func setUploadDirectory(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("invalid upload directory %q: %w", dir, err)
	}

	directory = dir
	absRootDir = abs
	absStagePath = filepath.Join(abs, stagingDir)

	return nil
}

// safeJoin resolves rel inside the upload directory and returns its absolute
// path. The trailing-separator check in isPathSafe rejects sibling-prefix
// escapes such as "/data" vs "/data-evil". Paths that target the staging
// directory (".tmp/...") are rejected because that dir holds in-flight
// uploads that users must not observe or mutate.
func safeJoin(rel string) (string, error) {
	if isStagingPath(rel) {
		return "", echo.NewHTTPError(http.StatusForbidden, "DENIED: path is reserved")
	}

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

func extractTarGz(file *multipart.FileHeader, finalPath string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			log.Printf("error closing tar source file: %v", closeErr)
		}
	}()

	stage, err := os.MkdirTemp(absStagePath, "tar-*")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}

	// MkdirTemp's 0700 default would break sidecars/backups running as other UIDs.
	if err := os.Chmod(stage, 0o755); err != nil {
		removeAllLogged(stage)
		return fmt.Errorf("chmod staging dir: %w", err)
	}

	committed := false

	defer func() {
		if !committed {
			removeAllLogged(stage)
		}
	}()

	if err := UntarGz(stage, src); err != nil {
		return err
	}

	if err := publishDir(finalPath, stage); err != nil {
		return err
	}

	committed = true

	return nil
}

func saveRegularFile(file *multipart.FileHeader, finalPath string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			log.Printf("error closing source file: %v", closeErr)
		}
	}()

	return publishFile(finalPath, src)
}

func uploaderDelete(c echo.Context) error {
	path := c.FormValue("path")

	// Reject empty path explicitly: safeJoin("") resolves to the upload root.
	// Without this guard, a DELETE with a missing or unparseable body (e.g.
	// urlencoded body on DELETE, which net/http does not parse) would wipe
	// the entire cache directory.
	if path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "path is required")
	}

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

		// recursive=true allows directory entries; for those we must use
		// RemoveAll because Remove fails on non-empty dirs. This restores
		// the reference behavior that a prior refactor lost.
		var rmErr error
		if recursive && file.IsDir() {
			rmErr = os.RemoveAll(filePath)
		} else {
			rmErr = os.Remove(filePath)
		}

		if rmErr != nil {
			log.Printf("failed to delete %s: %v", file.Name(), rmErr)
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

	// Skip the staging dir when sweeping the upload root so a misconfigured
	// cron (path="", recursive=true, days=0) can't wipe in-flight uploads.
	skipStaging := dir == absRootDir

	for _, file := range tmpfiles {
		if skipStaging && file.Name() == stagingDir {
			continue
		}

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

// Shared between Uploader() and the test server so route registration cannot drift.
func registerRoutes(e *echo.Echo, fileServer http.Handler) {
	e.GET(healthPath, healthCheck)
	e.HEAD("/:path", lastModified)
	e.POST("/upload", upload)
	e.DELETE("/upload", uploaderDelete)
	e.DELETE("/delete", deleteOldFilesOfDir)
	e.GET("/*", echo.WrapHandler(fileServer))
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

	if err := setupStagingDir(); err != nil {
		return err
	}

	// The multipart parser spools parts >32MB into os.TempDir(); on
	// readOnlyRootFilesystem pods the default /tmp is unwritable so every
	// large upload would fail with EROFS. Point TMPDIR at our writable PVC.
	if err := os.Setenv("TMPDIR", absStagePath); err != nil {
		log.Printf("failed to set TMPDIR=%s: %v (multipart spool may fail on readOnlyRootFilesystem pods)", absStagePath, err)
	}

	e := echo.New()
	e.HideBanner = true
	e.Server.ReadHeaderTimeout = readHeaderTimeout
	e.Server.IdleTimeout = idleTimeout

	// Recover first so it catches panics in later middleware; Logger wraps
	// BodyLimit so 413s are still logged.
	e.Use(middleware.Recover())
	e.Use(middleware.Logger())
	e.Use(middleware.BodyLimit(cfg.maxUploadSize))
	registerAuth(e, cfg.credentials)

	registerRoutes(e, http.FileServer(hideStagingFS{root: http.Dir(absRootDir)}))

	addr := fmt.Sprintf("%s:%s", host, port)
	log.Printf("krci-cache listening on %s (directory=%s, max_upload=%s, shutdown_timeout=%s)",
		addr, directory, cfg.maxUploadSize, cfg.shutdownTimeout)

	return runWithGracefulShutdown(e, addr, cfg.shutdownTimeout)
}
