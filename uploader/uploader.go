// Package uploader provides HTTP upload server functionality with support for file uploads and tar.gz extraction.
package uploader

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Config holds the server configuration
type Config struct {
	Host                 string
	Port                 string
	Directory            string
	MaxUploadSize        int64         // Maximum file size for regular uploads (0 = unlimited)
	MaxConcurrentUploads int           // Maximum concurrent uploads
	RequestTimeout       time.Duration // Request timeout
	ReadTimeout          time.Duration // Read timeout
	WriteTimeout         time.Duration // Write timeout
	ShutdownTimeout      time.Duration // Graceful shutdown timeout
}

// LoadConfig loads configuration from environment variables
func LoadConfig() Config {
	config := Config{
		Host:                 "localhost",
		Port:                 "8080",
		Directory:            "./pub",
		MaxUploadSize:        0, // unlimited by default
		MaxConcurrentUploads: 10,
		RequestTimeout:       30 * time.Second,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         30 * time.Second,
		ShutdownTimeout:      30 * time.Second,
	}

	if host := os.Getenv("UPLOADER_HOST"); host != "" {
		config.Host = host
	}

	if port := os.Getenv("UPLOADER_PORT"); port != "" {
		config.Port = port
	}

	if dir := os.Getenv("UPLOADER_DIRECTORY"); dir != "" {
		config.Directory = dir
	}

	if maxSize := os.Getenv("UPLOADER_MAX_UPLOAD_SIZE"); maxSize != "" {
		if size, err := strconv.ParseInt(maxSize, 10, 64); err == nil {
			config.MaxUploadSize = size
		}
	}

	if maxConc := os.Getenv("UPLOADER_MAX_CONCURRENT_UPLOADS"); maxConc != "" {
		if conc, err := strconv.Atoi(maxConc); err == nil && conc > 0 {
			config.MaxConcurrentUploads = conc
		}
	}

	if timeout := os.Getenv("UPLOADER_REQUEST_TIMEOUT"); timeout != "" {
		if t, err := time.ParseDuration(timeout); err == nil {
			config.RequestTimeout = t
		}
	}

	return config
}

// Server represents the upload server
type Server struct {
	config       Config
	echo         *echo.Echo
	logger       *slog.Logger
	uploadSem    chan struct{} // Semaphore for limiting concurrent uploads
	shutdownChan chan struct{}
}

// NewServer creates a new upload server
func NewServer(config Config) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Setup simple slog-based logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Rename the timestamp field to match our previous format
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			return a
		},
	}))

	// Configure Echo with timeouts and limits
	e.Server.ReadTimeout = config.ReadTimeout
	e.Server.WriteTimeout = config.WriteTimeout
	e.Server.MaxHeaderBytes = 1 << 20 // 1MB header limit

	// Add middleware
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())
	e.Use(middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: config.RequestTimeout,
	}))

	// Custom logger middleware
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true,
		LogURI:      true,
		LogError:    true,
		LogMethod:   true,
		LogLatency:  true,
		LogRemoteIP: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			errorMsg := ""
			if v.Error != nil {
				errorMsg = v.Error.Error()
			}
			logger.Info("request",
				"method", v.Method,
				"uri", v.URI,
				"status", v.Status,
				"latency", v.Latency,
				"remote_ip", v.RemoteIP,
				"request_id", v.RequestID,
				"error", errorMsg,
			)
			return nil
		},
	}))

	s := &Server{
		config:       config,
		echo:         e,
		logger:       logger,
		uploadSem:    make(chan struct{}, config.MaxConcurrentUploads),
		shutdownChan: make(chan struct{}),
	}

	s.setupRoutes()
	s.setupAuth()

	return s
}

