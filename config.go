// SPDX-License-Identifier: MIT
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

type layout string

const (
	layoutBazel   layout = "bazel"
	layoutFlat    layout = "flat"
	layoutSubdirs layout = "subdirs"
)

type config struct {
	IPCEndpoint string
	URL         *url.URL
	IdleTimeout time.Duration
	FormatMax   int
	Diagnostics []string
	Layout      layout
	BearerToken string
	Headers     map[string]string
	UseNetrc    bool
	NetrcFile   string
}

func parseConfig(logger *logger) (*config, error) {
	ipcEndpoint := os.Getenv("CRSH_IPC_ENDPOINT")
	if runtime.GOOS == "windows" {
		ipcEndpoint = `\\.\pipe\` + ipcEndpoint
	}
	logger.logf("IPC endpoint: %s", ipcEndpoint)

	cfg := &config{
		IPCEndpoint: ipcEndpoint,
		Layout:      layoutSubdirs,
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
	logger.logf("URL: %s", cfg.URL)

	idleTimeout := os.Getenv("CRSH_IDLE_TIMEOUT")
	if idleTimeout == "" {
		idleTimeout = "0"
	}
	timeoutSecs, err := strconv.Atoi(idleTimeout)
	if err != nil {
		return nil, fmt.Errorf("invalid CRSH_IDLE_TIMEOUT: %w", err)
	}
	cfg.IdleTimeout = time.Duration(timeoutSecs) * time.Second
	logger.logf("Idle timeout: %s", cfg.IdleTimeout)

	formatMaxStr := os.Getenv("CRSH_FORMAT_MAX")
	if formatMaxStr == "" {
		cfg.FormatMax = 1
	} else {
		formatMax, err := strconv.Atoi(formatMaxStr)
		if err != nil {
			return nil, fmt.Errorf("invalid CRSH_FORMAT_MAX: %w", err)
		}
		cfg.FormatMax = formatMax
	}

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
		logger.logf("Attribute: %s=%s", key, value)

		switch key {
		case "bearer-token":
			cfg.BearerToken = value
		case "header":
			idx := strings.Index(value, "=")
			if idx >= 0 {
				headerKey := value[:idx]
				headerValue := value[idx+1:]
				cfg.Headers[headerKey] = headerValue
			} else {
				msg := fmt.Sprintf("error: invalid header (no \"=\"): %s", value)
				cfg.Diagnostics = append(cfg.Diagnostics, msg)
			}
		case "layout":
			switch layout(value) {
			case layoutBazel, layoutFlat, layoutSubdirs:
				cfg.Layout = layout(value)
			default:
				cfg.Diagnostics = append(cfg.Diagnostics, fmt.Sprintf("error: unknown layout: %s", value))
			}
		case "netrc-file":
			cfg.NetrcFile = value
			cfg.UseNetrc = true
		case "use-netrc":
			cfg.UseNetrc = value == "true"
		default:
			cfg.Diagnostics = append(cfg.Diagnostics, fmt.Sprintf("warning: unknown attribute: %s", key))
		}
	}

	for _, diag := range cfg.Diagnostics {
		logger.logf("%s", diag)
	}

	return cfg, nil
}
