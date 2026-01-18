// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2026 Joel Rosdahl

package main

import (
	"fmt"
	"os"
)

const helpText = `This is a ccache HTTP(S) storage helper, usually started automatically by ccache
when needed. More information here: https://ccache.dev/storage-helpers.html

Project: https://github.com/ccache/ccache-storage-http-go
Version: 0.1
`

func main() {
	if os.Getenv("CRSH_IPC_ENDPOINT") == "" || os.Getenv("CRSH_URL") == "" {
		fmt.Fprint(os.Stderr, helpText)
		os.Exit(1)
	}

	config, err := parseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(config.LogFile)
	defer logger.close()

	logger.logf("Starting")
	logger.logf("IPC endpoint: %s", config.IPCEndpoint)
	logger.logf("URL: %s", config.URL)
	logger.logf("Idle timeout: %s", config.IdleTimeout)

	server, err := newServer(config, logger)
	if err != nil {
		logger.logf("Failed to create server: %v", err)
		os.Exit(1)
	}

	if err := server.run(); err != nil {
		logger.logf("Server error: %v", err)
		os.Exit(1)
	}
}
