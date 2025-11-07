package s3fs

import (
	"bytes"
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
			return 0, err
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

	// S3 supports range reads
	rangeStr := aws.String("bytes=" + string(rune(off)) + "-" + string(rune(off+int64(len(b))-1)))
	output, err := f.fs.client.GetObject(f.fs.ctx, &s3.GetObjectInput{
		Bucket: aws.String(f.fs.bucket),
		Key:    aws.String(f.key),
		Range:  rangeStr,
	})
	if err != nil {
		return 0, err
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
		return err
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
	prefix := f.key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	output, err := f.fs.client.ListObjectsV2(f.fs.ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(f.fs.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, err
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