// Start starts the server with graceful shutdown support
func (s *Server) Start() error {
	s.logger.Info("starting server",
		"host", s.config.Host,
		"port", s.config.Port,
		"directory", s.config.Directory,
		"max_upload_size", s.config.MaxUploadSize,
		"max_concurrent_uploads", s.config.MaxConcurrentUploads,
	)

	// Start server in goroutine for graceful shutdown
	errChan := make(chan error, 1)

	go func() {
		if err := s.echo.Start(fmt.Sprintf("%s:%s", s.config.Host, s.config.Port)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-s.shutdownChan:
		return s.shutdown()
	}
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() {
	close(s.shutdownChan)
}

func (s *Server) shutdown() error {
	s.logger.Info("shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	return s.echo.Shutdown(ctx)
}

func (s *Server) setupRoutes() {
	// Health check endpoint
	s.echo.GET("/health", s.healthCheck)

	// Static file serving with cache headers
	s.echo.Static("/", s.config.Directory)

	// File operations
	s.echo.HEAD("/:path", s.lastModified)
	s.echo.POST("/upload", s.upload)
	s.echo.DELETE("/upload", s.deleteFile)
	s.echo.DELETE("/delete", s.deleteOldFiles)
}

func (s *Server) setupAuth() {
	creds := os.Getenv("UPLOADER_UPLOAD_CREDENTIALS")
	if creds == "" {
		s.logger.Debug("no upload credentials set - authentication disabled")
		return
	}

	credParts := strings.Split(creds, ":")
	if len(credParts) < 2 {
		s.logger.Error("invalid credentials format")
		return
	}

	c := middleware.DefaultBasicAuthConfig
	c.Skipper = func(c echo.Context) bool {
		// Skip auth for GET/HEAD requests and health check
		if c.Path() == "/health" {
			return true
		}

		if (c.Request().Method == "HEAD" || c.Request().Method == "GET") &&
			c.Path() != "/upload" && c.Path() != "/delete" {
			return true
		}

		return false
	}
	c.Validator = func(username, password string, c echo.Context) (bool, error) {
		expectedUser := credParts[0]
		expectedPass := strings.Join(credParts[1:], ":")

		return subtle.ConstantTimeCompare([]byte(username), []byte(expectedUser)) == 1 &&
			subtle.ConstantTimeCompare([]byte(password), []byte(expectedPass)) == 1, nil
	}
	s.echo.Use(middleware.BasicAuthWithConfig(c))
}

func (s *Server) healthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
	})
}

func (s *Server) upload(c echo.Context) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)

	// Acquire semaphore for concurrent upload limiting
	select {
	case s.uploadSem <- struct{}{}:
		defer func() { <-s.uploadSem }()
	default:
		s.logger.Debug("upload rejected - too many concurrent uploads",
			"request_id", requestID)
		return echo.NewHTTPError(http.StatusTooManyRequests, "Too many concurrent uploads")
	}

	// Get the uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		s.logger.Error("failed to get form file",
			"error", err,
			"request_id", requestID)

		return echo.NewHTTPError(http.StatusBadRequest, "No file uploaded")
	}

	// Check file size
	if s.config.MaxUploadSize > 0 && file.Size > s.config.MaxUploadSize {
		s.logger.Debug("upload rejected - file too large",
			"file_size", file.Size,
			"max_size", s.config.MaxUploadSize,
			"request_id", requestID)

		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "File too large")
	}

	// Get the upload path
	path := c.FormValue("path")
	if path == "" {
		path = file.Filename
	}

	// Security check: ensure path is safe
	if !s.isPathSafe(path) {
		s.logger.Debug("upload rejected - unsafe path",
			"path", path,
			"request_id", requestID)

		return echo.NewHTTPError(http.StatusForbidden, "Unsafe file path")
	}

	// Open the uploaded file
	src, err := file.Open()
	if err != nil {
		s.logger.Error("failed to open uploaded file",
			"error", err,
			"request_id", requestID)

		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to open file")
	}

	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			s.logger.Error("failed to close source file",
				"error", closeErr,
				"request_id", requestID)
		}
	}()

	s.logger.Info("processing upload",
		"filename", file.Filename,
		"size", file.Size,
		"path", path,
		"request_id", requestID)

	// Determine the full save path
	savePath := filepath.Join(s.config.Directory, path)

	// Handle the upload based on file type or targz form field
	ctx := c.Request().Context()
	targzFlag := c.FormValue("targz")

	if targzFlag == "true" || strings.HasSuffix(strings.ToLower(path), ".tar.gz") {
		err = s.handleTarGzUpload(ctx, savePath, src, path, requestID)
	} else {
		err = s.handleRegularUpload(ctx, savePath, src, path, requestID)
	}

	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	s.logger.Info("upload completed successfully",
		"filename", file.Filename,
		"path", path,
		"request_id", requestID)

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":  fmt.Sprintf("File has been uploaded to %s", path),
		"filename": file.Filename,
		"path":     path,
		"size":     file.Size,
	})
}

