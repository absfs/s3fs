package s3fs

import (
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/fstesting"
)

// TestS3FS_FSTesting runs the fstesting suite against s3fs.
func TestS3FS_FSTesting(t *testing.T) {
	// Create a mock S3 client for testing
	mockClient := NewMockS3Client()

	// Create the s3fs filesystem with the mock client
	s3fsImpl := NewWithClient(mockClient, "test-bucket")

	// Use ExtendFiler to create a full FileSystem from the Filer implementation
	fs := absfs.ExtendFiler(s3fsImpl)

	// Configure the test suite with s3fs capabilities
	suite := &fstesting.Suite{
		FS: fs,
		Features: fstesting.Features{
			// S3 does not support symlinks
			Symlinks: false,

			// S3 does not support hard links
			HardLinks: false,

			// S3 has limited/no permission support (Chmod returns ErrNotImplemented)
			Permissions: false,

			// S3 does not support Chtimes (returns ErrNotImplemented)
			Timestamps: false,

			// S3 keys are case-sensitive
			CaseSensitive: true,

			// S3 rename is atomic (copy + delete in one operation conceptually)
			AtomicRename: true,

			// S3 doesn't support sparse files (it's object storage)
			SparseFiles: false,

			// S3 supports large files
			LargeFiles: true,
		},
	}

	// Run the full test suite
	suite.Run(t)
}

// TestS3FS_QuickCheck runs a quick sanity check.
func TestS3FS_QuickCheck(t *testing.T) {
	mockClient := NewMockS3Client()
	s3fsImpl := NewWithClient(mockClient, "test-bucket")
	fs := absfs.ExtendFiler(s3fsImpl)

	suite := &fstesting.Suite{
		FS: fs,
		Features: fstesting.Features{
			Symlinks:      false,
			HardLinks:     false,
			Permissions:   false,
			Timestamps:    false,
			CaseSensitive: true,
			AtomicRename:  true,
			SparseFiles:   false,
			LargeFiles:    true,
		},
	}

	suite.QuickCheck(t)
}
