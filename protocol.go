// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

type storage interface {
	exists(key []byte) (bool, error)
	get(key []byte) (io.ReadCloser, int64, bool, error)
	put(key []byte, value io.Reader, size int64, overwrite bool) (bool, error)
	remove(key []byte) (bool, error)
}

const (
	protocolVersion = 0x01

	capGetPutRemove = 0x00
	capInfo         = 0x01
	capExists       = 0x02

	requestGet    = 0x00
	requestPut    = 0x01
	requestRemove = 0x02
	requestStop   = 0x03
	requestInfo   = 0x04
	requestExists = 0x05

	responseOK   = 0x00
	responseNoop = 0x01
	responseErr  = 0x02

	putFlagOverwrite = 0x01
)

func writeGreeting(w io.Writer) error {
	caps := [...]byte{capGetPutRemove, capInfo, capExists}

	if err := writeByte(w, protocolVersion); err != nil {
		return err
	}
	if err := writeByte(w, uint8(len(caps))); err != nil {
		return err
	}
	if _, err := w.Write(caps[:]); err != nil {
		return err
	}

	return nil
}

func readRequest(r io.Reader) (byte, error) {
	return readByte(r)
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

func readValueLen(r io.Reader) (uint64, error) {
	return readUint64(r)
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
	if err := writeUint64(w, uint64(len(value))); err != nil {
		return err
	}
	_, err := w.Write(value)
	return err
}

func writeBool(w io.Writer, b bool) error {
	if b {
		return writeByte(w, 0x01)
	}
	return writeByte(w, 0x00)
}

func writeByte(w io.Writer, b byte) error {
	var buf [1]byte
	buf[0] = b
	_, err := w.Write(buf[:])
	return err
}

func readByte(r io.Reader) (byte, error) {
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func writeUint64(w io.Writer, value uint64) error {
	var buf [8]byte
	binary.NativeEndian.PutUint64(buf[:], value)
	_, err := w.Write(buf[:])
	return err
}

func readUint64(r io.Reader) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.NativeEndian.Uint64(buf[:]), nil
}

func writeMsg(w io.Writer, msg string) error {
	if len(msg) > 255 {
		msg = msg[:255]
	}
	if err := writeByte(w, uint8(len(msg))); err != nil {
		return err
	}
	_, err := io.WriteString(w, msg)
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

func handleExists(r io.Reader, w io.Writer, s storage, logger *logger) error {
	key, err := readKey(r)
	if err != nil {
		return err
	}

	logger.logf("EXISTS request for key %x", key)

	found, err := s.exists(key)
	if err != nil {
		logger.logf("EXISTS error: %v", err)
		return writeErr(w, err.Error())
	}

	logger.logf("EXISTS result: %v", found)
	if err := writeOK(w); err != nil {
		return err
	}
	return writeBool(w, found)
}

func handleGet(r io.Reader, w io.Writer, s storage, logger *logger) error {
	key, err := readKey(r)
	if err != nil {
		return err
	}

	logger.logf("GET request for key %x", key)

	valueBody, valueLen, found, err := s.get(key)
	if err != nil {
		logger.logf("GET error: %v", err)
		return writeErr(w, err.Error())
	}
	if valueBody != nil {
		defer valueBody.Close()
	}

	if !found {
		logger.logf("GET key not found")
		return writeNoop(w)
	}

	if valueLen >= 0 {
		logger.logf("GET success (%d bytes)", valueLen)
		if err := writeOK(w); err != nil {
			return err
		}
		if err := writeUint64(w, uint64(valueLen)); err != nil {
			return err
		}
		_, err = io.Copy(w, valueBody)
		return err
	}

	// Unknown value length from the server, so read all data to learn the length
	value, err := io.ReadAll(valueBody)
	if err != nil {
		return err
	}

	logger.logf("GET success (%d bytes)", len(value))
	if err := writeOK(w); err != nil {
		return err
	}
	return writeValue(w, value)
}

func handleInfo(w io.Writer, c *config, logger *logger) error {
	logger.logf("INFO request")

	if err := writeMsg(w, "ccache-storage-http-go "+version); err != nil {
		return err
	}
	diagnostics := c.Diagnostics
	if len(diagnostics) > 255 {
		diagnostics = diagnostics[:255]
	}
	if err := writeByte(w, uint8(len(diagnostics))); err != nil {
		return err
	}
	for _, diag := range diagnostics {
		if err := writeMsg(w, diag); err != nil {
			return err
		}
	}
	return nil
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

	valueLen, err := readValueLen(r)
	if err != nil {
		return err
	}
	if valueLen > math.MaxInt64 {
		return fmt.Errorf("value too large: %d", valueLen)
	}
	valueReader := io.LimitReader(r, int64(valueLen))

	overwrite := (flags & putFlagOverwrite) != 0
	logger.logf("PUT request for key %x (%d bytes)", key, valueLen)

	stored, err := s.put(key, valueReader, int64(valueLen), overwrite)
	_, drainErr := io.Copy(io.Discard, valueReader)
	if drainErr != nil {
		return drainErr
	}
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

func processRequest(r io.Reader, w io.Writer, s storage, logger *logger, c *config) (bool, error) {
	reqType, err := readRequest(r)
	if err != nil {
		return false, err
	}

	switch reqType {
	case requestExists:
		if err := handleExists(r, w, s, logger); err != nil {
			return false, err
		}
	case requestGet:
		if err := handleGet(r, w, s, logger); err != nil {
			return false, err
		}
	case requestInfo:
		if err := handleInfo(w, c, logger); err != nil {
			return false, err
		}
	case requestPut:
		if err := handlePut(r, w, s, logger); err != nil {
			return false, err
		}
	case requestRemove:
		if err := handleRemove(r, w, s, logger); err != nil {
			return false, err
		}
	case requestStop:
		if err := handleStop(w, logger); err != nil {
			return false, err
		}
		return true, nil // stop the server
	default:
		logger.logf("Unknown request type: 0x%02x", reqType)
		return false, writeErr(w, fmt.Sprintf("unknown request type: 0x%02x", reqType))
	}

	return false, nil
}
