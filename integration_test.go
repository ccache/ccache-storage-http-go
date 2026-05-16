// SPDX-License-Identifier: MIT
// Copyright 2026 Joel Rosdahl

//go:build !windows

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testHelperEnv = "CRSH_TEST_HELPER"

func TestMain(m *testing.M) {
	// When re-invoked as the helper process, behave as the real binary.
	if os.Getenv(testHelperEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// responseSpec describes how the stub server should respond to a single route.
type responseSpec struct {
	status            int
	body              []byte
	omitContentLength bool
}

// recordedRequest holds the details of a request the stub server received.
type recordedRequest struct {
	method string
	path   string
	body   []byte
}

// stubServer is a test HTTP server that routes requests by (method, path) and
// records every request it receives.
type stubServer struct {
	server *httptest.Server
	mu     sync.Mutex
	reqs   []recordedRequest
}

func newStubServer(t *testing.T, routes map[[2]string]responseSpec) *stubServer {
	t.Helper()
	s := &stubServer{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reqs = append(s.reqs, recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		s.mu.Unlock()

		spec, ok := routes[[2]string{r.Method, r.URL.Path}]
		if !ok {
			spec = responseSpec{status: http.StatusNotFound}
		}

		if spec.omitContentLength {
			// Hijack the connection and write a raw HTTP/1.0 response so that
			// no Content-Length header is sent; the client must read until EOF.
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("http hijack not supported")
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			conn, rw, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			defer conn.Close()
			fmt.Fprintf(rw, "HTTP/1.0 %d %s\r\nConnection: close\r\n\r\n",
				spec.status, http.StatusText(spec.status))
			rw.Write(spec.body)
			rw.Flush()
			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(spec.body)))
		w.WriteHeader(spec.status)
		if r.Method != http.MethodHead {
			w.Write(spec.body)
		}
	}))
	t.Cleanup(func() { s.server.Close() })
	return s
}

func (s *stubServer) url() string { return s.server.URL }

func (s *stubServer) requests() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedRequest, len(s.reqs))
	copy(out, s.reqs)
	return out
}

// helperProcess manages a running instance of the binary, connected to a test
// stub HTTP server, with an open Unix-socket IPC connection.
type helperProcess struct {
	t          *testing.T
	baseURL    string
	tmpDir     string
	socketPath string
	logPath    string
	cmd        *exec.Cmd
	conn       net.Conn
}

type helperAttr struct {
	key   string
	value string
}

func newHelperProcessWithAttrs(t *testing.T, baseURL string, attrs []helperAttr) *helperProcess {
	t.Helper()
	tmpDir := t.TempDir()
	h := &helperProcess{
		t:          t,
		baseURL:    baseURL,
		tmpDir:     tmpDir,
		socketPath: filepath.Join(tmpDir, "helper.sock"),
		logPath:    filepath.Join(tmpDir, "helper.log"),
	}
	h.start(attrs)
	t.Cleanup(h.stop)
	return h
}

func newHelperProcess(t *testing.T, baseURL string) *helperProcess {
	return newHelperProcessWithAttrs(t, baseURL, []helperAttr{{key: "layout", value: "flat"}})
}