func (s *Server) handleTarGzUpload(ctx context.Context, savePath string, src io.Reader, _ /* path */, requestID string) error {
	// For tar.gz files, we extract to the target directory (savePath)
	// Create the target directory if it doesn't exist
	if err := os.MkdirAll(savePath, 0755); err != nil {
		s.logger.Error("failed to create directory for tar.gz upload",
			"error", err,
			"path", savePath,
			"request_id", requestID)

		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Extract tar.gz to the target directory
	contextReader := &contextReader{ctx: ctx, reader: src}
	if err := UntarGz(savePath, contextReader); err != nil {
		s.logger.Error("failed to extract tar.gz",
			"error", err,
			"path", savePath,
			"request_id", requestID)

		return fmt.Errorf("failed to extract tar.gz: %w", err)
	}

	return nil
}

func (s *Server) handleRegularUpload(ctx context.Context, savePath string, src io.Reader, _ /* path */, requestID string) error {
	// Create directory if it doesn't exist
	dirPath := filepath.Dir(savePath)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		s.logger.Error("failed to create directory for regular upload",
			"error", err,
			"path", dirPath,
			"request_id", requestID)

		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create destination file
	dst, err := os.Create(savePath)
	if err != nil {
		s.logger.Error("failed to create destination file",
			"error", err,
			"path", savePath,
			"request_id", requestID)

		return fmt.Errorf("failed to create file: %w", err)
	}

	defer func() {
		if closeErr := dst.Close(); closeErr != nil {
			s.logger.Error("failed to close destination file",
				"error", closeErr,
				"path", savePath,
				"request_id", requestID)
		}
	}()

	// Copy file content with context awareness
	contextReader := &contextReader{ctx: ctx, reader: src}
	if _, err := io.Copy(dst, contextReader); err != nil {
		// Clean up partial file on error
		_ = os.Remove(savePath)
		s.logger.Error("failed to copy file content",
			"error", err,
			"path", savePath,
			"request_id", requestID)

		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// contextReader wraps an io.Reader with context cancellation support
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (cr *contextReader) Read(p []byte) (int, error) {
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	default:
		return cr.reader.Read(p)
	}
}

func (s *Server) deleteFile(c echo.Context) error {
	// Try query parameter first, then form value for backward compatibility
	path := c.QueryParam("path")
	if path == "" {
		path = c.FormValue("path")
	}

	if path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Path parameter is required")
	}

	// Security check: ensure path is safe
	if !s.isPathSafe(path) {
		s.logger.Debug("delete rejected - unsafe path", "path", path)
		return echo.NewHTTPError(http.StatusBadRequest, "Unsafe file path")
	}

	fullPath := filepath.Join(s.config.Directory, path)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		s.logger.Debug("delete failed - file not found", "path", path)
		return echo.NewHTTPError(http.StatusNotFound, "File not found")
	}

	// Delete the file
	if err := os.Remove(fullPath); err != nil {
		s.logger.Error("failed to delete file",
			"error", err, "path", path)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete file")
	}

	s.logger.Info("file deleted successfully", "path", path)

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message": fmt.Sprintf("File %s has been deleted", path),
		"path":    path,
	})
}

func (s *Server) lastModified(c echo.Context) error {
	path := c.Param("path")
	filePath := filepath.Join(s.config.Directory, path)

	if !s.isPathSafe(filePath) {
		return echo.NewHTTPError(http.StatusForbidden, "DENIED: You should not try to get outside the root directory.")
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return echo.NotFoundHandler(c)
	}

	// Add caching headers for better performance
	c.Response().Header().Set(echo.HeaderLastModified, info.ModTime().UTC().Format(http.TimeFormat))
	c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=3600")

	return c.NoContent(http.StatusOK)
}

// deleteOldFilesParams holds the parameters for deleting old files
type deleteOldFilesParams struct {
	path      string
	days      int
	recursive bool
}

