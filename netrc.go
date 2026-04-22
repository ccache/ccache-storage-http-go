// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func defaultNetrcPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	if runtime.GOOS != "windows" {
		return filepath.Join(home, ".netrc")
	}

	dotNetrc := filepath.Join(home, ".netrc")
	if _, err := os.Stat(dotNetrc); err == nil {
		return dotNetrc
	}

	underscoreNetrc := filepath.Join(home, "_netrc")
	if _, err := os.Stat(underscoreNetrc); err == nil {
		return underscoreNetrc
	}

	return dotNetrc
}

type netrcKeyword int

const (
	netrcKeywordNone netrcKeyword = iota
	netrcKeywordMachine
	netrcKeywordLogin
	netrcKeywordPassword
	netrcKeywordAccount
	netrcKeywordMacdef
)

func tokenizeNetrcLine(line string) ([]string, error) {
	tokens := make([]string, 0, 8)

	for i := 0; i < len(line); {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r') {
			i++
		}
		if i >= len(line) {
			break
		}

		if line[i] != '"' {
			start := i
			for i < len(line) && line[i] > ' ' {
				i++
			}
			tokens = append(tokens, line[start:i])
			continue
		}

		i++
		var token strings.Builder
		escape := false
		closed := false
		for i < len(line) {
			ch := line[i]
			i++

			if escape {
				switch ch {
				case 'n':
					ch = '\n'
				case 'r':
					ch = '\r'
				case 't':
					ch = '\t'
				}
				token.WriteByte(ch)
				escape = false
				continue
			}

			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				closed = true
				break
			}

			token.WriteByte(ch)
		}

		if !closed {
			return nil, fmt.Errorf("unterminated quoted string")
		}

		tokens = append(tokens, token.String())
	}

	return tokens, nil
}

// findNetrcCredentials looks up login and password for the given machine in the
// netrc file at netrcPath. If requestedLogin is non-empty, only entries with a
// matching login are considered. Returns empty strings if no matching entry is
// found.
func findNetrcCredentials(netrcPath, machine, requestedLogin string) (login, password string, err error) {
	f, err := os.Open(netrcPath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 128*1024)

	matchedMachine := false
	defaultEntry := false
	pendingKeyword := netrcKeywordNone
	defaultLogin := ""
	defaultPassword := ""
	loginMatches := func(login string) bool {
		return requestedLogin == "" || login == requestedLogin
	}

	finalizeCurrentEntry := func() (string, string, bool) {
		if login == "" || !loginMatches(login) {
			return "", "", false
		}
		if matchedMachine {
			return login, password, true
		}
		if defaultEntry {
			defaultLogin = login
			defaultPassword = password
		}

		return "", "", false
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t\r")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		tokens, err := tokenizeNetrcLine(line)
		if err != nil {
			return "", "", fmt.Errorf("invalid netrc syntax: %w", err)
		}

	tokenLoop:
		for _, tok := range tokens {
			switch pendingKeyword {
			case netrcKeywordMachine:
				matchedMachine = tok == machine
				defaultEntry = false
				login, password = "", ""
				pendingKeyword = netrcKeywordNone
				continue

			case netrcKeywordLogin:
				if matchedMachine || defaultEntry {
					login = tok
				}
				pendingKeyword = netrcKeywordNone
				continue

			case netrcKeywordPassword:
				if matchedMachine || defaultEntry {
					password = tok
				}
				pendingKeyword = netrcKeywordNone
				continue

			case netrcKeywordAccount:
				pendingKeyword = netrcKeywordNone
				continue

			case netrcKeywordMacdef:
				pendingKeyword = netrcKeywordNone
				for scanner.Scan() {
					if strings.TrimSpace(scanner.Text()) == "" {
						break
					}
				}
				break tokenLoop
			}

			switch tok {
			case "machine":
				if l, p, ok := finalizeCurrentEntry(); ok {
					return l, p, nil
				}
				matchedMachine = false
				defaultEntry = false
				pendingKeyword = netrcKeywordMachine

			case "default":
				if l, p, ok := finalizeCurrentEntry(); ok {
					return l, p, nil
				}
				matchedMachine = false
				defaultEntry = true
				login, password = "", ""

			case "login":
				pendingKeyword = netrcKeywordLogin

			case "password":
				pendingKeyword = netrcKeywordPassword

			case "account":
				pendingKeyword = netrcKeywordAccount

			case "macdef":
				pendingKeyword = netrcKeywordMacdef
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", err
	}

	if l, p, ok := finalizeCurrentEntry(); ok {
		return l, p, nil
	}
	if defaultLogin != "" {
		return defaultLogin, defaultPassword, nil
	}
	return "", "", nil
}
