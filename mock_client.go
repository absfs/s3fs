package s3fs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// MockS3Client is an in-memory mock implementation of S3Client for testing.
type MockS3Client struct {
	mu       sync.RWMutex
	objects  map[string]*mockObject  // key -> object
	uploads  map[string]*mockUpload  // uploadID -> upload
	uploadID int                     // counter for generating upload IDs

	// Error injection for testing failure scenarios
	GetObjectErr    error
	PutObjectErr    error
	DeleteObjectErr error
	CopyObjectErr   error
	HeadObjectErr   error
	ListObjectsErr  error

	// Per-key error injection for DeleteObject (checked before global DeleteObjectErr)
	DeleteObjectKeyErrs map[string]error

	// DefaultMaxKeys overrides the default 1000 max keys for ListObjectsV2.
	// Set to a small value in tests to force pagination behavior.
	DefaultMaxKeys int32

	// Call tracking for assertions
	GetObjectCalls    []string
	PutObjectCalls    []string
	DeleteObjectCalls []string
	CopyObjectCalls   []copyObjectCall
	HeadObjectCalls   []string
	ListObjectsCalls  []string
}

type mockUpload struct {
	key   string
	parts map[int32][]byte // partNumber -> data
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
	m.uploads = nil
	m.uploadID = 0
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

// ListObjectsV2 implements S3Client with pagination support via ContinuationToken.
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

	// Collect and sort matching keys for deterministic pagination
	var keys []string
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	// Handle ContinuationToken (the token is the last key from previous page)
	startIdx := 0
	if params.ContinuationToken != nil {
		token := aws.ToString(params.ContinuationToken)
		for i, key := range keys {
			if key > token {
				startIdx = i
				break
			}
			// If we reach the end without finding a key > token, no results
			if i == len(keys)-1 {
				startIdx = len(keys)
			}
		}
	}

	maxKeys := int32(1000)
	if m.DefaultMaxKeys > 0 {
		maxKeys = m.DefaultMaxKeys
	}
	if params.MaxKeys != nil {
		maxKeys = *params.MaxKeys
	}

	var contents []types.Object
	endIdx := startIdx
	for i := startIdx; i < len(keys) && int32(len(contents)) < maxKeys; i++ {
		obj := m.objects[keys[i]]
		contents = append(contents, types.Object{
			Key:          aws.String(keys[i]),
			Size:         aws.Int64(int64(len(obj.data))),
			LastModified: aws.Time(obj.lastModified),
		})
		endIdx = i
	}

	isTruncated := endIdx < len(keys)-1 && len(contents) > 0
	var nextToken *string
	if isTruncated {
		nextToken = aws.String(keys[endIdx])
	}

	return &s3.ListObjectsV2Output{
		Contents:              contents,
		IsTruncated:           aws.Bool(isTruncated),
		KeyCount:              aws.Int32(int32(len(contents))),
		NextContinuationToken: nextToken,
	}, nil
}

// CreateMultipartUpload implements S3Client.
func (m *MockS3Client) CreateMultipartUpload(ctx context.Context, params *s3.CreateMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.uploadID++
	id := fmt.Sprintf("upload-%d", m.uploadID)

	if m.uploads == nil {
		m.uploads = make(map[string]*mockUpload)
	}
	m.uploads[id] = &mockUpload{
		key:   aws.ToString(params.Key),
		parts: make(map[int32][]byte),
	}

	return &s3.CreateMultipartUploadOutput{
		UploadId: aws.String(id),
	}, nil
}

// UploadPart implements S3Client.
func (m *MockS3Client) UploadPart(ctx context.Context, params *s3.UploadPartInput, optFns ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := aws.ToString(params.UploadId)
	upload, ok := m.uploads[id]
	if !ok {
		return nil, fmt.Errorf("no such upload: %s", id)
	}

	data, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}

	partNum := aws.ToInt32(params.PartNumber)
	upload.parts[partNum] = data

	etag := fmt.Sprintf("etag-part-%d", partNum)
	return &s3.UploadPartOutput{
		ETag: aws.String(etag),
	}, nil
}

// CompleteMultipartUpload implements S3Client.
func (m *MockS3Client) CompleteMultipartUpload(ctx context.Context, params *s3.CompleteMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := aws.ToString(params.UploadId)
	upload, ok := m.uploads[id]
	if !ok {
		return nil, fmt.Errorf("no such upload: %s", id)
	}

	// Assemble parts in order
	var assembled []byte
	for i := int32(1); i <= int32(len(upload.parts)); i++ {
		data, ok := upload.parts[i]
		if !ok {
			return nil, fmt.Errorf("missing part %d", i)
		}
		assembled = append(assembled, data...)
	}

	// Store as final object
	m.objects[upload.key] = &mockObject{
		data:         assembled,
		lastModified: time.Now(),
		contentType:  "application/octet-stream",
	}

	delete(m.uploads, id)
	return &s3.CompleteMultipartUploadOutput{}, nil
}

// AbortMultipartUpload implements S3Client.
func (m *MockS3Client) AbortMultipartUpload(ctx context.Context, params *s3.AbortMultipartUploadInput, optFns ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := aws.ToString(params.UploadId)
	delete(m.uploads, id)
	return &s3.AbortMultipartUploadOutput{}, nil
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