func (s *Server) deleteOldFiles(c echo.Context) error {
	params, err := s.parseDeleteOldFilesParams(c)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(s.config.Directory, params.path)
	if err := s.validateDeleteOldFilesPath(params.path, fullPath); err != nil {
		return err
	}

	files, err := findFilesOlderThanXDays(fullPath, params.days, params.recursive)
	if err != nil {
		s.logger.Error("failed to find old files",
			"error", err, "path", params.path, "days", params.days)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find old files")
	}

	if len(files) == 0 {
		return s.respondNoOldFiles(c, params)
	}

	deletedFiles := s.deleteFilesFromList(files, fullPath)

	return s.respondDeletedFiles(c, params, files, deletedFiles)
}

func (s *Server) parseDeleteOldFilesParams(c echo.Context) (*deleteOldFilesParams, error) {
	// Try query parameter first, then form value for backward compatibility
	path := c.QueryParam("path")
	if path == "" {
		path = c.FormValue("path")
	}

	if path == "" {
		path = ""
	}

	daysStr := c.QueryParam("days")
	if daysStr == "" {
		daysStr = c.FormValue("days")
	}

	if daysStr == "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Days parameter is required")
	}

	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Invalid days parameter")
	}

	recursiveStr := c.QueryParam("recursive")
	if recursiveStr == "" {
		recursiveStr = c.FormValue("recursive")
	}

	recursive := false
	if recursiveStr != "" {
		recursive, err = strconv.ParseBool(recursiveStr)
		if err != nil {
			s.logger.Debug("invalid recursive parameter", "value", recursiveStr)
			return nil, echo.NewHTTPError(http.StatusBadRequest, "Invalid recursive parameter")
		}
	}

	return &deleteOldFilesParams{
		path:      path,
		days:      days,
		recursive: recursive,
	}, nil
}

func (s *Server) validateDeleteOldFilesPath(path, fullPath string) error {
	// Security check: ensure path is safe
	if !s.isPathSafe(path) {
		s.logger.Debug("delete old files rejected - unsafe path", "path", path)
		return echo.NewHTTPError(http.StatusBadRequest, "Unsafe file path")
	}

	// Check if directory exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		s.logger.Debug("delete old files failed - directory not found", "path", path)
		return echo.NewHTTPError(http.StatusNotFound, "Directory not found")
	}

	return nil
}

func (s *Server) deleteFilesFromList(files []os.FileInfo, fullPath string) []string {
	deletedFiles := []string{}

	for _, file := range files {
		filePath := filepath.Join(fullPath, file.Name())
		if err := os.Remove(filePath); err != nil {
			s.logger.Error("failed to delete old file",
				"error", err, "file", file.Name())
			// Continue with other files even if one fails
			continue
		}

		deletedFiles = append(deletedFiles, file.Name())
	}

	return deletedFiles
}

func (s *Server) respondNoOldFiles(c echo.Context, params *deleteOldFilesParams) error {
	s.logger.Info("no old files found to delete",
		"path", params.path, "days", params.days)

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":       "NO Old Files found to delete",
		"path":          params.path,
		"days":          params.days,
		"count":         0,
		"files_count":   0,
		"deleted_files": []string{},
	})
}

func (s *Server) respondDeletedFiles(c echo.Context, params *deleteOldFilesParams, files []os.FileInfo, deletedFiles []string) error {
	s.logger.Info("old files deleted successfully",
		"path", params.path, "days", params.days,
		"total_files", len(files),
		"deleted_files", len(deletedFiles))

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":       "Old files deleted successfully",
		"path":          params.path,
		"days":          params.days,
		"count":         len(deletedFiles), // Use "count" for compatibility with tests
		"files_count":   len(files),
		"deleted_count": len(deletedFiles),
		"deleted_files": deletedFiles,
	})
}

// isPathSafe checks if the given path is within the upload directory
func (s *Server) isPathSafe(path string) bool {
	// Handle path traversal attempts
	if strings.Contains(path, "..") {
		return false
	}

	var fullPath string
	if filepath.IsAbs(path) {
		// For absolute paths, use as-is
		fullPath = path
	} else {
		// For relative paths, join with upload directory first
		fullPath = filepath.Join(s.config.Directory, path)
	}

	abspath, err := filepath.Abs(fullPath)
	if err != nil {
		return false
	}

	absoluteUploadDir, err := filepath.Abs(s.config.Directory)
	if err != nil {
		return false
	}

	return strings.HasPrefix(abspath, absoluteUploadDir)
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
