// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"fmt"
	"os"
	"runtime"

	storagehelper "github.com/ccache/ccache-go-storage-helper"
)

const version = "0.10"

const helpText = `This is a ccache HTTP(S) storage helper, usually started automatically by ccache
when needed. More information here: https://ccache.dev/storage-helpers.html

Project: https://github.com/ccache/ccache-storage-http-go
Version: ` + version + `
`

func main() {
	if os.Getenv("CRSH_IPC_ENDPOINT") == "" || os.Getenv("CRSH_URL") == "" {
		fmt.Fprint(os.Stderr, helpText)
		os.Exit(1)
	}

	logger := storagehelper.NewLogger(os.Getenv("CRSH_LOGFILE"))
	defer logger.Close()

	logger.Logf("Starting")

	config, err := parseConfig(logger)
	if err != nil {
		logger.Logf("Error: %v", err)
		os.Exit(1)
	}

	storage, err := newStorageClient(config, logger)
	if err != nil {
		logger.Logf("Failed to create storage: %v", err)
		os.Exit(1)
	}

	rootDir := "/"
	if runtime.GOOS == "windows" {
		rootDir = `C:\`
	}
	if err := os.Chdir(rootDir); err != nil {
		logger.Logf("Warning: failed to chdir to root: %v", err)
	}

	identity := "ccache-storage-http-go " + version
	server := storagehelper.NewServer(config.Config, identity, storage, logger)
	if err := server.Run(); err != nil {
		logger.Logf("Server error: %v", err)
		os.Exit(1)
	}
}
