package s3fs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// MockS3Client is an in-memory mock implementation of S3Client for testing.
type MockS3Client struct {
	mu      sync.RWMutex
	objects map[string]*mockObject // key -> object

	// Error injection for testing failure scenarios
	GetObjectErr    error
	PutObjectErr    error
	DeleteObjectErr error
	CopyObjectErr   error
	HeadObjectErr   error
	ListObjectsErr  error

	// Per-key error injection for DeleteObject (checked before global DeleteObjectErr)
	DeleteObjectKeyErrs map[string]error

	// Call tracking for assertions
	GetObjectCalls    []string
	PutObjectCalls    []string
	DeleteObjectCalls []string
	CopyObjectCalls   []copyObjectCall
	HeadObjectCalls   []string
	ListObjectsCalls  []string
}

type mockObject struct {
	data         []byte
	lastModified time.Time
	contentType  string
	metadata     map[string]string
}

type copyObjectCall struct {
	Source      string
	Destination string
}

// NewMockS3Client creates a new mock S3 client for testing.
func NewMockS3Client() *MockS3Client {
	return &MockS3Client{
		objects: make(map[string]*mockObject),
	}
}

// Reset clears all objects and error injections.
func (m *MockS3Client) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.objects = make(map[string]*mockObject)
	m.GetObjectErr = nil
	m.PutObjectErr = nil
	m.DeleteObjectErr = nil
	m.DeleteObjectKeyErrs = nil
	m.CopyObjectErr = nil
	m.HeadObjectErr = nil
	m.ListObjectsErr = nil
	m.GetObjectCalls = nil
	m.PutObjectCalls = nil
	m.DeleteObjectCalls = nil
	m.CopyObjectCalls = nil
	m.HeadObjectCalls = nil
	m.ListObjectsCalls = nil
}

// PutTestObject adds a test object directly to the mock store.
func (m *MockS3Client) PutTestObject(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.objects[key] = &mockObject{
		data:         data,
		lastModified: time.Now(),
		contentType:  "application/octet-stream",
	}
}

// GetTestObject retrieves a test object from the mock store.
func (m *MockS3Client) GetTestObject(key string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	obj, ok := m.objects[key]
	if !ok {
		return nil, false
	}
	return obj.data, true
}

// HasObject checks if an object exists in the mock store.
func (m *MockS3Client) HasObject(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.objects[key]
	return ok
}

// ObjectCount returns the number of objects in the mock store.
func (m *MockS3Client) ObjectCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.objects)
}

// GetObject implements S3Client.
func (m *MockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	m.mu.Lock()
	key := aws.ToString(params.Key)
	m.GetObjectCalls = append(m.GetObjectCalls, key)
	m.mu.Unlock()

	if m.GetObjectErr != nil {
		return nil, m.GetObjectErr
	}

	m.mu.RLock()
	obj, ok := m.objects[key]
	m.mu.RUnlock()

	if !ok {
		return nil, &types.NoSuchKey{Message: aws.String("The specified key does not exist.")}
	}

	data := obj.data
	contentLength := int64(len(data))

	// Handle range requests
	if params.Range != nil {
		rangeStr := aws.ToString(params.Range)
		start, end, err := parseRange(rangeStr, int64(len(obj.data)))
		if err != nil {
			return nil, fmt.Errorf("invalid range: %w", err)
		}
		data = obj.data[start : end+1]
		contentLength = int64(len(data))
	}

	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(contentLength),
		LastModified:  aws.Time(obj.lastModified),
		ContentType:   aws.String(obj.contentType),
	}, nil
}

