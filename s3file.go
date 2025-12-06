package s3fs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// File represents a file in S3.
type File struct {
	fs      *FileSystem
	name    string
	key     string
	writing bool
	buffer  []byte
	offset  int64
	body    io.ReadCloser
}

// Name returns the name of the file.
func (f *File) Name() string {
	return f.name
}

// Read reads from the S3 object.
func (f *File) Read(b []byte) (int, error) {
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

// ReadAt reads from the S3 object at a specific offset.
func (f *File) ReadAt(b []byte, off int64) (int, error) {
	if f.writing {
		return 0, os.ErrInvalid
	}

	// S3 supports range reads - Issue #11 fix: use proper integer formatting
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

// Write writes to the file buffer (will be uploaded on Close).
func (f *File) Write(b []byte) (int, error) {
	if !f.writing {
		return 0, os.ErrInvalid
	}

	f.buffer = append(f.buffer, b...)
	f.offset += int64(len(b))
	return len(b), nil
}

// WriteAt writes to the buffer at a specific offset.
func (f *File) WriteAt(b []byte, off int64) (int, error) {
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
	return f.Write([]byte(s))
}

// Close closes the file and uploads to S3 if writing.
func (f *File) Close() error {
	if f.body != nil {
		f.body.Close()
	}

	if f.writing {
		// Upload the buffer to S3
		_, err := f.fs.client.PutObject(f.fs.ctx, &s3.PutObjectInput{
			Bucket: aws.String(f.fs.bucket),
			Key:    aws.String(f.key),
			Body:   bytes.NewReader(f.buffer),
		})
		return wrapError("close", f.name, err)
	}

	return nil
}

// Seek seeks within the file.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	// For S3, seeking is limited. This is a simplified implementation.
	switch whence {
	case io.SeekStart:
		f.offset = offset
	case io.SeekCurrent:
		f.offset += offset
	case io.SeekEnd:
		// Would need to know file size
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

// Truncate truncates the file.
func (f *File) Truncate(size int64) error {
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

// Readdir reads directory entries (lists objects with prefix).
func (f *File) Readdir(n int) ([]os.FileInfo, error) {
	// Check if this is a directory (or could be one)
	// For read operations on files opened for reading, check if it's actually a directory marker
	if !f.writing && f.body == nil {
		// Check if the key exists and is a file (not a directory)
		output, err := f.fs.client.HeadObject(f.fs.ctx, &s3.HeadObjectInput{
			Bucket: aws.String(f.fs.bucket),
			Key:    aws.String(f.key),
		})
		if err == nil {
			// Object exists and is a file (not ending in /)
			if !strings.HasSuffix(f.key, "/") {
				return nil, &os.PathError{Op: "readdir", Path: f.name, Err: os.ErrInvalid}
			}
		}
		// If object doesn't exist, that's ok - we'll try listing with prefix
		_ = output
	}

	prefix := f.key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	output, err := f.fs.client.ListObjectsV2(f.fs.ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(f.fs.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, wrapError("readdir", f.name, err)
	}

	var infos []os.FileInfo
	for _, obj := range output.Contents {
		key := aws.ToString(obj.Key)
		// Skip the directory marker itself (e.g., "dir/" when listing "dir/")
		if key == prefix {
			continue
		}

		// Issue #12 fix: use safe nil-pointer handling with aws.ToInt64/aws.ToTime
		infos = append(infos, &fileInfo{
			name:    key,
			size:    aws.ToInt64(obj.Size),
			modTime: aws.ToTime(obj.LastModified),
			isDir:   strings.HasSuffix(key, "/"),
		})

		if n > 0 && len(infos) >= n {
			break
		}
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
