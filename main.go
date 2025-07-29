// Package main provides the entry point for the krci-cache application.
package main

import (
	"log"

	"github.com/KubeRocketCI/krci-cache/uploader"
)

func main() {
	log.Println("Starting krci-cache application")

	// Use the simplified uploader (like go-simple-uploader)
	if err := uploader.Uploader(); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
