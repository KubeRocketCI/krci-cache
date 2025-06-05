// Package main provides the entry point for the krci-cache application.
package main

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KubeRocketCI/krci-cache/uploader"
)

// setupLogger creates a structured JSON logger using slog
func setupLogger() *slog.Logger {
	// Create slog JSON handler for structured logging
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Rename the timestamp field to match our previous format
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			return a
		},
	})

	return slog.New(handler)
}

func runServer(server *uploader.Server, logger *slog.Logger) {
	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	serverErrChan := make(chan error, 1)

	go func() {
		if err := server.Start(); err != nil {
			serverErrChan <- err
		}
	}()

	// Wait for shutdown signal or server error
	handleServerLifecycle(logger, server, sigChan, serverErrChan)
}

func handleServerLifecycle(logger *slog.Logger, server *uploader.Server, sigChan chan os.Signal, serverErrChan chan error) {
	select {
	case err := <-serverErrChan:
		if err != nil {
			logger.Error("server error", "error", err)
			log.Fatal(err)
		}
	case sig := <-sigChan:
		logger.Info("received shutdown signal", "signal", sig.String())
		server.Shutdown()
		waitForShutdown(logger, serverErrChan)
	}
}

func waitForShutdown(logger *slog.Logger, serverErrChan chan error) {
	// Wait for server to finish shutdown with a timeout
	shutdownTimeout := time.NewTimer(2 * time.Second)
	defer shutdownTimeout.Stop()

	select {
	case err := <-serverErrChan:
		if err != nil {
			logger.Error("error during shutdown", "error", err)
		}

		logger.Info("server shutdown completed")
	case <-shutdownTimeout.C:
		logger.Info("shutdown timeout reached, forcing exit")
	}
}

func main() {
	// Setup structured JSON logging with slog
	logger := setupLogger()
	logger.Info("starting krci-cache application")

	// Load configuration and start server
	config := uploader.LoadConfig()
	server := uploader.NewServer(config)

	// Run the server with graceful shutdown handling
	runServer(server, logger)

	logger.Info("krci-cache application stopped")
}
