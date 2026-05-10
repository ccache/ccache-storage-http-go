# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.6] - 2026-05-10

### Added

- Support for [netrc] authentication.
- Prebuilt linux-riscv64 binary release.
- Support for CRSH greeting message format 2.
- Sending of config errors/warnings to ccache.
- Basic integration test suite.

[netrc]: https://everything.curl.dev/usingcurl/netrc.html

### Fixed

- Unnecessary serialization of storage client connections.
- Inefficient IPC I/O.
- Status code handling for HEAD requests.

### Changed

- Improve logging of failures.
- Improve connection pool size.
- Make transfer of GET and PUT payloads streaming, avoiding an extra data copy.
- Increase network buffers to 64 KiB.
- Remove server connection timeout (ccache handles connection timeout).

## [0.5] - 2026-03-18

### Changed

- Change working directory to `/` (or `C:\` on Windows) to avoid blocking
  removal of the directory the server was started from.

## [0.4] - 2026-03-15

### Changed

- Set `User-Agent` header to `ccache-storage-http-go/$VERSION` in HTTP requests.
- Improve generation of release notes.

## [0.3] - 2026-03-07

### Changed

- Add `-go` suffix to release archive names to distinguish the project from the
  ccache-storage-http-cpp project.
- Move files inside release archives to a subdirectory named after the archive.

## [0.2] - 2026-03-05

### Changed

- Switch license to MIT.
- Build prebuilt binaries with Go 1.26.0.

## [0.1] - 2026-01-18

### Added

- Implemented first version.
