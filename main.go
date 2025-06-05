// Package main provides the entry point for the krci-cache application.
package main

import (
	"log"

	"github.com/KubeRocketCI/krci-cache/uploader"
)

func main() {
	err := uploader.Uploader()
	if err != nil {
		log.Fatal(err)
	}
}
