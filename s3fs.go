// Package s3fs implements an absfs.Filer for S3-compatible object storage.
// It provides file operations on S3 buckets using the AWS SDK v2.
package s3fs

import (
	"context"
	"os"
	"path"
	"strings"
	"time"

	"github.com/absfs/absfs"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// FileSystem implements absfs.Filer for S3 object storage.
type FileSystem struct {
	client *s3.Client
	bucket string
	ctx    context.Context
}

// Config contains the configuration for connecting to S3.
type Config struct {
	Bucket string      // S3 bucket name
	Region string      // AWS region
	Config *aws.Config // Optional AWS config (if nil, uses default config loading)
}

// New creates a new S3 filesystem with the given configuration.
func New(cfg *Config) (*FileSystem, error) {
	ctx := context.Background()

	var awsConfig aws.Config
	var err error

	if cfg.Config != nil {
		awsConfig = *cfg.Config
	} else {
		// Load default AWS config
		awsConfig, err = config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
		if err != nil {
			return nil, err
		}
	}

	client := s3.NewFromConfig(awsConfig)

	return &FileSystem{
		client: client,
		bucket: cfg.Bucket,
		ctx:    ctx,
	}, nil
}

// OpenFile opens a file in S3.
// Note: S3 doesn't support traditional file flags, so this is a simplified implementation.
// Files opened with O_WRONLY, O_RDWR, or O_CREATE are opened in write mode and buffer
// data in memory until Close(). Files opened with O_RDONLY are opened in read mode and
// stream data from S3.
func (fs *FileSystem) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	name = strings.TrimPrefix(name, "/")

	// For write operations
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE) != 0 {
		return &File{
			fs:      fs,
			name:    name,
			key:     name,
			writing: true,
			buffer:  []byte{},
		}, nil
	}

	// For read operations, get the object
	return &File{
		fs:      fs,
		name:    name,
		key:     name,
		writing: false,
	}, nil
}

// Mkdir creates a "directory" in S3 (creates a zero-byte object with trailing slash).
// S3 doesn't have real directories, but this creates a marker object to represent one.
// The perm parameter is ignored as S3 doesn't support POSIX permissions.
func (fs *FileSystem) Mkdir(name string, perm os.FileMode) error {
	name = strings.TrimPrefix(name, "/")
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}

	_, err := fs.client.PutObject(fs.ctx, &s3.PutObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
		Body:   strings.NewReader(""),
	})
	if err != nil {
		return wrapError("Mkdir", name, err)
	}
	return nil
}

// Remove removes a file from S3.
// This deletes the S3 object with the given key.
func (fs *FileSystem) Remove(name string) error {
	name = strings.TrimPrefix(name, "/")

	_, err := fs.client.DeleteObject(fs.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		return wrapError("Remove", name, err)
	}
	return nil
}

// Rename renames (moves) a file in S3 by copying and deleting.
// Since S3 doesn't support atomic rename, this operation copies the object to the
// new location and then deletes the original. This is not atomic and may fail
// partway through.
func (fs *FileSystem) Rename(oldpath, newpath string) error {
	oldpath = strings.TrimPrefix(oldpath, "/")
	newpath = strings.TrimPrefix(newpath, "/")

	// Copy object to new location
	_, err := fs.client.CopyObject(fs.ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(fs.bucket),
		CopySource: aws.String(path.Join(fs.bucket, oldpath)),
		Key:        aws.String(newpath),
	})
	if err != nil {
		return wrapError("Rename", oldpath, err)
	}

	// Delete old object
	_, err = fs.client.DeleteObject(fs.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(oldpath),
	})
	if err != nil {
		return wrapError("Rename", oldpath, err)
	}
	return nil
}

// Stat returns file info for an S3 object.
// It uses HeadObject to retrieve metadata without downloading the object content.
func (fs *FileSystem) Stat(name string) (os.FileInfo, error) {
	name = strings.TrimPrefix(name, "/")

	output, err := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		return nil, wrapError("Stat", name, err)
	}

	return &fileInfo{
		name:    path.Base(name),
		size:    *output.ContentLength,
		modTime: *output.LastModified,
		isDir:   strings.HasSuffix(name, "/"),
	}, nil
}

// Chmod is not supported for S3.
// S3 doesn't have POSIX file permissions, so this always returns ErrNotImplemented.
func (fs *FileSystem) Chmod(name string, mode os.FileMode) error {
	return absfs.ErrNotImplemented
}

// Chtimes is not supported for S3.
// S3 object modification times are managed by the service and cannot be changed,
// so this always returns ErrNotImplemented.
func (fs *FileSystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return absfs.ErrNotImplemented
}

// Chown is not supported for S3.
// S3 doesn't have POSIX ownership, so this always returns ErrNotImplemented.
func (fs *FileSystem) Chown(name string, uid, gid int) error {
	return absfs.ErrNotImplemented
}

// fileInfo implements os.FileInfo for S3 objects.
type fileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return fi.size }
func (fi *fileInfo) Mode() os.FileMode  { return 0644 }
func (fi *fileInfo) ModTime() time.Time { return fi.modTime }
func (fi *fileInfo) IsDir() bool        { return fi.isDir }
func (fi *fileInfo) Sys() interface{}   { return nil }