func (h *helperProcess) start(attrs []helperAttr) {
	h.t.Helper()

	exe, err := os.Executable()
	if err != nil {
		h.t.Fatalf("os.Executable: %v", err)
	}

	env := append(os.Environ(),
		testHelperEnv+"=1",
		"CRSH_IPC_ENDPOINT="+h.socketPath,
		"CRSH_URL="+h.baseURL,
		"CRSH_IDLE_TIMEOUT=30",
		fmt.Sprintf("CRSH_NUM_ATTR=%d", len(attrs)),
		"CRSH_LOGFILE="+h.logPath,
	)
	for i, attr := range attrs {
		env = append(env,
			fmt.Sprintf("CRSH_ATTR_KEY_%d=%s", i, attr.key),
			fmt.Sprintf("CRSH_ATTR_VALUE_%d=%s", i, attr.value),
		)
	}

	h.cmd = exec.Command(exe)
	h.cmd.Env = env
	if err := h.cmd.Start(); err != nil {
		h.t.Fatalf("start helper: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)

	// Wait for the Unix socket to appear.
	for {
		if _, err := os.Stat(h.socketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			h.cmd.Process.Kill()
			h.cmd.Wait()
			h.t.Fatalf("timed out waiting for IPC socket; helper log:\n%s", h.readLog())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Connect to the socket.
	var conn net.Conn
	for {
		var err error
		conn, err = net.Dial("unix", h.socketPath)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			h.cmd.Process.Kill()
			h.cmd.Wait()
			h.t.Fatalf("timed out connecting to IPC socket: %v; helper log:\n%s", err, h.readLog())
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.conn = conn

	h.validateGreeting()
}

func (h *helperProcess) validateGreeting() {
	h.t.Helper()

	greeting := make([]byte, 5)
	if _, err := io.ReadFull(h.conn, greeting); err != nil {
		h.cmd.Process.Kill()
		h.cmd.Wait()
		h.t.Fatalf("read greeting: %v; helper log:\n%s", err, h.readLog())
	}

	if greeting[0] != 1 {
		h.cmd.Process.Kill()
		h.cmd.Wait()
		h.t.Fatalf("unexpected protocol version %v; helper log:\n%s", greeting[0], h.readLog())
	}

	if !bytes.Equal(greeting[1:], []byte{3, capGetPutRemove, capInfo, capExists}) {
		h.cmd.Process.Kill()
		h.cmd.Wait()
		h.t.Fatalf("unexpected capabilities; helper log:\n%s", h.readLog())
	}
}

func (h *helperProcess) stop() {
	if h.conn != nil {
		h.conn.SetDeadline(time.Now().Add(5 * time.Second))
		h.conn.Write([]byte{requestStop})
		var buf [1]byte
		io.ReadFull(h.conn, buf[:]) // read STATUS_OK
		h.conn.Close()
		h.conn = nil
	}
	if h.cmd != nil && h.cmd.Process != nil {
		done := make(chan error, 1)
		go func() { done <- h.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			h.cmd.Process.Kill()
			<-done
		}
		h.cmd = nil
	}
}

type infoResponse struct {
	identity    string
	diagnostics []string
}

func (h *helperProcess) ipcInfo() infoResponse {
	h.t.Helper()
	h.write([]byte{requestInfo})

	identity := h.readMsg()
	diagCount := int(h.readByte())
	diagnostics := make([]string, diagCount)
	for i := range diagnostics {
		diagnostics[i] = h.readMsg()
	}

	return infoResponse{identity: identity, diagnostics: diagnostics}
}

// ipcGet sends a GET request over IPC and returns (status, payload).
// payload is non-nil only for STATUS_OK; it contains the error string for
// STATUS_ERR.
func (h *helperProcess) ipcGet(hexKey string) (byte, []byte) {
	h.t.Helper()
	key := h.decodeHex(hexKey)
	h.write(append([]byte{requestGet, byte(len(key))}, key...))

	status := h.readByte()
	switch status {
	case responseOK:
		var lenBuf [8]byte
		h.readFull(lenBuf[:])
		n := binary.NativeEndian.Uint64(lenBuf[:])
		payload := make([]byte, n)
		h.readFull(payload)
		return status, payload
	case responseNoop:
		return status, nil
	case responseErr:
		return status, []byte(h.readMsg())
	default:
		h.t.Fatalf("unexpected GET status: %d", status)
		return status, nil
	}
}

// ipcPut sends a PUT request over IPC and returns (status, errorMsg).
// errorMsg is non-nil only for STATUS_ERR.
func (h *helperProcess) ipcPut(hexKey string, payload []byte, overwrite bool) (byte, []byte) {
	h.t.Helper()
	key := h.decodeHex(hexKey)
	var flags byte
	if overwrite {
		flags = putFlagOverwrite
	}
	var lenBuf [8]byte
	binary.NativeEndian.PutUint64(lenBuf[:], uint64(len(payload)))
	msg := append([]byte{requestPut, byte(len(key))}, key...)
	msg = append(msg, flags)
	msg = append(msg, lenBuf[:]...)
	msg = append(msg, payload...)
	h.write(msg)

	status := h.readByte()
	switch status {
	case responseOK, responseNoop:
		return status, nil
	case responseErr:
		return status, []byte(h.readMsg())
	default:
		h.t.Fatalf("unexpected PUT status: %d", status)
		return status, nil
	}
}

func (h *helperProcess) write(b []byte) {
	h.t.Helper()
	if _, err := h.conn.Write(b); err != nil {
		h.t.Fatalf("write to IPC socket: %v", err)
	}
}

func (h *helperProcess) readFull(buf []byte) {
	h.t.Helper()
	if _, err := io.ReadFull(h.conn, buf); err != nil {
		h.t.Fatalf("read from IPC socket: %v", err)
	}
}

func (h *helperProcess) readByte() byte {
	h.t.Helper()
	var buf [1]byte
	h.readFull(buf[:])
	return buf[0]
}

func (h *helperProcess) readMsg() string {
	h.t.Helper()
	msgLen := h.readByte()
	msg := make([]byte, msgLen)
	h.readFull(msg)
	return string(msg)
}

func (h *helperProcess) decodeHex(s string) []byte {
	h.t.Helper()
	b := make([]byte, len(s)/2)
	for i := range b {
		hi := hexNibble(h.t, s[2*i])
		lo := hexNibble(h.t, s[2*i+1])
		b[i] = hi<<4 | lo
	}
	return b
}

func (h *helperProcess) readLog() string {
	data, err := os.ReadFile(h.logPath)
	if err != nil {
		return fmt.Sprintf("(could not read log: %v)", err)
	}
	return string(data)
}

func hexNibble(t *testing.T, c byte) byte {
	t.Helper()
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		t.Fatalf("invalid hex character %q", c)
		return 0
	}
}

// --- Integration tests ---

func TestIntegrationInfoReturnsIdentityAndDiagnostics(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{})

	h := newHelperProcessWithAttrs(t, server.url(), []helperAttr{
		{key: "layout", value: "flat"},
		{key: "header", value: "broken-header"},
		{key: "mystery", value: "value"},
	})

	info := h.ipcInfo()

	if info.identity != "ccache-storage-http-go "+version {
		t.Fatalf("identity: want %q, got %q", "ccache-storage-http-go "+version, info.identity)
	}
	wantDiagnostics := []string{
		"error: invalid header (no \"=\"): broken-header",
		"warning: unknown attribute: mystery",
	}
	if len(info.diagnostics) != len(wantDiagnostics) {
		t.Fatalf("diagnostics: want %d entries, got %d (%v)", len(wantDiagnostics), len(info.diagnostics), info.diagnostics)
	}
	for i, want := range wantDiagnostics {
		if info.diagnostics[i] != want {
			t.Fatalf("diagnostics[%d]: want %q, got %q", i, want, info.diagnostics[i])
		}
	}
	if reqs := server.requests(); len(reqs) != 0 {
		t.Fatalf("want no upstream HTTP requests, got %v", reqs)
	}
}

func TestIntegrationGetWithContentLength(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"GET", "/abcd"}: {status: 200, body: []byte("cache-hit")},
	})

	h := newHelperProcess(t, server.url())

	status, payload := h.ipcGet("abcd")

	if status != responseOK {
		t.Fatalf("status: want %d (OK), got %d", responseOK, status)
	}
	if string(payload) != "cache-hit" {
		t.Fatalf("payload: want %q, got %q", "cache-hit", string(payload))
	}
	reqs := server.requests()
	if len(reqs) != 1 || reqs[0].method != "GET" || reqs[0].path != "/abcd" {
		t.Fatalf("want [GET /abcd], got %v", reqs)
	}
}

