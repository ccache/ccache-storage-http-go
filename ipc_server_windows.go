// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2026 Joel Rosdahl

//go:build windows

package main

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func (s *ipcServer) createListener() (net.Listener, error) {
	listener, err := winio.ListenPipe(s.config.IPCEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on named pipe: %w", err)
	}
	return listener, nil
}
