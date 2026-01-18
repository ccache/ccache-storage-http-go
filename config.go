// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2026 Joel Rosdahl

package main

import (
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type config struct {
	LogFile     string
	IPCEndpoint string
	URL         *url.URL
	IdleTimeout time.Duration
	Layout      string
	BearerToken string
	Headers     map[string]string
}

func parseConfig() (*config, error) {
	ipcEndpoint := os.Getenv("CRSH_IPC_ENDPOINT")
	if runtime.GOOS == "windows" {
		ipcEndpoint = `\\.\pipe\` + ipcEndpoint
	}
	cfg := &config{
		LogFile:     os.Getenv("CRSH_LOGFILE"),
		IPCEndpoint: ipcEndpoint,
		Layout:      "subdirs",
		Headers:     make(map[string]string),
	}

	urlStr := os.Getenv("CRSH_URL")
	if urlStr == "" {
		return nil, fmt.Errorf("CRSH_URL not set")
	}
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CRSH_URL: %w", err)
	}
	cfg.URL = parsedURL

	idleTimeout := os.Getenv("CRSH_IDLE_TIMEOUT")
	if idleTimeout == "" {
		idleTimeout = "0"
	}
	timeoutSecs, err := strconv.Atoi(idleTimeout)
	if err != nil {
		return nil, fmt.Errorf("invalid CRSH_IDLE_TIMEOUT: %w", err)
	}
	cfg.IdleTimeout = time.Duration(timeoutSecs) * time.Second

	numAttr := os.Getenv("CRSH_NUM_ATTR")
	if numAttr == "" {
		numAttr = "0"
	}
	n, err := strconv.Atoi(numAttr)
	if err != nil {
		return nil, fmt.Errorf("invalid CRSH_NUM_ATTR: %w", err)
	}
	for i := 0; i < n; i++ {
		key := os.Getenv(fmt.Sprintf("CRSH_ATTR_KEY_%d", i))
		value := os.Getenv(fmt.Sprintf("CRSH_ATTR_VALUE_%d", i))
		if key == "" {
			continue
		}

		switch key {
		case "layout":
			cfg.Layout = value
		case "bearer-token":
			cfg.BearerToken = value
		case "header":
			idx := strings.Index(value, "=")
			if idx <= 0 {
				continue
			}
			headerKey := value[:idx]
			headerValue := value[idx+1:]
			cfg.Headers[headerKey] = headerValue
		}
	}

	return cfg, nil
}
