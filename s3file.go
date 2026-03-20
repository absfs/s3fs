package s3fs

import (
	"bytes"
	"fmt"
	"io"
	iosfs "io/fs"
	"os"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// File represents a file in S3.
// File instances are safe for concurrent use; all mutable state is
// protected by an internal mutex.
type File struct {
	mu      sync.Mutex
	fs      *FileSystem
	name    string
	key     string
	writing bool
	append  bool
	buffer  []byte
	offset  int64
	body    io.ReadCloser
}

// Name returns the name of the file.
func (f *File) Name() string {
	return f.name
}

// Read reads from the S3 object.
// On first call, the object is fetched from S3 and cached for subsequent reads.
func (f *File) Read(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.writing {
		return 0, os.ErrInvalid
	}

	// Lazy load the object body
	if f.body == nil {
		output, err := f.fs.client.GetObject(f.fs.ctx, &s3.GetObjectInput{
			Bucket: aws.String(f.fs.bucket),
			Key:    aws.String(f.key),
		})
		if err != nil {
			return 0, wrapError("read", f.name, err)
		}
		f.body = output.Body
	}

	return f.body.Read(b)
}

// ReadAt reads from the S3 object at a specific offset using S3 range reads.
// Each call issues a new GetObject request with the appropriate byte range.
func (f *File) ReadAt(b []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.writing {
		return 0, os.ErrInvalid
	}

	rangeStr := fmt.Sprintf("bytes=%d-%d", off, off+int64(len(b))-1)
	output, err := f.fs.client.GetObject(f.fs.ctx, &s3.GetObjectInput{
		Bucket: aws.String(f.fs.bucket),
		Key:    aws.String(f.key),
		Range:  aws.String(rangeStr),
	})
	if err != nil {
		return 0, wrapError("read", f.name, err)
	}
	defer output.Body.Close()

	return io.ReadFull(output.Body, b)
}

// Write writes to the file buffer. The buffer is uploaded to S3 on Close.
// In append mode, writes always go to the end of the buffer regardless of
// the current offset.
func (f *File) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.writing {
		return 0, os.ErrInvalid
	}

	f.buffer = append(f.buffer, b...)
	f.offset = int64(len(f.buffer))
	return len(b), nil
}

// WriteAt writes to the buffer at a specific offset.
func (f *File) WriteAt(b []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.writing {
		return 0, os.ErrInvalid
	}

	// Extend buffer if necessary
	if int(off)+len(b) > len(f.buffer) {
		newBuf := make([]byte, int(off)+len(b))
		copy(newBuf, f.buffer)
		f.buffer = newBuf
	}

	copy(f.buffer[off:], b)
	return len(b), nil
}

// WriteString writes a string to the file.
func (f *File) WriteString(s string) (int, error) {
	// Write handles its own locking
	return f.Write([]byte(s))
}

// Close closes the file. For files opened for writing, the buffered content
// is uploaded to S3.
func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.body != nil {
		f.body.Close()
	}

	if f.writing {
		_, err := f.fs.client.PutObject(f.fs.ctx, &s3.PutObjectInput{
			Bucket: aws.String(f.fs.bucket),
			Key:    aws.String(f.key),
			Body:   bytes.NewReader(f.buffer),
		})
		return wrapError("close", f.name, err)
	}

	return nil
}

// Seek sets the offset for the next read or write.
// SeekEnd is not supported for S3 files.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch whence {
	case io.SeekStart:
		f.offset = offset
	case io.SeekCurrent:
		f.offset += offset
	case io.SeekEnd:
		return 0, os.ErrInvalid
	}
	return f.offset, nil
}

// Stat returns file info.
func (f *File) Stat() (os.FileInfo, error) {
	return f.fs.Stat(f.name)
}

// Sync is a no-op for S3 (writes are synchronous).
func (f *File) Sync() error {
	return nil
}

