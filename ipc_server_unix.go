// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2026 Joel Rosdahl

//go:build !windows

package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

func (s *ipcServer) createListener() (net.Listener, error) {
	if err := os.Remove(s.config.IPCEndpoint); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove existing socket: %w", err)
	}

	oldMask := syscall.Umask(0077)
	defer syscall.Umask(oldMask)

	listener, err := net.Listen("unix", s.config.IPCEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on Unix socket: %w", err)
	}

	return listener, nil
}
