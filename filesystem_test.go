package s3fs

import (
	"errors"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Helper to create a test filesystem with mock client
func newTestFS() (*FileSystem, *MockS3Client) {
	mock := NewMockS3Client()
	fs := NewWithClient(mock, "test-bucket")
	return fs, mock
}

// =============================================================================
// Task 3.1: FileSystem Core Operations Tests
// =============================================================================

// --- OpenFile Tests ---

func TestOpenFile_ReadMode(t *testing.T) {
	fs, _ := newTestFS()

	f, err := fs.OpenFile("test.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	file, ok := f.(*File)
	if !ok {
		t.Fatal("OpenFile did not return *File")
	}

	if file.writing {
		t.Error("File should be in read mode")
	}
	if file.name != "test.txt" {
		t.Errorf("File name = %q, want %q", file.name, "test.txt")
	}
}

func TestOpenFile_WriteMode_CREATE(t *testing.T) {
	fs, _ := newTestFS()

	f, err := fs.OpenFile("new.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	file, ok := f.(*File)
	if !ok {
		t.Fatal("OpenFile did not return *File")
	}

	if !file.writing {
		t.Error("File should be in write mode")
	}
}

func TestOpenFile_WriteMode_RDWR(t *testing.T) {
	fs, _ := newTestFS()

	f, err := fs.OpenFile("rw.txt", os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	file := f.(*File)
	if !file.writing {
		t.Error("File should be in write mode for O_RDWR")
	}
}

func TestOpenFile_WriteMode_WRONLY(t *testing.T) {
	fs, _ := newTestFS()

	f, err := fs.OpenFile("wo.txt", os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	file := f.(*File)
	if !file.writing {
		t.Error("File should be in write mode for O_WRONLY")
	}
}

func TestOpenFile_PathSanitization_LeadingSlash(t *testing.T) {
	fs, _ := newTestFS()

	f, err := fs.OpenFile("/path/to/file.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	file := f.(*File)
	if file.key != "path/to/file.txt" {
		t.Errorf("Key = %q, want %q (leading slash should be removed)", file.key, "path/to/file.txt")
	}
}

func TestOpenFile_EmptyBuffer_WriteMode(t *testing.T) {
	fs, _ := newTestFS()

	f, err := fs.OpenFile("empty.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	file := f.(*File)
	if len(file.buffer) != 0 {
		t.Errorf("Buffer should be empty, got len=%d", len(file.buffer))
	}
}

// --- Mkdir Tests ---

func TestMkdir_Basic(t *testing.T) {
	fs, mock := newTestFS()

	err := fs.Mkdir("testdir", 0755)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	// Verify object was created with trailing slash
	if !mock.HasObject("testdir/") {
		t.Error("Directory object not created with trailing slash")
	}

	// Verify the object is empty (zero-byte)
	data, ok := mock.GetTestObject("testdir/")
	if !ok {
		t.Fatal("Directory object not found")
	}
	if len(data) != 0 {
		t.Errorf("Directory object should be empty, got %d bytes", len(data))
	}
}

func TestMkdir_WithTrailingSlash(t *testing.T) {
	fs, mock := newTestFS()

	err := fs.Mkdir("dir-with-slash/", 0755)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	// Should not double the trailing slash
	if !mock.HasObject("dir-with-slash/") {
		t.Error("Directory object not created")
	}
}

func TestMkdir_NestedPath(t *testing.T) {
	fs, mock := newTestFS()

	err := fs.Mkdir("parent/child/grandchild", 0755)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	if !mock.HasObject("parent/child/grandchild/") {
		t.Error("Nested directory object not created")
	}
}

func TestMkdir_LeadingSlashRemoved(t *testing.T) {
	fs, mock := newTestFS()

	err := fs.Mkdir("/absolute-style-path", 0755)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	if !mock.HasObject("absolute-style-path/") {
		t.Error("Directory should be created without leading slash")
	}
}

func TestMkdir_Error(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutObjectErr = errors.New("simulated error")

	err := fs.Mkdir("fail-dir", 0755)
	if err == nil {
		t.Error("Expected error from Mkdir")
	}
}

// --- Remove Tests ---

func TestRemove_ExistingFile(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("to-delete.txt", []byte("content"))

	err := fs.Remove("to-delete.txt")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if mock.HasObject("to-delete.txt") {
		t.Error("Object should be deleted")
	}
}

func TestRemove_NonExistentFile(t *testing.T) {
	fs, _ := newTestFS()

	// S3 DeleteObject returns success even for non-existent objects
	err := fs.Remove("does-not-exist.txt")
	if err != nil {
		t.Errorf("Remove should succeed for non-existent file (S3 behavior): %v", err)
	}
}

func TestRemove_Directory(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("mydir/", []byte{})

	err := fs.Remove("mydir/")
	if err != nil {
		t.Fatalf("Remove directory failed: %v", err)
	}

	if mock.HasObject("mydir/") {
		t.Error("Directory object should be deleted")
	}
}

func TestRemove_LeadingSlashRemoved(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("file.txt", []byte("data"))

	err := fs.Remove("/file.txt")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if mock.HasObject("file.txt") {
		t.Error("File should be deleted")
	}
}

func TestRemove_Error(t *testing.T) {
	fs, mock := newTestFS()
	mock.DeleteObjectErr = errors.New("simulated error")

	err := fs.Remove("fail.txt")
	if err == nil {
		t.Error("Expected error from Remove")
	}
}

// --- Rename Tests ---

func TestRename_Basic(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("old.txt", []byte("content"))

	err := fs.Rename("old.txt", "new.txt")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	// New object should exist with same content
	data, ok := mock.GetTestObject("new.txt")
	if !ok {
		t.Fatal("New object not found after rename")
	}
	if string(data) != "content" {
		t.Errorf("Content = %q, want %q", string(data), "content")
	}

	// Old object should be deleted
	if mock.HasObject("old.txt") {
		t.Error("Old object should be deleted after rename")
	}
}

func TestRename_CrossDirectory(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("dir1/file.txt", []byte("data"))

	err := fs.Rename("dir1/file.txt", "dir2/file.txt")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	if !mock.HasObject("dir2/file.txt") {
		t.Error("File should be in new directory")
	}
	if mock.HasObject("dir1/file.txt") {
		t.Error("File should not be in old directory")
	}
}

func TestRename_LeadingSlashRemoved(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("source.txt", []byte("data"))

	err := fs.Rename("/source.txt", "/dest.txt")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	if !mock.HasObject("dest.txt") {
		t.Error("Destination should not have leading slash")
	}
}

func TestRename_CopyError(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("src.txt", []byte("data"))
	mock.CopyObjectErr = errors.New("copy failed")

	err := fs.Rename("src.txt", "dst.txt")
	if err == nil {
		t.Error("Expected error from Rename when copy fails")
	}

	// Source should still exist
	if !mock.HasObject("src.txt") {
		t.Error("Source should still exist when copy fails")
	}
}

func TestRename_DeleteError(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("src.txt", []byte("data"))
	mock.DeleteObjectErr = errors.New("delete failed")

	err := fs.Rename("src.txt", "dst.txt")
	if err == nil {
		t.Error("Expected error from Rename when delete fails")
	}

	// Both objects may exist (copy succeeded, delete failed)
	if !mock.HasObject("dst.txt") {
		t.Error("Destination should exist after copy")
	}
}

func TestRename_SourceNotFound(t *testing.T) {
	fs, _ := newTestFS()

	err := fs.Rename("nonexistent.txt", "new.txt")
	if err == nil {
		t.Error("Expected error when source doesn't exist")
	}

	var noSuchKey *types.NoSuchKey
	if !errors.As(err, &noSuchKey) {
		t.Errorf("Expected NoSuchKey error, got: %v", err)
	}
}

// --- Stat Tests ---

func TestStat_ExistingFile(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("file.txt", []byte("hello world"))

	info, err := fs.Stat("file.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "file.txt" {
		t.Errorf("Name = %q, want %q", info.Name(), "file.txt")
	}
	if info.Size() != 11 {
		t.Errorf("Size = %d, want 11", info.Size())
	}
	if info.IsDir() {
		t.Error("IsDir should be false for file")
	}
}

func TestStat_Directory(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("mydir/", []byte{})

	info, err := fs.Stat("mydir/")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if !info.IsDir() {
		t.Error("IsDir should be true for directory (trailing slash)")
	}
}

func TestStat_NestedPath(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("a/b/c/file.txt", []byte("nested"))

	info, err := fs.Stat("a/b/c/file.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "file.txt" {
		t.Errorf("Name = %q, want %q (base name)", info.Name(), "file.txt")
	}
}

func TestStat_NonExistentFile(t *testing.T) {
	fs, _ := newTestFS()

	_, err := fs.Stat("nonexistent.txt")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestStat_LeadingSlashRemoved(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("file.txt", []byte("data"))

	info, err := fs.Stat("/file.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "file.txt" {
		t.Errorf("Name = %q, want %q", info.Name(), "file.txt")
	}
}

func TestStat_Error(t *testing.T) {
	fs, mock := newTestFS()
	mock.HeadObjectErr = errors.New("simulated error")

	_, err := fs.Stat("any.txt")
	if err == nil {
		t.Error("Expected error from Stat")
	}
}

func TestStat_FileInfoMethods(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("methods.txt", []byte("test"))

	info, err := fs.Stat("methods.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	// Test Mode
	if info.Mode() != 0644 {
		t.Errorf("Mode = %o, want 0644", info.Mode())
	}

	// Test ModTime (should be non-zero)
	if info.ModTime().IsZero() {
		t.Error("ModTime should not be zero")
	}

	// Test Sys
	if info.Sys() != nil {
		t.Error("Sys should return nil")
	}
}

// --- Chmod, Chtimes, Chown Tests ---

func TestChmod_NotImplemented(t *testing.T) {
	fs, _ := newTestFS()

	err := fs.Chmod("file.txt", 0755)
	if err == nil {
		t.Error("Expected error from Chmod")
	}
}

func TestChtimes_NotImplemented(t *testing.T) {
	fs, _ := newTestFS()

	err := fs.Chtimes("file.txt", testTime, testTime)
	if err == nil {
		t.Error("Expected error from Chtimes")
	}
}

func TestChown_NotImplemented(t *testing.T) {
	fs, _ := newTestFS()

	err := fs.Chown("file.txt", 1000, 1000)
	if err == nil {
		t.Error("Expected error from Chown")
	}
}
