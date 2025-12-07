// Package s3fs implements an absfs.Filer for S3-compatible object storage.
// It provides file operations on S3 buckets using the AWS SDK v2.
package s3fs

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"time"

	"github.com/absfs/absfs"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// FileSystem implements absfs.Filer for S3 object storage.
type FileSystem struct {
	client S3Client
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

// NewWithClient creates a new S3 filesystem with a custom S3 client.
// This is primarily useful for testing with mock clients.
func NewWithClient(client S3Client, bucket string) *FileSystem {
	return &FileSystem{
		client: client,
		bucket: bucket,
		ctx:    context.Background(),
	}
}

// NewWithClientAndContext creates a new S3 filesystem with a custom S3 client and context.
func NewWithClientAndContext(ctx context.Context, client S3Client, bucket string) *FileSystem {
	return &FileSystem{
		client: client,
		bucket: bucket,
		ctx:    ctx,
	}
}

// wrapError wraps S3 errors to be compatible with os.IsNotExist and os.IsExist.
func wrapError(op, path string, err error) error {
	if err == nil {
		return nil
	}

	// Check for NoSuchKey error
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return &os.PathError{Op: op, Path: path, Err: os.ErrNotExist}
	}

	// For other errors, wrap them in PathError
	return &os.PathError{Op: op, Path: path, Err: err}
}

// OpenFile opens a file in S3.
// Note: S3 doesn't support traditional file flags, so this is a simplified implementation.
func (fs *FileSystem) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	name = strings.TrimPrefix(name, "/")

	// Check if O_EXCL is set - file must not exist
	if flag&os.O_EXCL != 0 && flag&os.O_CREATE != 0 {
		_, err := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(fs.bucket),
			Key:    aws.String(name),
		})
		if err == nil {
			// File exists, return error
			return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrExist}
		}
		// File doesn't exist, which is what we want for O_EXCL
		var noSuchKey *types.NoSuchKey
		if !errors.As(err, &noSuchKey) {
			// Some other error occurred
			return nil, wrapError("open", name, err)
		}
	}

	// For write operations
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE) != 0 {
		// Check if trying to write to a directory
		// Directories in S3 are marked with trailing slash
		dirKey := name
		if !strings.HasSuffix(dirKey, "/") {
			dirKey += "/"
		}

		output, err := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(fs.bucket),
			Key:    aws.String(dirKey),
		})
		if err == nil {
			// Directory exists
			_ = output
			return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrInvalid}
		}

		return &File{
			fs:      fs,
			name:    name,
			key:     name,
			writing: true,
			buffer:  []byte{},
		}, nil
	}

	// For read operations, verify the file or directory exists
	_, err := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		// If not found, check if it's a directory (with trailing slash)
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			dirKey := name
			if !strings.HasSuffix(dirKey, "/") {
				dirKey += "/"
			}
			_, dirErr := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
				Bucket: aws.String(fs.bucket),
				Key:    aws.String(dirKey),
			})
			if dirErr != nil {
				// Neither file nor directory marker exists
				return nil, wrapError("open", name, err)
			}
			// Directory marker exists, open it for reading
			return &File{
				fs:      fs,
				name:    name,
				key:     name,
				writing: false,
			}, nil
		}
		return nil, wrapError("open", name, err)
	}

	return &File{
		fs:      fs,
		name:    name,
		key:     name,
		writing: false,
	}, nil
}

// Mkdir creates a "directory" in S3 (creates a zero-byte object with trailing slash).
func (fs *FileSystem) Mkdir(name string, perm os.FileMode) error {
	name = strings.TrimPrefix(name, "/")
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}

	// Check if directory already exists
	_, err := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	if err == nil {
		// Directory already exists
		return &os.PathError{Op: "mkdir", Path: name, Err: os.ErrExist}
	}

	// Only proceed if error is NoSuchKey
	var noSuchKey *types.NoSuchKey
	if !errors.As(err, &noSuchKey) {
		return wrapError("mkdir", name, err)
	}

	_, err = fs.client.PutObject(fs.ctx, &s3.PutObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
		Body:   strings.NewReader(""),
	})
	return wrapError("mkdir", name, err)
}

// Remove removes a file from S3.
// Note: S3's DeleteObject succeeds even if the object doesn't exist.
func (fs *FileSystem) Remove(name string) error {
	name = strings.TrimPrefix(name, "/")

	_, err := fs.client.DeleteObject(fs.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	return wrapError("remove", name, err)
}

// Rename renames (moves) a file in S3 by copying and deleting.
func (fs *FileSystem) Rename(oldpath, newpath string) error {
	oldpath = strings.TrimPrefix(oldpath, "/")
	newpath = strings.TrimPrefix(newpath, "/")

	// Copy object to new location
	// Issue #13 fix: CopySource must be in format /bucket/key (with leading slash)
	copySource := "/" + fs.bucket + "/" + oldpath
	_, err := fs.client.CopyObject(fs.ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(fs.bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(newpath),
	})
	if err != nil {
		return wrapError("rename", oldpath, err)
	}

	// Delete old object
	_, err = fs.client.DeleteObject(fs.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(oldpath),
	})
	return wrapError("rename", oldpath, err)
}

// Stat returns file info for an S3 object.
func (fs *FileSystem) Stat(name string) (os.FileInfo, error) {
	name = strings.TrimPrefix(name, "/")

	// Try the exact path first
	output, err := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	if err == nil {
		// Issue #12 fix: use safe nil-pointer handling with aws.ToInt64/aws.ToTime
		return &fileInfo{
			name:    path.Base(name),
			size:    aws.ToInt64(output.ContentLength),
			modTime: aws.ToTime(output.LastModified),
			isDir:   strings.HasSuffix(name, "/"),
		}, nil
	}

	// If not found and doesn't have trailing slash, try with trailing slash (directory)
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) && !strings.HasSuffix(name, "/") {
		dirKey := name + "/"
		output, dirErr := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(fs.bucket),
			Key:    aws.String(dirKey),
		})
		if dirErr == nil {
			return &fileInfo{
				name:    path.Base(name),
				size:    aws.ToInt64(output.ContentLength),
				modTime: aws.ToTime(output.LastModified),
				isDir:   true,
			}, nil
		}
	}

	return nil, wrapError("stat", name, err)
}

// Chmod is not supported for S3.
func (fs *FileSystem) Chmod(name string, mode os.FileMode) error {
	return absfs.ErrNotImplemented
}

// Chtimes is not supported for S3.
func (fs *FileSystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return absfs.ErrNotImplemented
}

// Chown is not supported for S3.
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
