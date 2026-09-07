# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

### Added

- Prebuilt linux-s390x binary release.
- New `connection-pool-size` attribute for setting the maximum number of
  connections.

## [0.9] - 2026-08-30

[0.9]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.9

### Added

- New `bearer-token-file` attribute for reading the bearer token from a file
  for each request, so that the token can be rotated without restarting the
  helper.

### Changed

- Use ccache-go-storage-helper v0.1.0.

## [0.8] - 2026-07-18

[0.8]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.8

### Fixed

- Fix bazel key padding for short keys.
- Fix PUT error path race.
- Fix accept error busy loop.

## [0.7] - 2026-05-18

[0.7]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.7

### Added

- Support for info operation.
- Support for exists operation.

### Removed

- Support for experimental CRSH greeting message format 2.

## [0.6] - 2026-05-10

[0.6]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.6

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

[0.5]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.5

### Changed

- Change working directory to `/` (or `C:\` on Windows) to avoid blocking
  removal of the directory the server was started from.

## [0.4] - 2026-03-15

[0.4]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.4

### Changed

- Set `User-Agent` header to `ccache-storage-http-go/$VERSION` in HTTP requests.
- Improve generation of release notes.

## [0.3] - 2026-03-07

[0.3]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.3

### Changed

- Add `-go` suffix to release archive names to distinguish the project from the
  ccache-storage-http-cpp project.
- Move files inside release archives to a subdirectory named after the archive.

## [0.2] - 2026-03-05

[0.2]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.2

### Changed

- Switch license to MIT.
- Build prebuilt binaries with Go 1.26.0.

## [0.1] - 2026-01-18

[0.1]: https://github.com/ccache/ccache-storage-http-go/releases/tag/v0.1

### Added

- Implemented first version.
