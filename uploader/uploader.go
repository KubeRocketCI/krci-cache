// Package uploader provides HTTP upload server functionality with support for file uploads and tar.gz extraction.
package uploader

import (
	"crypto/subtle"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var (
	// Default configuration values
	host      = "localhost"
	port      = "8080"
	directory = "./pub"
)

// Simple upload handler based on go-simple-uploader pattern
func upload(c echo.Context) error {
	// Get the uploaded file - using Echo's simple method like go-simple-uploader
	file, err := c.FormFile("file")
	if err != nil {
		return err
	}

	// Get form parameters
	untargz := c.FormValue("targz")
	path := c.FormValue("path")

	// Use filename if no path specified
	if path == "" {
		path = file.Filename
	}

	// Directory traversal detection (same as go-simple-uploader)
	savepath := filepath.Join(directory, path)
	abspath, _ := filepath.Abs(savepath)
	absuploaddir, _ := filepath.Abs(directory)

	if !strings.HasPrefix(abspath, absuploaddir) {
		return echo.NewHTTPError(http.StatusForbidden, "DENIED: You should not upload outside the upload directory.")
	}

	// Handle tar.gz extraction
	if untargz == "true" {
		return handleTarGzUpload(c, file, path, savepath, abspath)
	}

	// Handle regular file upload
	return handleRegularUpload(c, file, path, savepath)
}

// handleTarGzUpload handles tar.gz file extraction
func handleTarGzUpload(c echo.Context, file *multipart.FileHeader, path, savepath, abspath string) error {
	if err := os.MkdirAll(savepath, 0o755); err != nil {
		return err
	}

	// Re-open file for tar extraction (same pattern as go-simple-uploader)
	src, err := file.Open()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			fmt.Printf("Error closing tar source file: %v\n", closeErr)
		}
	}()

	err = UntarGz(abspath, src)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":  fmt.Sprintf("File has been uploaded to %s", path),
		"filename": file.Filename,
		"path":     path,
		"size":     file.Size,
	})
}

// handleRegularUpload handles regular file uploads
func handleRegularUpload(c echo.Context, file *multipart.FileHeader, path, savepath string) error {
	// Open the uploaded file
	src, err := file.Open()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			fmt.Printf("Error closing source file: %v\n", closeErr)
		}
	}()

	// Create directory if needed
	if _, err := os.Stat(savepath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(savepath), 0o755); err != nil {
			return err
		}
	}

	dst, err := os.Create(savepath)
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := dst.Close(); closeErr != nil {
			fmt.Printf("Error closing destination file: %v\n", closeErr)
		}
	}()

	// Simple copy operation (same as go-simple-uploader)
	if _, err = io.Copy(dst, src); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":  fmt.Sprintf("File has been uploaded to %s", path),
		"filename": file.Filename,
		"path":     path,
		"size":     file.Size,
	})
}

// Simple delete handler
func uploaderDelete(c echo.Context) error {
	path := c.FormValue("path")

	// Directory traversal detection
	savePath := filepath.Join(directory, path)
	abspath, _ := filepath.Abs(savePath)
	absoluteUploadDir, _ := filepath.Abs(directory)

	if !strings.HasPrefix(abspath, absoluteUploadDir) {
		return echo.NewHTTPError(http.StatusForbidden, "DENIED: You should not upload outside the upload directory.")
	}

	if _, err := os.Stat(abspath); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Could not find your file")
	}

	err := os.RemoveAll(abspath)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Could not delete your file: %s", err.Error()))
	}

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message": fmt.Sprintf("File %s has been deleted", path),
		"path":    path,
	})
}

// Health check handler
func healthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
	})
}

// Simple last modified handler
func lastModified(c echo.Context) error {
	path := c.Param("path")
	filePath := filepath.Join(directory, path)
	abspath, _ := filepath.Abs(filePath)
	absoluteUploadDir, _ := filepath.Abs(directory)

	if !strings.HasPrefix(abspath, absoluteUploadDir) {
		return echo.NewHTTPError(http.StatusForbidden, "DENIED: You should not try to get outside the root directory.")
	}

	info, err := os.Stat(abspath)
	if err != nil {
		return echo.NotFoundHandler(c)
	}

	c.Response().Header().Set(echo.HeaderLastModified, info.ModTime().UTC().Format(http.TimeFormat))

	return c.NoContent(http.StatusOK)
}

// Simple delete old files handler
func deleteOldFilesOfDir(c echo.Context) error {
	path := c.FormValue("path")
	days, _ := strconv.Atoi(c.FormValue("days"))
	recursiveFlag := c.FormValue("recursive")

	if len(recursiveFlag) == 0 {
		recursiveFlag = "false"
	}

	recursive, err := strconv.ParseBool(recursiveFlag)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid recursive parameter")
	}

	filePath := filepath.Join(directory, path)
	abspath, _ := filepath.Abs(filePath)
	absoluteUploadDir, _ := filepath.Abs(directory)

	if !strings.HasPrefix(abspath, absoluteUploadDir) {
		return echo.NewHTTPError(http.StatusForbidden, "DENIED: You should not try to get outside the root directory.")
	}

	_, err = os.Stat(abspath)
	if err != nil {
		return echo.NotFoundHandler(c)
	}

	files, err := findFilesOlderThanXDays(abspath, days, recursive)
	if err != nil {
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
			fmt.Printf("Failed to delete file %s: %v\n", file.Name(), err)
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

// Uploader starts the simplified upload server based on go-simple-uploader pattern
func Uploader() error {
	// Load configuration from environment
	if os.Getenv("UPLOADER_DIRECTORY") != "" {
		directory = os.Getenv("UPLOADER_DIRECTORY")
	}

	if os.Getenv("UPLOADER_HOST") != "" {
		host = os.Getenv("UPLOADER_HOST")
	}

	if os.Getenv("UPLOADER_PORT") != "" {
		port = os.Getenv("UPLOADER_PORT")
	}

	// Create simple Echo instance like go-simple-uploader
	e := echo.New()

	// Only essential middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.Static("/", directory)
	e.GET("/health", healthCheck)
	e.HEAD("/:path", lastModified)
	e.POST("/upload", upload)
	e.DELETE("/upload", uploaderDelete)
	e.DELETE("/delete", deleteOldFilesOfDir)

	// Simple auth setup (same pattern as go-simple-uploader)
	if os.Getenv("UPLOADER_UPLOAD_CREDENTIALS") != "" {
		creds := strings.Split(os.Getenv("UPLOADER_UPLOAD_CREDENTIALS"), ":")
		c := middleware.DefaultBasicAuthConfig
		c.Skipper = func(c echo.Context) bool {
			if c.Path() == "/health" {
				return true
			}

			if (c.Request().Method == "HEAD" || c.Request().Method == "GET") && c.Path() != "/upload" && c.Path() != "/delete" {
				return true
			}

			return false
		}
		c.Validator = func(username, password string, c echo.Context) (bool, error) {
			if subtle.ConstantTimeCompare([]byte(username), []byte(creds[0])) == 1 &&
				subtle.ConstantTimeCompare([]byte(password), []byte(strings.Join(creds[1:], ":"))) == 1 {
				return true, nil
			}

			return false, nil
		}
		e.Use(middleware.BasicAuthWithConfig(c))
	}

	return e.Start(fmt.Sprintf("%s:%s", host, port))
}
