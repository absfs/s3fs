package s3fs

import "context"

// WithContext returns a new FileSystem that uses the given context for all operations.
// This allows for cancellation and timeout control of S3 operations.
func (fs *FileSystem) WithContext(ctx context.Context) *FileSystem {
	return &FileSystem{
		client: fs.client,
		bucket: fs.bucket,
		ctx:    ctx,
	}
}

// Context returns the context used by the filesystem.
func (fs *FileSystem) Context() context.Context {
	return fs.ctx
}