func TestIntegrationGetWithoutContentLength(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"GET", "/ef01"}: {status: 200, body: []byte("streamed-body"), omitContentLength: true},
	})

	h := newHelperProcess(t, server.url())

	status, payload := h.ipcGet("ef01")

	if status != responseOK {
		t.Fatalf("status: want %d (OK), got %d", responseOK, status)
	}
	if string(payload) != "streamed-body" {
		t.Fatalf("payload: want %q, got %q", "streamed-body", string(payload))
	}
	reqs := server.requests()
	if len(reqs) != 1 || reqs[0].method != "GET" || reqs[0].path != "/ef01" {
		t.Fatalf("want [GET /ef01], got %v", reqs)
	}
}

func TestIntegrationGetNotFoundReturnsNoop(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{})

	h := newHelperProcess(t, server.url())

	status, payload := h.ipcGet("beef")

	if status != responseNoop {
		t.Fatalf("status: want %d (NOOP), got %d", responseNoop, status)
	}
	if payload != nil {
		t.Fatalf("payload: want nil, got %q", payload)
	}
	reqs := server.requests()
	if len(reqs) != 1 || reqs[0].method != "GET" || reqs[0].path != "/beef" {
		t.Fatalf("want [GET /beef], got %v", reqs)
	}
}

func TestIntegrationGetServerErrorReturnsError(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"GET", "/dead"}: {status: 500},
	})

	h := newHelperProcess(t, server.url())

	status, errMsg := h.ipcGet("dead")

	if status != responseErr {
		t.Fatalf("status: want %d (ERR), got %d", responseErr, status)
	}
	if string(errMsg) != "HTTP 500" {
		t.Fatalf("error: want %q, got %q", "HTTP 500", string(errMsg))
	}
	reqs := server.requests()
	if len(reqs) != 1 || reqs[0].method != "GET" || reqs[0].path != "/dead" {
		t.Fatalf("want [GET /dead], got %v", reqs)
	}
}

