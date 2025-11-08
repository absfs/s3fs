# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Comprehensive GoDoc comments for all exported types and functions
- Custom error types for better error handling (`S3Error`, `ErrNotExist`, etc.)
- Helper functions: `MkdirAll`, `RemoveAll`, `Exists`, `Walk`
- Context support via `WithContext()` method for cancellation and timeouts
- Multipart upload support for large files via `NewMultipartUpload()`
- Extensive unit tests for file operations
- Benchmark tests for performance baselines
- Example code in `example_test.go`
- CONTRIBUTING.md with contribution guidelines
- Error wrapping throughout the codebase for better debugging

### Fixed
- Critical bug in `ReadAt` method that incorrectly converted int64 to string
- Code formatting issues in test files
- Improved error handling with proper error wrapping and context

### Changed
- Enhanced error messages with operation context and file paths
- Improved documentation with detailed usage examples
- Better test coverage (now >80%)
- More robust error handling throughout

## [0.1.0] - Initial Release

### Added
- Initial implementation of `absfs.Filer` interface for S3
- Basic file operations: `OpenFile`, `Read`, `Write`, `Close`
- Directory operations: `Mkdir`, `Remove`, `Rename`
- File information via `Stat`
- Support for AWS SDK v2
- Basic README with usage examples
- MIT License
- GitHub Actions CI/CD workflow

### Limitations
- No POSIX permissions support (S3 limitation)
- Limited seek support (no `io.SeekEnd`)
- Non-atomic rename operation
- In-memory buffering for writes

[Unreleased]: https://github.com/absfs/s3fs/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/absfs/s3fs/releases/tag/v0.1.0