// Truncate changes the size of the file buffer. It does not change the I/O offset.
func (f *File) Truncate(size int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.writing {
		return os.ErrInvalid
	}

	if size < int64(len(f.buffer)) {
		f.buffer = f.buffer[:size]
	} else {
		newBuf := make([]byte, size)
		copy(newBuf, f.buffer)
		f.buffer = newBuf
	}
	return nil
}

// Readdir reads directory entries (lists objects with the file's key as prefix).
// If n > 0, Readdir returns at most n entries. If n <= 0, it returns all entries.
func (f *File) Readdir(n int) ([]os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.writing && f.body == nil {
		output, err := f.fs.client.HeadObject(f.fs.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(f.fs.bucket),
			Key:    aws.String(f.key),
		})
		if err == nil && !strings.HasSuffix(f.key, "/") {
			return nil, &os.PathError{Op: "readdir", Path: f.name, Err: os.ErrInvalid}
		}
		_ = output
	}

	prefix := f.key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var infos []os.FileInfo
	seen := make(map[string]bool)

	var continuationToken *string
	for {
		output, err := f.fs.client.ListObjectsV2(f.fs.ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(f.fs.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, wrapError("readdir", f.name, err)
		}

		for _, obj := range output.Contents {
			key := aws.ToString(obj.Key)
			if key == prefix {
				continue
			}

			name := strings.TrimPrefix(key, prefix)
			slashIdx := strings.Index(name, "/")

			var displayName string
			var isDir bool

			if slashIdx == -1 {
				displayName = name
				isDir = false
			} else if slashIdx == len(name)-1 {
				displayName = name[:slashIdx]
				isDir = true
			} else {
				displayName = name[:slashIdx]
				isDir = true
			}

			if seen[displayName] {
				continue
			}
			seen[displayName] = true

			infos = append(infos, &fileInfo{
				name:    displayName,
				size:    aws.ToInt64(obj.Size),
				modTime: aws.ToTime(obj.LastModified),
				isDir:   isDir,
			})

			if n > 0 && len(infos) >= n {
				if len(infos) == 0 && n > 0 {
					return nil, io.EOF
				}
				return infos, nil
			}
		}

		if !aws.ToBool(output.IsTruncated) {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	if len(infos) == 0 && n > 0 {
		return nil, io.EOF
	}
	return infos, nil
}

// Readdirnames reads directory entry names.
func (f *File) Readdirnames(n int) ([]string, error) {
	infos, err := f.Readdir(n)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name()
	}
	return names, nil
}

// ReadDir reads the contents of the directory and returns a slice of up to n DirEntry values.
// If n > 0, ReadDir returns at most n entries. If n <= 0, it returns all entries.
func (f *File) ReadDir(n int) ([]iosfs.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.writing && f.body == nil {
		output, err := f.fs.client.HeadObject(f.fs.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(f.fs.bucket),
			Key:    aws.String(f.key),
		})
		if err == nil && !strings.HasSuffix(f.key, "/") {
			return nil, &os.PathError{Op: "readdir", Path: f.name, Err: os.ErrInvalid}
		}
		_ = output
	}

	prefix := f.key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var entries []iosfs.DirEntry
	seen := make(map[string]bool)

	var continuationToken *string
	for {
		output, err := f.fs.client.ListObjectsV2(f.fs.ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(f.fs.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, wrapError("readdir", f.name, err)
		}

		for _, obj := range output.Contents {
			key := aws.ToString(obj.Key)
			if key == prefix {
				continue
			}

			name := strings.TrimPrefix(key, prefix)
			slashIdx := strings.Index(name, "/")

			var displayName string
			var isDir bool

			if slashIdx == -1 {
				displayName = name
				isDir = false
			} else if slashIdx == len(name)-1 {
				displayName = name[:slashIdx]
				isDir = true
			} else {
				displayName = name[:slashIdx]
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

			if n > 0 && len(entries) >= n {
				if len(entries) == 0 && n > 0 {
					return nil, io.EOF
				}
				return entries, nil
			}
		}

		if !aws.ToBool(output.IsTruncated) {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	if len(entries) == 0 && n > 0 {
		return nil, io.EOF
	}
	return entries, nil
}
