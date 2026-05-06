// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

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
	}, newLogger(""))
	if err != nil {
		t.Fatalf("newStorageClient returned error: %v", err)
	}

	var wg sync.WaitGroup
	for _, key := range [][]byte{{0x01}, {0x02}} {
		wg.Add(1)
		go func(key []byte) {
			defer wg.Done()
			body, _, found, err := client.get(key)
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
	}, newLogger(""))
	if err != nil {
		t.Fatalf("newStorageClient returned error: %v", err)
	}

	payload := []byte("payload")
	stored, err := client.put([]byte{0xf0, 0x0d}, bytes.NewReader(payload), int64(len(payload)), false)
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
