// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2026 Joel Rosdahl

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type ipcServer struct {
	config    *config
	logger    *logger
	storage   *storageClient
	listener  net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	idleTimer *time.Timer
	timerMu   sync.Mutex
}

func newServer(cfg *config, logger *logger) (*ipcServer, error) {
	storage, err := newStorageClient(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &ipcServer{
		config:  cfg,
		logger:  logger,
		storage: storage,
		ctx:     ctx,
		cancel:  cancel,
	}, nil
}

func (s *ipcServer) run() error {
	listener, err := s.createListener()
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	s.listener = listener
	defer s.listener.Close()

	s.logger.logf("Server listening on %s", s.config.IPCEndpoint)
	s.resetIdleTimer()
	go s.acceptLoop()
	<-s.ctx.Done() // wait for context cancellation
	s.logger.logf("Server shutting down")
	s.listener.Close()

	return nil
}

func (s *ipcServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				s.logger.logf("Accept error: %v", err)
				continue
			}
		}

		s.logger.logf("Client connected")
		s.resetIdleTimer()

		go s.handleConnection(conn)
	}
}

func (s *ipcServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	if err := writeGreeting(conn); err != nil {
		s.logger.logf("Failed to send greeting: %v", err)
		return
	}

	for {
		shouldStop, err := processRequest(conn, s.storage, s.logger)
		if err != nil {
			if err == io.EOF {
				s.logger.logf("Client disconnected")
			} else {
				s.logger.logf("Request processing error: %v", err)
			}
			return
		}

		if shouldStop {
			s.logger.logf("Stop requested, shutting down")
			s.cancel()
			return
		}

		s.resetIdleTimer()
	}
}

func (s *ipcServer) resetIdleTimer() {
	if s.config.IdleTimeout == 0 {
		return
	}

	s.timerMu.Lock()
	defer s.timerMu.Unlock()

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}

	s.idleTimer = time.AfterFunc(s.config.IdleTimeout, func() {
		s.logger.logf("Idle timeout reached, shutting down")
		s.cancel()
	})
}
