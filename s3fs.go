// Package s3fs implements an [absfs.Filer] backed by S3-compatible object storage.
//
// Directories are represented as zero-byte objects with a trailing slash.
// Write operations are buffered in memory and uploaded to S3 on [File.Close].
// The FileSystem is safe for concurrent use; individual [File] instances are
// also safe for concurrent use as of v1.1.
//
// S3 does not support POSIX permissions, ownership, or timestamps, so
// [FileSystem.Chmod], [FileSystem.Chtimes], and [FileSystem.Chown] return
// [absfs.ErrNotImplemented].
package s3fs

import (
	"bytes"
	"context"
	"errors"
	"io"
	iosfs "io/fs"
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

// sanitizePath cleans and validates a path for S3 operations.
// It normalizes "." and ".." components and rejects paths that would
// escape above the root (e.g., "../secret").
func sanitizePath(p string) (string, error) {
	// Normalize: add leading slash so path.Clean handles ".." correctly
	clean := path.Clean("/" + p)
	// Remove the leading slash we added
	clean = strings.TrimPrefix(clean, "/")

	// path.Clean("/" + "../x") yields "/x" which is fine,
	// but path.Clean("/" + "../../x") also yields "/x".
	// The real danger is "..": path.Clean("/..") → "/"
	// We detect traversal by checking if the cleaned path would have
	// gone above root. After cleaning with a leading slash, if the result
	// is "/" or starts with "..", it's traversal.
	if clean == "." {
		clean = ""
	}
	if strings.HasPrefix(clean, "..") {
		return "", &os.PathError{Op: "sanitize", Path: p, Err: os.ErrInvalid}
	}

	return clean, nil
}

// WithContext returns a copy of the FileSystem that uses the given context
// for all S3 operations.
func (fs *FileSystem) WithContext(ctx context.Context) *FileSystem {
	return &FileSystem{
		client: fs.client,
		bucket: fs.bucket,
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

// OpenFile opens the named file in S3.
//
// Supported flags: O_RDONLY, O_WRONLY, O_RDWR, O_CREATE, O_EXCL, O_TRUNC, O_APPEND.
// When O_APPEND is set, existing content is loaded and new writes append to it.
// When O_TRUNC is set (or O_CREATE for a new file), the file starts empty.
// Paths are sanitized to prevent traversal attacks.
func (fs *FileSystem) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	var err error
	name, err = sanitizePath(name)
	if err != nil {
		return nil, err
	}

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
		dirKey := name
		if !strings.HasSuffix(dirKey, "/") {
			dirKey += "/"
		}

		output, err := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(fs.bucket),
			Key:    aws.String(dirKey),
		})
		if err == nil {
			_ = output
			return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrInvalid}
		}

		var buf []byte

		// Load existing content unless O_TRUNC is set
		if flag&os.O_TRUNC == 0 {
			getOutput, getErr := fs.client.GetObject(fs.ctx, &s3.GetObjectInput{
				Bucket: aws.String(fs.bucket),
				Key:    aws.String(name),
			})
			if getErr == nil {
				buf, _ = io.ReadAll(getOutput.Body)
				getOutput.Body.Close()
			}
			// If file doesn't exist, start with empty buffer
		}

		if buf == nil {
			buf = []byte{}
		}

		f := &File{
			fs:      fs,
			name:    name,
			key:     name,
			writing: true,
			buffer:  buf,
		}

		if flag&os.O_APPEND != 0 {
			f.append = true
			f.offset = int64(len(buf))
		}

		return f, nil
	}

	// For read operations, verify the file or directory exists
	_, err = fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
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

// Mkdir creates a directory in S3 by writing a zero-byte object with a trailing slash.
// Returns os.ErrExist if the directory already exists.
func (fs *FileSystem) Mkdir(name string, perm os.FileMode) error {
	var err error
	name, err = sanitizePath(name)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}

	// Check if directory already exists
	_, err = fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
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

