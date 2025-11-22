package s3fs

import (
	"errors"
	"fmt"
)

// Common errors returned by s3fs operations.
var (
	// ErrNotExist is returned when a file or directory does not exist.
	ErrNotExist = errors.New("s3fs: file does not exist")

	// ErrInvalidSeek is returned when an invalid seek operation is attempted.
	ErrInvalidSeek = errors.New("s3fs: invalid seek operation")

	// ErrWriteOnReadFile is returned when attempting to write to a read-only file.
	ErrWriteOnReadFile = errors.New("s3fs: cannot write to read-only file")

	// ErrReadOnWriteFile is returned when attempting to read from a write-only file.
	ErrReadOnWriteFile = errors.New("s3fs: cannot read from write-only file")
)

// S3Error wraps S3 operation errors with additional context.
type S3Error struct {
	Op   string // Operation that failed (e.g., "GetObject", "PutObject")
	Path string // Path of the file involved
	Err  error  // Underlying error
}

// Error implements the error interface.
func (e *S3Error) Error() string {
	return fmt.Sprintf("s3fs: %s %s: %v", e.Op, e.Path, e.Err)
}

// Unwrap returns the underlying error.
func (e *S3Error) Unwrap() error {
	return e.Err
}

// wrapError wraps an error with S3Error context.
func wrapError(op, path string, err error) error {
	if err == nil {
		return nil
	}
	return &S3Error{
		Op:   op,
		Path: path,
		Err:  err,
	}
}
