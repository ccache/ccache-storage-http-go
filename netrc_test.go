// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeNetrcFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, ".netrc")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write netrc: %v", err)
	}
	return path
}

func TestFindNetrcCredentials(t *testing.T) {
	tests := []struct {
		name           string
		netrc          string
		machine        string
		requestedLogin string
		login          string
		password       string
	}{
		{
			name:     "simple machine match",
			netrc:    "machine cache.example.com login alice password secret\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "secret",
		},
		{
			name:     "default fallback",
			netrc:    "machine other.example.com login bob password other\ndefault login anonymous password guest@example.com\n",
			machine:  "cache.example.com",
			login:    "anonymous",
			password: "guest@example.com",
		},
		{
			name:     "machine match takes precedence over earlier default",
			netrc:    "default login anonymous password guest@example.com\nmachine cache.example.com login alice password secret\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "secret",
		},
		{
			name:     "quoted credentials",
			netrc:    "machine cache.example.com login \"Alice Smith\" password \"sp ace\\nline\\\"quote\"\n",
			machine:  "cache.example.com",
			login:    "Alice Smith",
			password: "sp ace\nline\"quote",
		},
		{
			name:     "hash is data inside token",
			netrc:    "machine cache.example.com login alice password abc#123\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "abc#123",
		},
		{
			name:     "comment line ignored",
			netrc:    "  # comment\nmachine cache.example.com login alice password secret\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "secret",
		},
		{
			name:     "keyword values can be split across lines",
			netrc:    "machine\ncache.example.com\nlogin\nalice\npassword\nsecret\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "secret",
		},
		{
			name:     "macdef block skipped",
			netrc:    "machine cache.example.com login alice password secret macdef init\nlogin wrong-user\npassword wrong-secret\n\ndefault login fallback password fallback-secret\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "secret",
		},
		{
			name:     "macdef name can be split across lines",
			netrc:    "machine cache.example.com login alice password secret macdef\ninit\nlogin wrong-user\npassword wrong-secret\n\ndefault login fallback password fallback-secret\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "secret",
		},
		{
			name:     "macdef name matching keyword is ignored",
			netrc:    "machine cache.example.com login alice password secret macdef login\npassword wrong-secret\n\ndefault login fallback password fallback-secret\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "secret",
		},
		{
			name:     "account value matching keyword is ignored",
			netrc:    "machine cache.example.com login alice account login password secret\n",
			machine:  "cache.example.com",
			login:    "alice",
			password: "secret",
		},
		{
			name:           "machine login match is selected when requested",
			netrc:          "machine cache.example.com login alice password secret-a\nmachine cache.example.com login bob password secret-b\n",
			machine:        "cache.example.com",
			requestedLogin: "bob",
			login:          "bob",
			password:       "secret-b",
		},
		{
			name:           "default entry must match requested login",
			netrc:          "default login anonymous password guest@example.com\n",
			machine:        "cache.example.com",
			requestedLogin: "alice",
			login:          "",
			password:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeNetrcFile(t, tt.netrc)

			login, password, err := findNetrcCredentials(path, tt.machine, tt.requestedLogin)
			if err != nil {
				t.Fatalf("findNetrcCredentials returned error: %v", err)
			}

			if login != tt.login {
				t.Fatalf("login = %q, want %q", login, tt.login)
			}
			if password != tt.password {
				t.Fatalf("password = %q, want %q", password, tt.password)
			}
		})
	}
}

func TestFindNetrcCredentialsReturnsEmptyWhenMissing(t *testing.T) {
	path := writeNetrcFile(t, "machine other.example.com login bob password other\n")

	login, password, err := findNetrcCredentials(path, "cache.example.com", "")
	if err != nil {
		t.Fatalf("findNetrcCredentials returned error: %v", err)
	}
	if login != "" || password != "" {
		t.Fatalf("got login=%q password=%q, want both empty", login, password)
	}
}
