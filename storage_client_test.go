// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	storagehelper "github.com/ccache/ccache-go-storage-helper"
)

type delayedBodyCloseTransport struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (t delayedBodyCloseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.started <- struct{}{}
	go func() {
		<-t.release
		req.Body.Close()
	}()
	return nil, errors.New("transport error")
}

func TestStorageClientAllowsConcurrentRequests(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("value"))
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	client, err := newStorageClient(&config{
		URL:     baseURL,
		Layout:  layoutSubdirs,
		Headers: map[string]string{},
	}, storagehelper.NewLogger(""))
	if err != nil {
		t.Fatalf("newStorageClient returned error: %v", err)
	}

	var wg sync.WaitGroup
	for _, key := range [][]byte{{0x01}, {0x02}} {
		wg.Add(1)
		go func(key []byte) {
			defer wg.Done()
			body, _, found, err := client.Get(key)
			if body != nil {
				defer body.Close()
			}
			if err != nil {
				t.Errorf("get(%x) returned error: %v", key, err)
			} else if !found {
				t.Errorf("get(%x) returned found=false, want true", key)
			} else if _, err := io.Copy(io.Discard, body); err != nil {
				t.Errorf("drain get(%x) body: %v", key, err)
			}
		}(key)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			close(release)
			wg.Wait()
			t.Fatal("timed out waiting for concurrent requests to reach the server")
		}
	}

	close(release)
	wg.Wait()
}

func TestStorageClientPutWithoutOverwritePropagatesHeadErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusInternalServerError)
		case http.MethodPut:
			t.Fatal("unexpected PUT after failing HEAD preflight")
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	client, err := newStorageClient(&config{
		URL:     baseURL,
		Layout:  layoutFlat,
		Headers: map[string]string{},
	}, storagehelper.NewLogger(""))
	if err != nil {
		t.Fatalf("newStorageClient returned error: %v", err)
	}

	payload := []byte("payload")
	stored, err := client.Put([]byte{0xf0, 0x0d}, bytes.NewReader(payload), int64(len(payload)), false)
	if err == nil {
		t.Fatal("put returned nil error, want HTTP 500")
	}
	if stored {
		t.Fatal("put returned stored=true, want false")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("put returned error %q, want HTTP 500", err)
	}
}

func TestStorageClientPutWaitsForTransportToCloseBody(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	client := &storageClient{
		client: &http.Client{
			Transport: delayedBodyCloseTransport{
				started: started,
				release: release,
			},
		},
		baseURL: &url.URL{Scheme: "http", Host: "example.com"},
		layout:  layoutFlat,
		logger:  storagehelper.NewLogger(""),
	}

	result := make(chan error, 1)
	go func() {
		_, err := client.Put([]byte{0xf0, 0x0d}, bytes.NewReader([]byte("payload")), 7, true)
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request to reach the transport")
	}

	select {
	case err := <-result:
		t.Fatalf("put returned before transport closed the body: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("put returned nil error, want transport error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for put to return")
	}
}

func TestStorageClientBearerTokenFileIsReadPerRequest(t *testing.T) {
	var mu sync.Mutex
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("value"))
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("first-token\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	client, err := newStorageClient(&config{
		URL:             baseURL,
		Layout:          layoutFlat,
		BearerTokenFile: tokenFile,
		Headers:         map[string]string{},
	}, storagehelper.NewLogger(""))
	if err != nil {
		t.Fatalf("newStorageClient returned error: %v", err)
	}

	get := func() {
		t.Helper()
		body, _, found, err := client.Get([]byte{0x01})
		if err != nil {
			t.Fatalf("get returned error: %v", err)
		}
		if !found {
			t.Fatal("get returned found=false, want true")
		}
		defer body.Close()
		if _, err := io.Copy(io.Discard, body); err != nil {
			t.Fatalf("drain get body: %v", err)
		}
	}

	get()

	if err := os.WriteFile(tokenFile, []byte("second-token\n"), 0o600); err != nil {
		t.Fatalf("rewrite token file: %v", err)
	}

	get()

	want := []string{"Bearer first-token", "Bearer second-token"}
	mu.Lock()
	defer mu.Unlock()
	if len(authHeaders) != len(want) {
		t.Fatalf("want %d requests, got %d", len(want), len(authHeaders))
	}
	for i := range want {
		if authHeaders[i] != want[i] {
			t.Fatalf("Authorization header of request %d: want %q, got %q", i, want[i], authHeaders[i])
		}
	}
}

func TestStorageClientMissingBearerTokenFileFailsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unexpected request with unreadable bearer token file")
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	client, err := newStorageClient(&config{
		URL:             baseURL,
		Layout:          layoutFlat,
		BearerTokenFile: filepath.Join(t.TempDir(), "missing"),
		Headers:         map[string]string{},
	}, storagehelper.NewLogger(""))
	if err != nil {
		t.Fatalf("newStorageClient returned error: %v", err)
	}

	body, _, found, err := client.Get([]byte{0x01})
	if body != nil {
		defer body.Close()
	}
	if err == nil {
		t.Fatal("get returned nil error, want bearer token file error")
	}
	if found {
		t.Fatal("get returned found=true, want false")
	}
	if !strings.Contains(err.Error(), "bearer token file") {
		t.Fatalf("get returned error %q, want bearer token file error", err)
	}
}

func TestStorageClientEmptyBearerTokenFileFailsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unexpected request with empty bearer token file")
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	client, err := newStorageClient(&config{
		URL:             baseURL,
		Layout:          layoutFlat,
		BearerTokenFile: tokenFile,
		Headers:         map[string]string{},
	}, storagehelper.NewLogger(""))
	if err != nil {
		t.Fatalf("newStorageClient returned error: %v", err)
	}

	_, _, _, err = client.Get([]byte{0x01})
	if err == nil {
		t.Fatal("get returned nil error, want empty bearer token file error")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("get returned error %q, want empty bearer token file error", err)
	}
}
