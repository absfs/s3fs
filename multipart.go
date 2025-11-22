package s3fs

import (
	"bytes"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	// MinPartSize is the minimum size for a multipart upload part (5MB).
	MinPartSize = 5 * 1024 * 1024

	// DefaultPartSize is the default size for multipart upload parts (10MB).
	DefaultPartSize = 10 * 1024 * 1024
)

// MultipartUpload handles large file uploads to S3 using multipart upload.
type MultipartUpload struct {
	fs         *FileSystem
	key        string
	uploadID   string
	partNumber int32
	parts      []types.CompletedPart
	partSize   int64
}

// NewMultipartUpload creates a new multipart upload session.
func (fs *FileSystem) NewMultipartUpload(key string) (*MultipartUpload, error) {
	key = trimPrefix(key)

	output, err := fs.client.CreateMultipartUpload(fs.ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(fs.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, wrapError("NewMultipartUpload", key, err)
	}

	return &MultipartUpload{
		fs:         fs,
		key:        key,
		uploadID:   *output.UploadId,
		partNumber: 1,
		parts:      make([]types.CompletedPart, 0),
		partSize:   DefaultPartSize,
	}, nil
}

// SetPartSize sets the size of each part for the multipart upload.
// The part size must be at least MinPartSize (5MB).
func (mu *MultipartUpload) SetPartSize(size int64) error {
	if size < MinPartSize {
		return wrapError("SetPartSize", mu.key, ErrInvalidSeek)
	}
	mu.partSize = size
	return nil
}

// UploadPart uploads a single part of the multipart upload.
func (mu *MultipartUpload) UploadPart(data []byte) error {
	output, err := mu.fs.client.UploadPart(mu.fs.ctx, &s3.UploadPartInput{
		Bucket:     aws.String(mu.fs.bucket),
		Key:        aws.String(mu.key),
		UploadId:   aws.String(mu.uploadID),
		PartNumber: aws.Int32(mu.partNumber),
		Body:       bytes.NewReader(data),
	})
	if err != nil {
		return wrapError("UploadPart", mu.key, err)
	}

	mu.parts = append(mu.parts, types.CompletedPart{
		ETag:       output.ETag,
		PartNumber: aws.Int32(mu.partNumber),
	})
	mu.partNumber++

	return nil
}

// UploadFromReader uploads data from a reader, automatically splitting into parts.
func (mu *MultipartUpload) UploadFromReader(r io.Reader) error {
	buf := make([]byte, mu.partSize)

	for {
		n, err := io.ReadFull(r, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return wrapError("UploadFromReader", mu.key, err)
		}

		if n == 0 {
			break
		}

		// Upload this part
		if err := mu.UploadPart(buf[:n]); err != nil {
			return err
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
	}

	return nil
}

// Complete completes the multipart upload.
func (mu *MultipartUpload) Complete() error {
	_, err := mu.fs.client.CompleteMultipartUpload(mu.fs.ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(mu.fs.bucket),
		Key:      aws.String(mu.key),
		UploadId: aws.String(mu.uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: mu.parts,
		},
	})
	if err != nil {
		return wrapError("Complete", mu.key, err)
	}

	return nil
}

// Abort aborts the multipart upload and deletes all uploaded parts.
func (mu *MultipartUpload) Abort() error {
	_, err := mu.fs.client.AbortMultipartUpload(mu.fs.ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(mu.fs.bucket),
		Key:      aws.String(mu.key),
		UploadId: aws.String(mu.uploadID),
	})
	if err != nil {
		return wrapError("Abort", mu.key, err)
	}

	return nil
}

// trimPrefix is a helper function to remove leading slashes.
func trimPrefix(s string) string {
	if len(s) > 0 && s[0] == '/' {
		return s[1:]
	}
	return s
}
