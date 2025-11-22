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
// It implements the absfs.File interface for S3 object operations.
// Files are opened in either read or write mode. Write mode uses an in-memory
// buffer that is uploaded to S3 on Close().
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
// On the first call, it fetches the object from S3 and reads from the response body.
// Subsequent calls continue reading from the same response stream.
func (f *File) Read(b []byte) (int, error) {
	if f.writing {
		return 0, ErrReadOnWriteFile
	}

	// Lazy load the object body
	if f.body == nil {
		output, err := f.fs.client.GetObject(f.fs.ctx, &s3.GetObjectInput{
			Bucket: aws.String(f.fs.bucket),
			Key:    aws.String(f.key),
		})
		if err != nil {
			return 0, wrapError("Read", f.name, err)
		}
		f.body = output.Body
	}

	n, err := f.body.Read(b)
	if err != nil && err != io.EOF {
		return n, wrapError("Read", f.name, err)
	}
	return n, err
}

// ReadAt reads from the S3 object at a specific offset.
// It uses S3's Range header to read only the requested bytes.
// Each call makes a separate request to S3.
func (f *File) ReadAt(b []byte, off int64) (int, error) {
	if f.writing {
		return 0, ErrReadOnWriteFile
	}

	// S3 supports range reads
	rangeStr := fmt.Sprintf("bytes=%d-%d", off, off+int64(len(b))-1)
	output, err := f.fs.client.GetObject(f.fs.ctx, &s3.GetObjectInput{
		Bucket: aws.String(f.fs.bucket),
		Key:    aws.String(f.key),
		Range:  aws.String(rangeStr),
	})
	if err != nil {
		return 0, wrapError("ReadAt", f.name, err)
	}
	defer output.Body.Close()

	n, err := io.ReadFull(output.Body, b)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return n, wrapError("ReadAt", f.name, err)
	}
	return n, err
}

// Write writes to the file buffer (will be uploaded on Close).
// Data is buffered in memory until Close() is called, which uploads the entire
// buffer to S3 in a single operation.
func (f *File) Write(b []byte) (int, error) {
	if !f.writing {
		return 0, ErrWriteOnReadFile
	}

	f.buffer = append(f.buffer, b...)
	f.offset += int64(len(b))
	return len(b), nil
}

// WriteAt writes to the buffer at a specific offset.
// The buffer is automatically expanded if the write extends beyond its current size.
func (f *File) WriteAt(b []byte, off int64) (int, error) {
	if !f.writing {
		return 0, ErrWriteOnReadFile
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
// For write mode files, this uploads the entire buffer to S3.
// For read mode files, this closes the response body.
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
		if err != nil {
			return wrapError("Close", f.name, err)
		}
	}

	return nil
}

// Seek seeks within the file.
// Note: This is a simplified implementation. For S3, seeking is limited and
// io.SeekEnd is not supported as it would require knowing the file size.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	// For S3, seeking is limited. This is a simplified implementation.
	switch whence {
	case io.SeekStart:
		f.offset = offset
	case io.SeekCurrent:
		f.offset += offset
	case io.SeekEnd:
		// Would need to know file size
		return 0, ErrInvalidSeek
	}
	return f.offset, nil
}

// Stat returns file info.
func (f *File) Stat() (os.FileInfo, error) {
	return f.fs.Stat(f.name)
}

// Sync is a no-op for S3.
// All writes are buffered until Close(), at which point they are synchronously uploaded.
func (f *File) Sync() error {
	return nil
}

// Truncate changes the size of the file buffer.
// If size is smaller than the current buffer, data is truncated.
// If size is larger, the buffer is extended with zero bytes.
func (f *File) Truncate(size int64) error {
	if !f.writing {
		return ErrWriteOnReadFile
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
// In S3, "directories" are represented by objects with keys that have the directory
// as a prefix. If n > 0, at most n entries are returned.
func (f *File) Readdir(n int) ([]os.FileInfo, error) {
	prefix := f.key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	output, err := f.fs.client.ListObjectsV2(f.fs.ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(f.fs.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, wrapError("Readdir", f.name, err)
	}

	var infos []os.FileInfo
	for _, obj := range output.Contents {
		infos = append(infos, &fileInfo{
			name:    aws.ToString(obj.Key),
			size:    *obj.Size,
			modTime: *obj.LastModified,
			isDir:   strings.HasSuffix(aws.ToString(obj.Key), "/"),
		})

		if n > 0 && len(infos) >= n {
			break
		}
	}

	return infos, nil
}

// Readdirnames reads directory entry names.
// It returns the names of up to n entries in the directory.
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
