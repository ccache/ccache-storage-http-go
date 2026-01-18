// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2026 Joel Rosdahl

package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type logger struct {
	mu   sync.Mutex
	file *os.File
}

func newLogger(logFile string) *logger {
	l := &logger{}
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err == nil {
			l.file = f
		}
	}
	return l
}

func (l *logger) logf(format string, args ...interface{}) {
	if l.file == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.file, "[%s] %s\n", timestamp, msg)
}

func (l *logger) close() {
	if l.file != nil {
		l.file.Close()
	}
}