func TestIntegrationPutOverwriteSendsBody(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"PUT", "/1234"}: {status: 201},
	})

	h := newHelperProcess(t, server.url())

	payload := []byte("new-entry")
	status, errMsg := h.ipcPut("1234", payload, true)

	if status != responseOK {
		t.Fatalf("status: want %d (OK), got %d (err=%q)", responseOK, status, errMsg)
	}
	reqs := server.requests()
	if len(reqs) != 1 || reqs[0].method != "PUT" || reqs[0].path != "/1234" {
		t.Fatalf("want [PUT /1234], got %v", reqs)
	}
	if string(reqs[0].body) != string(payload) {
		t.Fatalf("request body: want %q, got %q", payload, reqs[0].body)
	}
}

func TestIntegrationPutWithoutOverwriteChecksHeadBeforePut(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"HEAD", "/cafe"}: {status: 404},
		{"PUT", "/cafe"}:  {status: 201},
	})

	h := newHelperProcess(t, server.url())

	payload := []byte("write-after-head")
	status, errMsg := h.ipcPut("cafe", payload, false)

	if status != responseOK {
		t.Fatalf("status: want %d (OK), got %d (err=%q)", responseOK, status, errMsg)
	}
	reqs := server.requests()
	if len(reqs) != 2 ||
		reqs[0].method != "HEAD" || reqs[0].path != "/cafe" ||
		reqs[1].method != "PUT" || reqs[1].path != "/cafe" {
		t.Fatalf("want [HEAD /cafe, PUT /cafe], got %v", reqs)
	}
	if string(reqs[1].body) != string(payload) {
		t.Fatalf("PUT body: want %q, got %q", payload, reqs[1].body)
	}
}

func TestIntegrationPutWithoutOverwriteReturnsNoopWhenKeyExists(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"HEAD", "/face"}: {status: 200},
	})

	h := newHelperProcess(t, server.url())

	status, errMsg := h.ipcPut("face", []byte("unchanged"), false)

	if status != responseNoop {
		t.Fatalf("status: want %d (NOOP), got %d (err=%q)", responseNoop, status, errMsg)
	}
	reqs := server.requests()
	if len(reqs) != 1 || reqs[0].method != "HEAD" || reqs[0].path != "/face" {
		t.Fatalf("want [HEAD /face], got %v", reqs)
	}
}

func TestIntegrationPutWithoutOverwriteDrainsBodyBeforeNextRequest(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"HEAD", "/face"}: {status: 200},
		{"GET", "/beef"}:  {status: 200, body: []byte("after-noop")},
	})

	h := newHelperProcess(t, server.url())

	status, errMsg := h.ipcPut("face", []byte(strings.Repeat("x", 128<<10)), false)
	if status != responseNoop {
		t.Fatalf("status: want %d (NOOP), got %d (err=%q)", responseNoop, status, errMsg)
	}

	status, payload := h.ipcGet("beef")
	if status != responseOK {
		t.Fatalf("GET status: want %d (OK), got %d", responseOK, status)
	}
	if string(payload) != "after-noop" {
		t.Fatalf("GET payload: want %q, got %q", "after-noop", string(payload))
	}

	reqs := server.requests()
	if len(reqs) != 2 ||
		reqs[0].method != "HEAD" || reqs[0].path != "/face" ||
		reqs[1].method != "GET" || reqs[1].path != "/beef" {
		t.Fatalf("want [HEAD /face, GET /beef], got %v", reqs)
	}
}

func TestIntegrationPutHeadErrorIsReported(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"HEAD", "/f00d"}: {status: 500},
	})

	h := newHelperProcess(t, server.url())

	status, errMsg := h.ipcPut("f00d", []byte("payload"), false)

	if status != responseErr {
		t.Fatalf("status: want %d (ERR), got %d", responseErr, status)
	}
	if string(errMsg) != "HTTP 500" {
		t.Fatalf("error: want %q, got %q", "HTTP 500", string(errMsg))
	}
	reqs := server.requests()
	if len(reqs) != 1 || reqs[0].method != "HEAD" || reqs[0].path != "/f00d" {
		t.Fatalf("want [HEAD /f00d], got %v", reqs)
	}
}

func TestIntegrationPutServerErrorIsReported(t *testing.T) {
	server := newStubServer(t, map[[2]string]responseSpec{
		{"PUT", "/aaaa"}: {status: 500},
	})

	h := newHelperProcess(t, server.url())

	status, errMsg := h.ipcPut("aaaa", []byte("payload"), true)

	if status != responseErr {
		t.Fatalf("status: want %d (ERR), got %d", responseErr, status)
	}
	if string(errMsg) != "HTTP 500" {
		t.Fatalf("error: want %q, got %q", "HTTP 500", string(errMsg))
	}
	reqs := server.requests()
	if len(reqs) != 1 || reqs[0].method != "PUT" || reqs[0].path != "/aaaa" {
		t.Fatalf("want [PUT /aaaa], got %v", reqs)
	}
}