// PutObject implements S3Client.
func (m *MockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	m.mu.Lock()
	key := aws.ToString(params.Key)
	m.PutObjectCalls = append(m.PutObjectCalls, key)
	m.mu.Unlock()

	if m.PutObjectErr != nil {
		return nil, m.PutObjectErr
	}

	var data []byte
	if params.Body != nil {
		var err error
		data, err = io.ReadAll(params.Body)
		if err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	m.objects[key] = &mockObject{
		data:         data,
		lastModified: time.Now(),
		contentType:  aws.ToString(params.ContentType),
	}
	m.mu.Unlock()

	return &s3.PutObjectOutput{}, nil
}

// DeleteObject implements S3Client.
func (m *MockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	m.mu.Lock()
	key := aws.ToString(params.Key)
	m.DeleteObjectCalls = append(m.DeleteObjectCalls, key)
	m.mu.Unlock()

	if keyErr, ok := m.DeleteObjectKeyErrs[key]; ok {
		return nil, keyErr
	}
	if m.DeleteObjectErr != nil {
		return nil, m.DeleteObjectErr
	}

	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()

	return &s3.DeleteObjectOutput{}, nil
}

// CopyObject implements S3Client.
func (m *MockS3Client) CopyObject(ctx context.Context, params *s3.CopyObjectInput, optFns ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	m.mu.Lock()
	source := aws.ToString(params.CopySource)
	dest := aws.ToString(params.Key)
	m.CopyObjectCalls = append(m.CopyObjectCalls, copyObjectCall{Source: source, Destination: dest})
	m.mu.Unlock()

	if m.CopyObjectErr != nil {
		return nil, m.CopyObjectErr
	}

	// Extract the source key from the CopySource (format: /bucket/key or bucket/key)
	sourceKey := source
	if strings.HasPrefix(sourceKey, "/") {
		sourceKey = sourceKey[1:]
	}
	// Remove bucket prefix if present
	parts := strings.SplitN(sourceKey, "/", 2)
	if len(parts) == 2 {
		sourceKey = parts[1]
	}

	m.mu.RLock()
	obj, ok := m.objects[sourceKey]
	m.mu.RUnlock()

	if !ok {
		return nil, &types.NoSuchKey{Message: aws.String("The specified key does not exist.")}
	}

	m.mu.Lock()
	m.objects[dest] = &mockObject{
		data:         append([]byte(nil), obj.data...),
		lastModified: time.Now(),
		contentType:  obj.contentType,
		metadata:     obj.metadata,
	}
	m.mu.Unlock()

	return &s3.CopyObjectOutput{}, nil
}

// HeadObject implements S3Client.
func (m *MockS3Client) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	m.mu.Lock()
	key := aws.ToString(params.Key)
	m.HeadObjectCalls = append(m.HeadObjectCalls, key)
	m.mu.Unlock()

	if m.HeadObjectErr != nil {
		return nil, m.HeadObjectErr
	}

	m.mu.RLock()
	obj, ok := m.objects[key]
	m.mu.RUnlock()

	if !ok {
		return nil, &types.NoSuchKey{Message: aws.String("The specified key does not exist.")}
	}

	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(obj.data))),
		LastModified:  aws.Time(obj.lastModified),
		ContentType:   aws.String(obj.contentType),
	}, nil
}

// ListObjectsV2 implements S3Client.
func (m *MockS3Client) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	m.mu.Lock()
	prefix := aws.ToString(params.Prefix)
	m.ListObjectsCalls = append(m.ListObjectsCalls, prefix)
	m.mu.Unlock()

	if m.ListObjectsErr != nil {
		return nil, m.ListObjectsErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var contents []types.Object
	for key, obj := range m.objects {
		if strings.HasPrefix(key, prefix) {
			contents = append(contents, types.Object{
				Key:          aws.String(key),
				Size:         aws.Int64(int64(len(obj.data))),
				LastModified: aws.Time(obj.lastModified),
			})
		}
	}

	// Handle MaxKeys
	maxKeys := int32(1000) // Default
	if params.MaxKeys != nil {
		maxKeys = *params.MaxKeys
	}

	isTruncated := false
	if int32(len(contents)) > maxKeys {
		contents = contents[:maxKeys]
		isTruncated = true
	}

	return &s3.ListObjectsV2Output{
		Contents:    contents,
		IsTruncated: aws.Bool(isTruncated),
		KeyCount:    aws.Int32(int32(len(contents))),
	}, nil
}

// parseRange parses an HTTP range header value like "bytes=0-99".
func parseRange(rangeStr string, size int64) (start, end int64, err error) {
	if !strings.HasPrefix(rangeStr, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	rangeStr = strings.TrimPrefix(rangeStr, "bytes=")
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	if parts[0] != "" {
		_, err = fmt.Sscanf(parts[0], "%d", &start)
		if err != nil {
			return 0, 0, err
		}
	}

	if parts[1] != "" {
		_, err = fmt.Sscanf(parts[1], "%d", &end)
		if err != nil {
			return 0, 0, err
		}
	} else {
		end = size - 1
	}

	if start < 0 || end >= size || start > end {
		return 0, 0, fmt.Errorf("range out of bounds")
	}

	return start, end, nil
}

// Ensure MockS3Client satisfies S3Client interface
var _ S3Client = (*MockS3Client)(nil)