// Remove removes a file or directory from S3.
// Returns os.ErrNotExist if neither the file nor a directory marker exists.
// For directories, it removes the directory marker (key with trailing slash).
func (fs *FileSystem) Remove(name string) error {
	var err error
	name, err = sanitizePath(name)
	if err != nil {
		return err
	}

	// Check if the object exists as a file
	_, headErr := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})

	// If not a file, check as directory marker
	dirKey := name
	if !strings.HasSuffix(dirKey, "/") {
		dirKey += "/"
	}
	isDirMarker := false
	if headErr != nil {
		_, dirErr := fs.client.HeadObject(fs.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(fs.bucket),
			Key:    aws.String(dirKey),
		})
		if dirErr != nil {
			// Neither file nor directory exists
			return &os.PathError{Op: "remove", Path: name, Err: os.ErrNotExist}
		}
		isDirMarker = true
	}

	if isDirMarker {
		_, err = fs.client.DeleteObject(fs.ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(fs.bucket),
			Key:    aws.String(dirKey),
		})
		return wrapError("remove", name, err)
	}

	// Delete the file
	_, err = fs.client.DeleteObject(fs.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		return wrapError("remove", name, err)
	}

	// Also clean up directory marker if present
	if !strings.HasSuffix(name, "/") {
		_, _ = fs.client.DeleteObject(fs.ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(fs.bucket),
			Key:    aws.String(dirKey),
		})
	}

	return nil
}

