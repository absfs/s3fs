# S3FS - S3 FileSystem

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/absfs/s3fs/blob/master/LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/absfs/s3fs.svg)](https://pkg.go.dev/github.com/absfs/s3fs)

The `s3fs` package implements an `absfs.Filer` for S3-compatible object storage. It provides file operations on S3 buckets using the AWS SDK v2.

## Features

- **S3-compatible storage**: Works with AWS S3 and compatible services
- **Standard interface**: Implements `absfs.Filer` for seamless integration
- **Full file operations**: Read, write, delete, rename files and directories
- **Helper functions**: `MkdirAll`, `RemoveAll`, `Exists`, `Walk`
- **Context support**: Cancellation and timeout control for all operations
- **Multipart uploads**: Efficient handling of large files (>5MB)
- **Error handling**: Custom error types with detailed context
- **Well documented**: Comprehensive GoDoc comments and examples
- **Production ready**: Extensive tests, benchmarks, and CI/CD

## Install

```bash
go get github.com/absfs/s3fs
```

## Example Usage

### Basic Usage

```go
package main

import (
    "log"
    "os"

    "github.com/absfs/s3fs"
)

func main() {
    // Create S3 filesystem
    fs, err := s3fs.New(&s3fs.Config{
        Bucket: "my-bucket",
        Region: "us-east-1",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Write a file
    f, _ := fs.OpenFile("path/to/file.txt", os.O_CREATE|os.O_WRONLY, 0644)
    f.Write([]byte("Hello, S3!"))
    f.Close()

    // Read a file
    f, _ = fs.OpenFile("path/to/file.txt", os.O_RDONLY, 0)
    defer f.Close()

    buf := make([]byte, 1024)
    n, _ := f.Read(buf)
    log.Printf("Read: %s", buf[:n])

    // Delete a file
    fs.Remove("path/to/file.txt")
}
```

### With Context and Timeout

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/absfs/s3fs"
)

func main() {
    // Create S3 filesystem
    fs, err := s3fs.New(&s3fs.Config{
        Bucket: "my-bucket",
        Region: "us-east-1",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Use with context timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    fsWithCtx := fs.WithContext(ctx)

    // All operations now respect the timeout
    f, _ := fsWithCtx.OpenFile("path/to/file.txt", os.O_RDONLY, 0)
    defer f.Close()
}
```

### Helper Functions

```go
package main

import (
    "log"
    "os"

    "github.com/absfs/s3fs"
)

func main() {
    fs, _ := s3fs.New(&s3fs.Config{
        Bucket: "my-bucket",
        Region: "us-east-1",
    })

    // Create nested directories
    fs.MkdirAll("path/to/nested/dir", 0755)

    // Check if file exists
    exists, _ := fs.Exists("path/to/file.txt")
    if exists {
        log.Println("File exists!")
    }

    // Walk directory tree
    fs.Walk("path/to/dir", func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        log.Printf("Found: %s (%d bytes)\n", path, info.Size())
        return nil
    })

    // Remove directory and all contents
    fs.RemoveAll("path/to/dir")
}
```

### Multipart Upload for Large Files

```go
package main

import (
    "log"

    "github.com/absfs/s3fs"
)

func main() {
    fs, _ := s3fs.New(&s3fs.Config{
        Bucket: "my-bucket",
        Region: "us-east-1",
    })

    // Create multipart upload
    upload, err := fs.NewMultipartUpload("path/to/large-file.bin")
    if err != nil {
        log.Fatal(err)
    }

    // Upload parts (minimum 5MB per part)
    part1 := make([]byte, 10*1024*1024) // 10MB
    if err := upload.UploadPart(part1); err != nil {
        upload.Abort()
        log.Fatal(err)
    }

    // Complete the upload
    if err := upload.Complete(); err != nil {
        log.Fatal(err)
    }
}
```

## API Reference

### FileSystem Methods

Core operations:
- `OpenFile(name, flag, perm)` - Open a file for reading or writing
- `Mkdir(name, perm)` - Create a directory
- `Remove(name)` - Remove a file
- `Rename(old, new)` - Rename/move a file
- `Stat(name)` - Get file information

Helper methods:
- `MkdirAll(name, perm)` - Create directory and parents
- `RemoveAll(name)` - Remove directory and contents
- `Exists(name)` - Check if file/directory exists
- `Walk(root, fn)` - Walk directory tree
- `WithContext(ctx)` - Create filesystem with custom context
- `NewMultipartUpload(key)` - Start multipart upload

### File Methods

- `Read(b)`, `ReadAt(b, off)` - Read from file
- `Write(b)`, `WriteAt(b, off)`, `WriteString(s)` - Write to file
- `Seek(offset, whence)` - Seek to position
- `Truncate(size)` - Change file size
- `Stat()` - Get file info
- `Readdir(n)`, `Readdirnames(n)` - List directory contents
- `Close()` - Close file and flush writes

## Limitations

- **Chmod, Chtimes, Chown**: Not supported (S3 doesn't have POSIX permissions)
- **Directories**: Represented as zero-byte objects with trailing slash
- **Seeking**: Limited support (`io.SeekEnd` not supported)
- **Atomic operations**: Rename requires copy+delete (not atomic)
- **Write buffering**: Writes are buffered in memory until Close()

## Authentication

The filesystem uses the AWS SDK's default credential chain, which checks:
1. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
2. Shared credentials file (~/.aws/credentials)
3. IAM role (when running on EC2, ECS, Lambda, etc.)

## Error Handling

S3FS provides custom error types for better error handling:

```go
// Check if file doesn't exist
_, err := fs.Stat("nonexistent.txt")
if errors.Is(err, s3fs.ErrNotExist) {
    log.Println("File doesn't exist")
}

// S3Error provides operation context
var s3Err *s3fs.S3Error
if errors.As(err, &s3Err) {
    log.Printf("Operation: %s, Path: %s, Error: %v", s3Err.Op, s3Err.Path, s3Err.Err)
}
```

## Testing

Run tests:
```bash
go test -v ./...
go test -race ./...
go test -bench=. -benchmem ./...
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## absfs

Check out the [`absfs`](https://github.com/absfs/absfs) repo for more information about the abstract filesystem interface and features like filesystem composition.

## LICENSE

This project is governed by the MIT License. See [LICENSE](https://github.com/absfs/s3fs/blob/master/LICENSE)
