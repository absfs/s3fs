# S3FS - S3 FileSystem

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/absfs/s3fs/blob/master/LICENSE)

The `s3fs` package implements an `absfs.Filer` for S3-compatible object storage. It provides file operations on S3 buckets using the AWS SDK v2.

## Features

- **S3-compatible storage**: Works with AWS S3 and compatible services
- **Standard interface**: Implements `absfs.Filer` for seamless integration
- **Basic operations**: Read, write, delete files in S3 buckets

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

### With Custom AWS Config

```go
package main

import (
    "context"
    "log"

    "github.com/absfs/s3fs"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials"
)

func main() {
    // Load custom AWS config
    cfg, err := config.LoadDefaultConfig(context.TODO(),
        config.WithRegion("us-west-2"),
        config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
            "access-key",
            "secret-key",
            "",
        )),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create S3 filesystem with custom config
    fs, err := s3fs.New(&s3fs.Config{
        Bucket: "my-bucket",
        Config: &cfg,
    })
    if err != nil {
        log.Fatal(err)
    }

    // Use filesystem
}
```

## Limitations

- **Chmod, Chtimes, Chown**: Not supported (S3 doesn't have POSIX permissions)
- **Directories**: Represented as zero-byte objects with trailing slash
- **Seeking**: Limited support (S3 is object storage, not a traditional filesystem)
- **Atomic operations**: Some operations (like Rename) require copy+delete

## Authentication

The filesystem uses the AWS SDK's default credential chain, which checks:
1. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
2. Shared credentials file (~/.aws/credentials)
3. IAM role (when running on EC2, ECS, Lambda, etc.)

## absfs

Check out the [`absfs`](https://github.com/absfs/absfs) repo for more information about the abstract filesystem interface and features like filesystem composition.

## LICENSE

This project is governed by the MIT License. See [LICENSE](https://github.com/absfs/s3fs/blob/master/LICENSE)