// Rename moves a file in S3 by copying to the new key and deleting the old one.
// This is not atomic. If the delete fails after a successful copy, the copy is
// rolled back (best-effort).
func (fs *FileSystem) Rename(oldpath, newpath string) error {
	var err error
	oldpath, err = sanitizePath(oldpath)
	if err != nil {
		return err
	}
	newpath, err = sanitizePath(newpath)
	if err != nil {
		return err
	}

	// Copy object to new location
	// Issue #13 fix: CopySource must be in format /bucket/key (with leading slash)
	copySource := "/" + fs.bucket + "/" + oldpath
	_, err = fs.client.CopyObject(fs.ctx, &s3.CopyObjectInput{
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
	if err != nil {
		// Rollback: delete the copy we just made
		_, _ = fs.client.DeleteObject(fs.ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(fs.bucket),
			Key:    aws.String(newpath),
		})
		return wrapError("rename", oldpath, err)
	}

	return nil
}

// Stat returns file info for an S3 object.
func (fs *FileSystem) Stat(name string) (os.FileInfo, error) {
	var err error
	name, err = sanitizePath(name)
	if err != nil {
		return nil, err
	}

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

// Truncate truncates a file to the specified size.
// For S3, this requires reading the file, truncating the data, and writing it back.
func (fs *FileSystem) Truncate(name string, size int64) error {
	var err error
	name, err = sanitizePath(name)
	if err != nil {
		return err
	}

	// Read the existing file content
	output, err := fs.client.GetObject(fs.ctx, &s3.GetObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		return wrapError("truncate", name, err)
	}
	defer output.Body.Close()

	// Read the content
	content, err := io.ReadAll(output.Body)
	if err != nil {
		return wrapError("truncate", name, err)
	}

	// Truncate the content
	var truncated []byte
	if size < int64(len(content)) {
		truncated = content[:size]
	} else {
		// Extend with zeros if size is larger
		truncated = make([]byte, size)
		copy(truncated, content)
	}

	// Write back the truncated content
	_, err = fs.client.PutObject(fs.ctx, &s3.PutObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
		Body:   bytes.NewReader(truncated),
	})
	return wrapError("truncate", name, err)
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

// Type implements iosfs.DirEntry.
func (fi *fileInfo) Type() iosfs.FileMode {
	if fi.isDir {
		return iosfs.ModeDir
	}
	return 0
}

// Info implements iosfs.DirEntry.
func (fi *fileInfo) Info() (iosfs.FileInfo, error) {
	return fi, nil
}

// ReadDir reads the named directory and returns a list of directory entries sorted by filename.
func (fs *FileSystem) ReadDir(name string) ([]iosfs.DirEntry, error) {
	var err error
	name, err = sanitizePath(name)
	if err != nil {
		return nil, err
	}

	// For root or empty, list with empty prefix
	prefix := name
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var entries []iosfs.DirEntry
	seen := make(map[string]bool)

	var continuationToken *string
	for {
		output, err := fs.client.ListObjectsV2(fs.ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(fs.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, wrapError("readdir", name, err)
		}

		for _, obj := range output.Contents {
			key := aws.ToString(obj.Key)
			if key == prefix {
				continue
			}

			relName := strings.TrimPrefix(key, prefix)
			slashIdx := strings.Index(relName, "/")

			var displayName string
			var isDir bool

			if slashIdx == -1 {
				displayName = relName
				isDir = false
			} else if slashIdx == len(relName)-1 {
				displayName = relName[:slashIdx]
				isDir = true
			} else {
				displayName = relName[:slashIdx]
				isDir = true
			}

			if seen[displayName] {
				continue
			}
			seen[displayName] = true

			entries = append(entries, &fileInfo{
				name:    displayName,
				size:    aws.ToInt64(obj.Size),
				modTime: aws.ToTime(obj.LastModified),
				isDir:   isDir,
			})
		}

		if !aws.ToBool(output.IsTruncated) {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	return entries, nil
}

// ReadFile reads the named file and returns its contents.
func (fs *FileSystem) ReadFile(name string) ([]byte, error) {
	var err error
	name, err = sanitizePath(name)
	if err != nil {
		return nil, err
	}

	output, err := fs.client.GetObject(fs.ctx, &s3.GetObjectInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		return nil, wrapError("readfile", name, err)
	}
	defer output.Body.Close()

	return io.ReadAll(output.Body)
}

// Sub returns an fs.FS corresponding to the subtree rooted at dir.
func (fs *FileSystem) Sub(dir string) (iosfs.FS, error) {
	dir = strings.TrimPrefix(dir, "/")
	return absfs.FilerToFS(fs, dir)
}

// subFS wraps a FileSystem to operate within a subdirectory.
type subFS struct {
	parent *FileSystem
	prefix string
}

func (s *subFS) fullPath(name string) string {
	name = strings.TrimPrefix(name, "/")
	if s.prefix == "" {
		return name
	}
	if name == "" {
		return s.prefix
	}
	return path.Join(s.prefix, name)
}

func (s *subFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	return s.parent.OpenFile(s.fullPath(name), flag, perm)
}

func (s *subFS) Mkdir(name string, perm os.FileMode) error {
	return s.parent.Mkdir(s.fullPath(name), perm)
}

func (s *subFS) Remove(name string) error {
	return s.parent.Remove(s.fullPath(name))
}

func (s *subFS) Rename(oldpath, newpath string) error {
	return s.parent.Rename(s.fullPath(oldpath), s.fullPath(newpath))
}

func (s *subFS) Stat(name string) (os.FileInfo, error) {
	return s.parent.Stat(s.fullPath(name))
}

func (s *subFS) Chmod(name string, mode os.FileMode) error {
	return s.parent.Chmod(s.fullPath(name), mode)
}

func (s *subFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return s.parent.Chtimes(s.fullPath(name), atime, mtime)
}

func (s *subFS) Chown(name string, uid, gid int) error {
	return s.parent.Chown(s.fullPath(name), uid, gid)
}

func (s *subFS) ReadDir(name string) ([]iosfs.DirEntry, error) {
	return s.parent.ReadDir(s.fullPath(name))
}

func (s *subFS) ReadFile(name string) ([]byte, error) {
	return s.parent.ReadFile(s.fullPath(name))
}

func (s *subFS) Sub(dir string) (iosfs.FS, error) {
	fullDir := s.fullPath(dir)
	return absfs.FilerToFS(s, fullDir)
}
