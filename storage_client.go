// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2026 Joel Rosdahl

package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type storageClient struct {
	client      *http.Client
	baseURL     *url.URL
	layout      string
	bearerToken string
	headers     map[string]string
	logger      *logger
	mu          sync.Mutex
}

func newStorageClient(cfg *config, logger *logger) (*storageClient, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return &storageClient{
		client:      client,
		baseURL:     cfg.URL,
		layout:      cfg.Layout,
		bearerToken: cfg.BearerToken,
		headers:     cfg.Headers,
		logger:      logger,
	}, nil
}

func (s *storageClient) keyToPath(key []byte) string {
	keyHex := hex.EncodeToString(key)

	switch s.layout {
	case "flat":
		return keyHex

	case "bazel":
		// Bazel format: ac/ + 64 hex digits, so pad shorter keys by repeating the key prefix to reach the expected SHA256 size.
		const sha256HexSize = 64
		if len(keyHex) >= sha256HexSize {
			return fmt.Sprintf("ac/%s", keyHex[:sha256HexSize])
		}
		return fmt.Sprintf("ac/%s%s", keyHex, keyHex[:sha256HexSize-len(keyHex)])

	default: // subdirs
		if len(keyHex) < 2 {
			return keyHex
		}
		return fmt.Sprintf("%s/%s", keyHex[:2], keyHex[2:])
	}
}

func (s *storageClient) buildURL(key []byte) (string, error) {
	base := *s.baseURL // Copy to avoid modifying the original
	path := s.keyToPath(key)
	if strings.HasSuffix(base.Path, "/") {
		base.Path = base.Path + path
	} else if base.Path == "" {
		base.Path = "/" + path
	} else {
		base.Path = base.Path + "/" + path
	}

	return base.String(), nil
}

func (s *storageClient) get(key []byte) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	urlStr, err := s.buildURL(key)
	if err != nil {
		return nil, false, err
	}

	s.logger.logf("GET %s", urlStr)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, false, err
	}

	s.addHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	value, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}

	return value, true, nil
}

func (s *storageClient) put(key []byte, value []byte, overwrite bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	urlStr, err := s.buildURL(key)
	if err != nil {
		return false, err
	}

	if !overwrite {
		exists, err := s.exists(urlStr)
		if err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	}

	s.logger.logf("PUT %s (%d bytes)", urlStr, len(value))
	req, err := http.NewRequest("PUT", urlStr, bytes.NewReader(value))
	if err != nil {
		return false, err
	}

	s.addHeaders(req)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	io.Copy(io.Discard, resp.Body) // Read and discard to enable connection reuse

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}

	return false, fmt.Errorf("HTTP %d", resp.StatusCode)
}

func (s *storageClient) remove(key []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	urlStr, err := s.buildURL(key)
	if err != nil {
		return false, err
	}

	s.logger.logf("DELETE %s", urlStr)
	req, err := http.NewRequest("DELETE", urlStr, nil)
	if err != nil {
		return false, err
	}

	s.addHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	io.Copy(io.Discard, resp.Body) // Read and discard to enable connection reuse

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}

	return false, fmt.Errorf("HTTP %d", resp.StatusCode)
}

func (s *storageClient) exists(urlStr string) (bool, error) {
	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err != nil {
		return false, err
	}

	s.addHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	io.Copy(io.Discard, resp.Body) // Read and discard to enable connection reuse

	return resp.StatusCode == http.StatusOK, nil
}

func (s *storageClient) addHeaders(req *http.Request) {
	if s.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.bearerToken)
	}

	for key, value := range s.headers {
		req.Header.Set(key, value)
	}
}
