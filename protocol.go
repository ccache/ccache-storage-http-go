// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2026 Joel Rosdahl

package main

import (
	"encoding/binary"
	"fmt"
	"io"
)

type storage interface {
	get(key []byte) ([]byte, bool, error)
	put(key []byte, value []byte, overwrite bool) (bool, error)
	remove(key []byte) (bool, error)
}

const (
	protocolVersion = 0x01
	cap0            = 0x00 // get/put/remove/stop operations

	requestGet    = 0x00
	requestPut    = 0x01
	requestRemove = 0x02
	requestStop   = 0x03

	responseOK   = 0x00
	responseNoop = 0x01
	responseErr  = 0x02

	putFlagOverwrite = 0x01
)

func writeGreeting(w io.Writer) error {
	if err := writeByte(w, protocolVersion); err != nil {
		return err
	}

	caps := []byte{cap0}
	if err := writeByte(w, uint8(len(caps))); err != nil {
		return err
	}
	for _, cap := range caps {
		if err := writeByte(w, cap); err != nil {
			return err
		}
	}

	return nil
}

func readRequest(r io.Reader) (byte, error) {
	reqType, err := readByte(r)
	if err != nil {
		return 0, err
	}
	return reqType, nil
}

func readKey(r io.Reader) ([]byte, error) {
	keyLen, err := readByte(r)
	if err != nil {
		return nil, err
	}

	key := make([]byte, keyLen)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}

	return key, nil
}

func readValue(r io.Reader) ([]byte, error) {
	var valueLen uint64
	if err := binary.Read(r, binary.NativeEndian, &valueLen); err != nil {
		return nil, err
	}

	value := make([]byte, valueLen)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, err
	}

	return value, nil
}

func writeOK(w io.Writer) error {
	return writeByte(w, responseOK)
}

func writeNoop(w io.Writer) error {
	return writeByte(w, responseNoop)
}

func writeErr(w io.Writer, msg string) error {
	if err := writeByte(w, responseErr); err != nil {
		return err
	}
	return writeMsg(w, msg)
}

func writeValue(w io.Writer, value []byte) error {
	valueLen := uint64(len(value))
	if err := binary.Write(w, binary.NativeEndian, valueLen); err != nil {
		return err
	}
	_, err := w.Write(value)
	return err
}

func writeByte(w io.Writer, b byte) error {
	_, err := w.Write([]byte{b})
	return err
}

func readByte(r io.Reader) (byte, error) {
	buf := make([]byte, 1)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func writeMsg(w io.Writer, msg string) error {
	if len(msg) > 255 {
		msg = msg[:255]
	}
	if err := writeByte(w, uint8(len(msg))); err != nil {
		return err
	}
	_, err := w.Write([]byte(msg))
	return err
}

func readMsg(r io.Reader) (string, error) {
	msgLen, err := readByte(r)
	if err != nil {
		return "", err
	}

	msg := make([]byte, msgLen)
	if _, err := io.ReadFull(r, msg); err != nil {
		return "", err
	}

	return string(msg), nil
}

func handleGet(r io.Reader, w io.Writer, s storage, logger *logger) error {
	key, err := readKey(r)
	if err != nil {
		return err
	}

	logger.logf("GET request for key %x", key)

	value, found, err := s.get(key)
	if err != nil {
		logger.logf("GET error: %v", err)
		return writeErr(w, err.Error())
	}

	if !found {
		logger.logf("GET key not found")
		return writeNoop(w)
	}

	logger.logf("GET success (%d bytes)", len(value))
	if err := writeOK(w); err != nil {
		return err
	}
	return writeValue(w, value)
}

func handlePut(r io.Reader, w io.Writer, s storage, logger *logger) error {
	key, err := readKey(r)
	if err != nil {
		return err
	}

	flags, err := readByte(r)
	if err != nil {
		return err
	}

	value, err := readValue(r)
	if err != nil {
		return err
	}

	overwrite := (flags & putFlagOverwrite) != 0
	logger.logf("PUT request for key %x (%d bytes)", key, len(value))

	stored, err := s.put(key, value, overwrite)
	if err != nil {
		logger.logf("PUT error: %v", err)
		return writeErr(w, err.Error())
	}

	if !stored {
		logger.logf("PUT not stored")
		return writeNoop(w)
	}

	logger.logf("PUT success")
	return writeOK(w)
}

func handleRemove(r io.Reader, w io.Writer, s storage, logger *logger) error {
	key, err := readKey(r)
	if err != nil {
		return err
	}

	logger.logf("REMOVE request for key %x", key)

	removed, err := s.remove(key)
	if err != nil {
		logger.logf("REMOVE error: %v", err)
		return writeErr(w, err.Error())
	}

	if !removed {
		logger.logf("REMOVE key not found")
		return writeNoop(w)
	}

	logger.logf("REMOVE success")
	return writeOK(w)
}

func handleStop(w io.Writer, logger *logger) error {
	logger.logf("STOP request received")
	return writeOK(w)
}

func processRequest(conn io.ReadWriter, s storage, logger *logger) (bool, error) {
	reqType, err := readRequest(conn)
	if err != nil {
		return false, err
	}

	switch reqType {
	case requestGet:
		if err := handleGet(conn, conn, s, logger); err != nil {
			return false, err
		}
	case requestPut:
		if err := handlePut(conn, conn, s, logger); err != nil {
			return false, err
		}
	case requestRemove:
		if err := handleRemove(conn, conn, s, logger); err != nil {
			return false, err
		}
	case requestStop:
		if err := handleStop(conn, logger); err != nil {
			return false, err
		}
		return true, nil // stop the server
	default:
		logger.logf("Unknown request type: 0x%02x", reqType)
		return false, writeErr(conn, fmt.Sprintf("unknown request type: 0x%02x", reqType))
	}

	return false, nil
}
