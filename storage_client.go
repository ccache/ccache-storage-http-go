// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

const httpTransportBufferSize = 64 << 10

type storageClient struct {
	client        *http.Client
	baseURL       *url.URL
	layout        layout
	bearerToken   string
	headers       map[string]string
	basicAuthUser string
	basicAuthPass string
	logger        *logger
}

func newStorageClient(cfg *config, logger *logger) (*storageClient, error) {
	connectionPoolSize := max(32, runtime.GOMAXPROCS(0))
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        connectionPoolSize,
			MaxIdleConnsPerHost: connectionPoolSize,
			MaxConnsPerHost:     connectionPoolSize,
			IdleConnTimeout:     90 * time.Second,
			ReadBufferSize:      httpTransportBufferSize,
			WriteBufferSize:     httpTransportBufferSize,
		},
	}

	sc := &storageClient{
		client:      client,
		baseURL:     cfg.URL,
		layout:      cfg.Layout,
		bearerToken: cfg.BearerToken,
		headers:     cfg.Headers,
		logger:      logger,
	}

	if cfg.UseNetrc {
		netrcPath := cfg.NetrcFile
		if netrcPath == "" {
			netrcPath = defaultNetrcPath()
		}
		if netrcPath != "" {
			requestedLogin := ""
			if cfg.URL.User != nil {
				requestedLogin = cfg.URL.User.Username()
			}

			login, password, err := findNetrcCredentials(netrcPath, cfg.URL.Hostname(), requestedLogin)
			if err != nil {
				if !os.IsNotExist(err) {
					logger.logf("Warning: could not read netrc file %q: %v", netrcPath, err)
				}
			} else {
				sc.basicAuthUser = login
				sc.basicAuthPass = password
			}
		}
	}

	return sc, nil
}

func (s *storageClient) keyToPath(key []byte) string {
	keyHex := hex.EncodeToString(key)

	switch s.layout {
	case layoutFlat:
		return keyHex

	case layoutBazel:
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

func (s *storageClient) get(key []byte) (io.ReadCloser, int64, bool, error) {
	urlStr, err := s.buildURL(key)
	if err != nil {
		return nil, 0, false, err
	}

	s.logger.logf("GET %s", urlStr)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, 0, false, err
	}

	s.addHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, false, err
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, 0, false, nil
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return resp.Body, resp.ContentLength, true, nil
}

func (s *storageClient) put(key []byte, value io.Reader, size int64, overwrite bool) (bool, error) {
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

	s.logger.logf("PUT %s (%d bytes)", urlStr, size)
	req, err := http.NewRequest("PUT", urlStr, value)
	if err != nil {
		return false, err
	}
	req.ContentLength = size
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

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, fmt.Errorf("HTTP %d", resp.StatusCode)
}

func (s *storageClient) addHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "ccache-storage-http-go/"+version)

	if s.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.bearerToken)
	} else if s.basicAuthUser != "" {
		req.SetBasicAuth(s.basicAuthUser, s.basicAuthPass)
	}

	for key, value := range s.headers {
		req.Header.Set(key, value)
	}
}
