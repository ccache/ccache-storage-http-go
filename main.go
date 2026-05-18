// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"fmt"
	"os"
	"runtime"
)

const version = "0.7"

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

	logger := newLogger(os.Getenv("CRSH_LOGFILE"))
	defer logger.close()

	logger.logf("Starting")

	config, err := parseConfig(logger)
	if err != nil {
		logger.logf("Error: %v", err)
		os.Exit(1)
	}

	server, err := newServer(config, logger)
	if err != nil {
		logger.logf("Failed to create server: %v", err)
		os.Exit(1)
	}

	rootDir := "/"
	if runtime.GOOS == "windows" {
		rootDir = `C:\`
	}
	if err := os.Chdir(rootDir); err != nil {
		logger.logf("Warning: failed to chdir to root: %v", err)
	}

	if err := server.run(); err != nil {
		logger.logf("Server error: %v", err)
		os.Exit(1)
	}
}
