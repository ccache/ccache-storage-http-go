// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	storagehelper "github.com/ccache/ccache-go-storage-helper"
)

type layout string

const (
	layoutBazel   layout = "bazel"
	layoutFlat    layout = "flat"
	layoutSubdirs layout = "subdirs"
)

type config struct {
	*storagehelper.Config
	URL             *url.URL
	Layout          layout
	BearerToken     string
	BearerTokenFile string
	Headers         map[string]string
	UseNetrc        bool
	NetrcFile       string
}

func parseConfig(logger *storagehelper.Logger) (*config, error) {
	commonConfig, err := storagehelper.ParseConfig(logger)
	if err != nil {
		return nil, err
	}

	cfg := &config{
		Config:  commonConfig,
		Layout:  layoutSubdirs,
		Headers: make(map[string]string),
	}

	parsedURL, err := url.Parse(cfg.Config.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid CRSH_URL: %w", err)
	}
	cfg.URL = parsedURL
	logger.Logf("URL: %s", cfg.URL)

	for _, attribute := range cfg.Attributes {
		key := attribute.Key
		value := attribute.Value

		switch key {
		case "bearer-token":
			cfg.BearerToken = value
		case "bearer-token-file":
			// The helper changes its working directory to the filesystem root
			// after configuration, so anchor a relative path to the invoker's
			// working directory now.
			absPath, err := filepath.Abs(value)
			if err != nil {
				cfg.Diagnostics = append(cfg.Diagnostics, fmt.Sprintf("error: invalid bearer-token-file path %q: %v", value, err))
			} else {
				cfg.BearerTokenFile = absPath
			}
		case "header":
			idx := strings.Index(value, "=")
			if idx >= 0 {
				cfg.Headers[value[:idx]] = value[idx+1:]
			} else {
				cfg.Diagnostics = append(cfg.Diagnostics, fmt.Sprintf("error: invalid header (no \"=\"): %s", value))
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

	if cfg.BearerToken != "" && cfg.BearerTokenFile != "" {
		cfg.Diagnostics = append(cfg.Diagnostics, "error: bearer-token and bearer-token-file cannot both be set")
	}

	for _, diag := range cfg.Diagnostics {
		logger.Logf("%s", diag)
	}

	return cfg, nil
}
