// Package uploader provides HTTP upload server functionality with support for file uploads and tar.gz extraction.
package uploader

import (
	"crypto/subtle"
	"fmt"
	"io"
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
	Host      string
	Port      string
	Directory string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() Config {
	config := Config{
		Host:      "localhost",
		Port:      "8080",
		Directory: "./pub",
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

	return config
}

// Server represents the upload server
type Server struct {
	config Config
	echo   *echo.Echo
}

// NewServer creates a new upload server
func NewServer(config Config) *Server {
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	s := &Server{
		config: config,
		echo:   e,
	}

	s.setupRoutes()
	s.setupAuth()

	return s
}

// Start starts the server
func (s *Server) Start() error {
	return s.echo.Start(fmt.Sprintf("%s:%s", s.config.Host, s.config.Port))
}

func (s *Server) setupRoutes() {
	s.echo.Static("/", s.config.Directory)
	s.echo.HEAD("/:path", s.lastModified)
	s.echo.POST("/upload", s.upload)
	s.echo.DELETE("/upload", s.deleteFile)
	s.echo.DELETE("/delete", s.deleteOldFiles)
}

func (s *Server) setupAuth() {
	creds := os.Getenv("UPLOADER_UPLOAD_CREDENTIALS")
	if creds == "" {
		return
	}

	credParts := strings.Split(creds, ":")
	if len(credParts) < 2 {
		return
	}

	c := middleware.DefaultBasicAuthConfig
	c.Skipper = func(c echo.Context) bool {
		// Skip auth for GET/HEAD requests except protected endpoints
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

func (s *Server) upload(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return err
	}

	untargz := c.FormValue("targz")
	path := c.FormValue("path")

	// Security: prevent directory traversal
	savePath := filepath.Join(s.config.Directory, path)
	if !s.isPathSafe(savePath) {
		return echo.NewHTTPError(http.StatusForbidden, "DENIED: You should not upload outside the upload directory.")
	}

	src, err := file.Open()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			fmt.Println(closeErr.Error())
		}
	}()

	if untargz != "" {
		if err := s.handleTarGzUpload(savePath, src, path); err != nil {
			return err
		}
	} else {
		if err := s.handleRegularUpload(savePath, src, path); err != nil {
			return err
		}
	}

	return c.HTML(http.StatusCreated, fmt.Sprintf("File has been uploaded to %s 🚀\n", path))
}

func (s *Server) handleTarGzUpload(savePath string, src io.Reader, _ string) error {
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return err
	}

	if err := UntarGz(savePath, src); err != nil {
		fmt.Println(err.Error())
		return err
	}

	return nil
}

func (s *Server) handleRegularUpload(savePath string, src io.Reader, _ string) error {
	if _, err := os.Stat(savePath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
			return err
		}
	}

	dst, err := os.Create(savePath)
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := dst.Close(); closeErr != nil {
			fmt.Println(closeErr.Error())
		}
	}()

	if _, err = io.Copy(dst, src); err != nil {
		return err
	}

	return nil
}

func (s *Server) deleteFile(c echo.Context) error {
	path := c.FormValue("path")
	savePath := filepath.Join(s.config.Directory, path)

	if !s.isPathSafe(savePath) {
		return echo.NewHTTPError(http.StatusForbidden, "DENIED: You should not upload outside the upload directory.")
	}

	if _, err := os.Stat(savePath); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Could not find your file")
	}

	if err := os.RemoveAll(savePath); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Could not delete your file: %s", err.Error()))
	}

	return c.HTML(http.StatusAccepted, fmt.Sprintf("File %s has been deleted 💇", path))
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

	c.Response().Header().Set(echo.HeaderLastModified, info.ModTime().UTC().Format(http.TimeFormat))

	return c.NoContent(http.StatusOK)
}

func (s *Server) deleteOldFiles(c echo.Context) error {
	path := c.FormValue("path")
	days, _ := strconv.Atoi(c.FormValue("days"))
	recursiveFlag := c.FormValue("recursive")

	if recursiveFlag == "" {
		recursiveFlag = "false"
	}

	recursive, err := strconv.ParseBool(recursiveFlag)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "DENIED: check if your formvalue recursive should be any of this ('true', 'True', 'false','False','TRUE','FALSE','f','t','F','T', '') ")
	}

	filePath := filepath.Join(s.config.Directory, path)
	if !s.isPathSafe(filePath) {
		return echo.NewHTTPError(http.StatusForbidden, "DENIED: You should not try to get outside the root directory.")
	}

	if _, err := os.Stat(filePath); err != nil {
		return echo.NotFoundHandler(c)
	}

	files, err := findFilesOlderThanXDays(filePath, days, recursive)
	if err != nil {
		return echo.NotFoundHandler(c)
	}

	if len(files) == 0 {
		return c.HTML(http.StatusAccepted, fmt.Sprintf("There are NO Old Files more than %d days to be deleted 💇", days))
	}

	for _, file := range files {
		newFilePath := filepath.Join(filePath, file.Name())
		if recursive && file.IsDir() {
			err = os.RemoveAll(newFilePath)
		} else {
			err = os.Remove(newFilePath)
		}

		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Could not delete your file: %s", err.Error()))
		}
	}

	if recursive {
		return c.HTML(http.StatusAccepted, fmt.Sprintf("Old Files/child directories more than %d days has been deleted 💇", days))
	}

	return c.HTML(http.StatusAccepted, fmt.Sprintf("Old Files more than %d days has been deleted 💇", days))
}

// isPathSafe checks if the given path is within the upload directory
func (s *Server) isPathSafe(path string) bool {
	abspath, _ := filepath.Abs(path)
	absoluteUploadDir, _ := filepath.Abs(s.config.Directory)

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

// Uploader is the legacy function to maintain backward compatibility
func Uploader() error {
	config := LoadConfig()
	server := NewServer(config)

	return server.Start()
}
