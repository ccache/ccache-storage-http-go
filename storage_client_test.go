// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
