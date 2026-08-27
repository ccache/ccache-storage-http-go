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
	"sync"
	"time"

	storagehelper "github.com/ccache/ccache-go-storage-helper"
)

const httpTransportBufferSize = 64 << 10

type storageClient struct {
	client          *http.Client
	baseURL         *url.URL
	layout          layout
	bearerToken     string
	bearerTokenFile string
	headers         map[string]string
	basicAuthUser   string
	basicAuthPass   string
	logger          *storagehelper.Logger
}

// requestBody lets callers wait until the HTTP transport has stopped reading
// from the underlying reader.
type requestBody struct {
	io.Reader
	done chan struct{}
	once sync.Once
}

func newRequestBody(reader io.Reader) *requestBody {
	return &requestBody{
		Reader: reader,
		done:   make(chan struct{}),
	}
}

func (b *requestBody) Close() error {
	b.once.Do(func() {
		close(b.done)
	})
	return nil
}

func (b *requestBody) wait() {
	<-b.done
}

func newStorageClient(cfg *config, logger *storagehelper.Logger) (*storageClient, error) {
	connectionPoolSize := max(32, runtime.GOMAXPROCS(0))
	client := &http.Client{
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
		client:          client,
		baseURL:         cfg.URL,
		layout:          cfg.Layout,
		bearerToken:     cfg.BearerToken,
		bearerTokenFile: cfg.BearerTokenFile,
		headers:         cfg.Headers,
		logger:          logger,
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
					logger.Logf("Warning: could not read netrc file %q: %v", netrcPath, err)
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
		var bazelKey string
		if keyHex != "" {
			for len(bazelKey) < sha256HexSize {
				remaining := sha256HexSize - len(bazelKey)
				if remaining > len(keyHex) {
					remaining = len(keyHex)
				}
				bazelKey += keyHex[:remaining]
			}
		}
		return "ac/" + bazelKey

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

func (s *storageClient) Exists(key []byte) (bool, error) {
	urlStr, err := s.buildURL(key)
	if err != nil {
		return false, err
	}

	s.logger.Logf("EXISTS %s", urlStr)
	return s.head(urlStr)
}

func (s *storageClient) Get(key []byte) (io.ReadCloser, int64, bool, error) {
	urlStr, err := s.buildURL(key)
	if err != nil {
		return nil, 0, false, err
	}

	s.logger.Logf("GET %s", urlStr)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, 0, false, err
	}

	if err := s.addHeaders(req); err != nil {
		return nil, 0, false, err
	}

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

func (s *storageClient) Put(key []byte, value io.Reader, size int64, overwrite bool) (bool, error) {
	urlStr, err := s.buildURL(key)
	if err != nil {
		return false, err
	}

	if !overwrite {
		exists, err := s.head(urlStr)
		if err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	}

	s.logger.Logf("PUT %s (%d bytes)", urlStr, size)
	body := newRequestBody(value)
	req, err := http.NewRequest("PUT", urlStr, body)
	if err != nil {
		return false, err
	}
	req.ContentLength = size
	if err := s.addHeaders(req); err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := s.client.Do(req)
	body.wait()
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

func (s *storageClient) Remove(key []byte) (bool, error) {
	urlStr, err := s.buildURL(key)
	if err != nil {
		return false, err
	}

	s.logger.Logf("DELETE %s", urlStr)
	req, err := http.NewRequest("DELETE", urlStr, nil)
	if err != nil {
		return false, err
	}

	if err := s.addHeaders(req); err != nil {
		return false, err
	}

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

func (s *storageClient) head(urlStr string) (bool, error) {
	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err != nil {
		return false, err
	}

	if err := s.addHeaders(req); err != nil {
		return false, err
	}

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

func (s *storageClient) addHeaders(req *http.Request) error {
	req.Header.Set("User-Agent", "ccache-storage-http-go/"+version)

	bearerToken := s.bearerToken
	if s.bearerTokenFile != "" {
		// Read the file for each request so that a rotated token is picked up
		// without restarting the helper.
		data, err := os.ReadFile(s.bearerTokenFile)
		if err != nil {
			return fmt.Errorf("could not read bearer token file: %w", err)
		}
		bearerToken = strings.TrimSpace(string(data))
		if bearerToken == "" {
			return fmt.Errorf("bearer token file %q is empty", s.bearerTokenFile)
		}
	}

	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	} else if s.basicAuthUser != "" {
		req.SetBasicAuth(s.basicAuthUser, s.basicAuthPass)
	}

	for key, value := range s.headers {
		req.Header.Set(key, value)
	}

	return nil
}
