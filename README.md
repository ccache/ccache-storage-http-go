# ccache-storage-http-go

A [ccache remote storage helper](https://ccache.dev/storage-helpers.html) for
HTTP/HTTPS storage, written in **Go**.

## Overview

This is a storage helper for [ccache] that enables caching compilation results
on HTTP/HTTPS servers. It implements the [ccache remote storage helper
protocol].

This project aims to:

1. Provide a high-performance, production-ready HTTP(S) ccache storage helper.
2. Serve as an example implementation of a ccache storage helper in **Go**. Feel
   free to use it as a starting point for implementing helpers for other storage
   service protocols.

See also the similar [ccache-remote-http-cpp] project for an example (and
production ready) **C++** implementation.

[ccache]: https://ccache.dev
[ccache remote storage helper protocol]: https://github.com/jrosdahl/ccache/blob/crsh/doc/remote_storage_helper_spec.md
[ccache-remote-http-cpp]: https://github.com/ccache/ccache-storage-http-cpp

## Features

- Supports HTTP and HTTPS
- High-performance concurrent request handling
- HTTP keep-alive for efficient connection reuse
- Cross-platform: Linux, macOS, Windows
- Multiple layout modes: `flat`, `subdirs`, `bazel`
- Bearer token authentication support
- Support for custom HTTP headers
- Optional debug logging

## Installation

The helper should be installed in a [location where ccache searches for helper
programs]. Install it as the name `ccache-storage-http` for HTTP support and/or
`ccache-storage-https` for HTTPS support.

[location where ccache searches for helper programs]: https://github.com/jrosdahl/ccache/blob/crsh/doc/manual.adoc#storage-helper-process

### Using a prebuilt binary

Grab a prebuilt binary from
[Releases](https://github.com/ccache/ccache-storage-http-go/releases) and place
it in a suitable directory as described above. Rename `ccache-storage-http` to
`ccache-storage-https` (or copy or make a symlink) to support HTTPS.

### Building from source

```bash
# Clone the repository:
git clone https://github.com/ccache/ccache-storage-http-go
cd ccache-storage-http-go

# On Windows:
go mod download
go build -ldflags="-s -w" -trimpath -o ccache-storage-http.exe .

# On Linux/macOS and similar:
make

# Install ccache-storage-http and a ccache-storage-https symlink in /usr/local/bin:
make install

# Install ccache-storage-http and a ccache-storage-https symlink in /example/dir:
make install INSTALL_DIR=/example/dir
```

## Configuration

The helper is configured via ccache's [`remote_storage` configuration]. The
binary is automatically invoked by ccache when needed.

For example:

```bash
# Set the CCACHE_REMOTE_STORAGE environment variable:
export CCACHE_REMOTE_STORAGE="https://cache.example.com"

# Or set remote_storage in ccache's configuration file:
ccache -o remote_storage="https://cache.example.com"
```

[`remote_storage` configuration]: https://github.com/jrosdahl/ccache/blob/crsh/doc/manual.adoc#remote-storage-backends

See also the [HTTP storage wiki page] for tips on how to set up a storage server.

[HTTP storage wiki page]: https://github.com/ccache/ccache/wiki/HTTP-storage

### Configuration attributes

The helper supports the following custom attributes:

- `@bearer-token`: Bearer token for `Authorization` header
- `@header`: Custom HTTP headers (can be specified multiple times)
- `@layout`: Storage layout mode
  - `subdirs` (default): First 2 hex chars as subdirectory
  - `flat`: All files in root directory
  - `bazel`: Bazel Remote Execution API compatible layout

Example:

```bash
export CCACHE_REMOTE_STORAGE="https://cache.example.com @header=Content-Type=application/octet-stream"
```

## Optional debug logging

You can set the `CRSH_LOGFILE` environment variable to enable debug logging to a
file:

```bash
export CRSH_LOGFILE=/path/to/debug.log
```

Note: The helper process is spawned by ccache, so the environment variable must
be set before ccache is invoked.

## Contributing

Contributions are welcome! Please submit pull requests or open issues on GitHub.
