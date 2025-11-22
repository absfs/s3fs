# Contributing to S3FS

Thank you for your interest in contributing to S3FS! This document provides guidelines and instructions for contributing.

## Code of Conduct

This project adheres to a code of conduct. By participating, you are expected to uphold this code. Please be respectful and constructive in your interactions.

## How to Contribute

### Reporting Issues

- Check if the issue already exists before creating a new one
- Provide a clear title and description
- Include steps to reproduce the problem
- Mention your Go version and environment details
- Include relevant error messages or stack traces

### Suggesting Features

- Open an issue with a clear description of the proposed feature
- Explain the use case and why it would be valuable
- Be open to discussion and feedback

### Pull Requests

1. **Fork the repository** and create your branch from `main`
2. **Write clear commit messages** that explain what and why
3. **Add tests** for any new functionality
4. **Update documentation** including README, GoDoc comments, and examples
5. **Ensure all tests pass** by running `go test -v -race ./...`
6. **Format your code** with `gofmt` and check with `go vet`
7. **Submit your pull request** with a clear description

## Development Setup

### Prerequisites

- Go 1.21 or later
- AWS credentials configured (for integration tests)
- Git

### Getting Started

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/s3fs.git
cd s3fs

# Install dependencies
go mod download

# Run tests
go test -v ./...

# Run tests with race detection
go test -v -race ./...

# Run benchmarks
go test -bench=. -benchmem ./...

# Check formatting
gofmt -l .

# Run vet
go vet ./...
```

## Testing Guidelines

### Unit Tests

- Write tests for all new functions and methods
- Use table-driven tests where appropriate
- Test edge cases and error conditions
- Aim for high code coverage (>80%)

### Integration Tests

- Integration tests require AWS credentials
- Use environment variables for configuration
- Clean up resources after tests
- Skip integration tests if credentials are not available

### Benchmarks

- Add benchmarks for performance-critical code
- Use `b.ResetTimer()` to exclude setup time
- Include memory allocation benchmarks with `-benchmem`

## Code Style

### General Guidelines

- Follow standard Go conventions and idioms
- Use `gofmt` for formatting (enforced by CI)
- Use meaningful variable and function names
- Keep functions focused and concise
- Add comments for exported types and functions

### Error Handling

- Use custom error types for S3-specific errors
- Wrap errors with context using `wrapError()`
- Return errors rather than panicking
- Check all error returns

### Documentation

- Add GoDoc comments for all exported types and functions
- Include usage examples in `example_test.go`
- Update README.md for user-facing changes
- Keep comments up-to-date with code changes

## Commit Messages

Use clear, descriptive commit messages:

```
Add multipart upload support for large files

- Implement NewMultipartUpload method
- Add UploadPart and Complete methods
- Include tests and examples
- Update documentation

Fixes #123
```

## Release Process

1. Update CHANGELOG.md with changes
2. Update version in documentation
3. Create a git tag with semantic version
4. Push tag to trigger release workflow

## Questions?

- Open an issue for questions about contributing
- Check existing issues and pull requests
- Read the documentation and examples

Thank you for contributing to S3FS!
