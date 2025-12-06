package s3fs

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
)

// =============================================================================
// Task 3.5: Edge Cases and Error Handling Tests
// =============================================================================

// --- Path Validation Tests (Issue #14) ---

func TestPath_EmptyPath(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("", []byte("root"))

	// Empty path should work (S3 allows empty keys technically)
	f, err := fs.OpenFile("", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile with empty path failed: %v", err)
	}

	if f.Name() != "" {
		t.Errorf("Name = %q, want empty string", f.Name())
	}
}

func TestPath_SpecialCharacters(t *testing.T) {
	fs, mock := newTestFS()

	// S3 supports special characters in keys
	specialPaths := []string{
		"file with spaces.txt",
		"file@special#chars.txt",
		"path/with/unicode-日本語.txt",
		"path/file%20encoded.txt",
		"path/file+plus.txt",
	}

	for _, p := range specialPaths {
		mock.PutTestObject(p, []byte("content"))

		f, err := fs.OpenFile(p, os.O_RDONLY, 0)
		if err != nil {
			t.Errorf("OpenFile(%q) failed: %v", p, err)
			continue
		}

		info, err := fs.Stat(p)
		if err != nil {
			t.Errorf("Stat(%q) failed: %v", p, err)
			continue
		}

		if info.Size() != 7 {
			t.Errorf("Stat(%q) size = %d, want 7", p, info.Size())
		}

		f.Close()
	}
}

func TestPath_DotPaths(t *testing.T) {
	fs, _ := newTestFS()

	// These paths should work (S3 doesn't interpret . and .. specially)
	paths := []string{
		"./current.txt",
		"../parent.txt",
		"dir/./file.txt",
		"dir/../sibling.txt",
	}

	for _, p := range paths {
		f, err := fs.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			t.Errorf("OpenFile(%q) failed: %v", p, err)
			continue
		}
		f.Close()
	}
}

func TestPath_DeepNesting(t *testing.T) {
	fs, mock := newTestFS()

	deepPath := "a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z/file.txt"
	mock.PutTestObject(deepPath, []byte("deep"))

	info, err := fs.Stat(deepPath)
	if err != nil {
		t.Fatalf("Stat on deep path failed: %v", err)
	}

	if info.Name() != "file.txt" {
		t.Errorf("Name = %q, want %q", info.Name(), "file.txt")
	}
}

// --- Concurrency Tests (Issue #7) ---

func TestConcurrency_MultipleReaders(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("concurrent.txt", []byte("concurrent read test"))

	var wg sync.WaitGroup
	errChan := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			f, err := fs.OpenFile("concurrent.txt", os.O_RDONLY, 0)
			if err != nil {
				errChan <- err
				return
			}

			buf := make([]byte, 100)
			_, err = f.Read(buf)
			if err != nil && err.Error() != "EOF" {
				errChan <- err
				return
			}

			f.Close()
		}()
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("Concurrent read error: %v", err)
	}
}

func TestConcurrency_MultipleWriters(t *testing.T) {
	fs, mock := newTestFS()

	var wg sync.WaitGroup
	errChan := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			filename := "concurrent-" + string(rune('0'+idx)) + ".txt"
			f, err := fs.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				errChan <- err
				return
			}

			_, err = f.Write([]byte("content"))
			if err != nil {
				errChan <- err
				return
			}

			err = f.Close()
			if err != nil {
				errChan <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("Concurrent write error: %v", err)
	}

	// Verify all files were created
	if mock.ObjectCount() < 10 {
		t.Errorf("Expected at least 10 objects, got %d", mock.ObjectCount())
	}
}

func TestConcurrency_MixedOperations(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("mixed.txt", []byte("initial content"))

	var wg sync.WaitGroup

	// Readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			f, _ := fs.OpenFile("mixed.txt", os.O_RDONLY, 0)
			buf := make([]byte, 100)
			f.Read(buf)
			f.Close()
		}()
	}

	// Writers (different files)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			filename := "mixed-write-" + string(rune('0'+idx)) + ".txt"
			f, _ := fs.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
			f.Write([]byte("written"))
			f.Close()
		}(i)
	}

	// Stats
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fs.Stat("mixed.txt")
		}()
	}

	wg.Wait()
}

// --- Error Propagation Tests (Issue #9) ---

func TestErrorPropagation_GetObject(t *testing.T) {
	fs, mock := newTestFS()
	testErr := errors.New("GetObject network error")
	mock.GetObjectErr = testErr

	f, _ := fs.OpenFile("file.txt", os.O_RDONLY, 0)

	buf := make([]byte, 10)
	_, err := f.Read(buf)
	if err == nil {
		t.Error("Expected error from Read")
	}
	if err.Error() != testErr.Error() {
		t.Errorf("Error = %q, want %q", err.Error(), testErr.Error())
	}
}

func TestErrorPropagation_PutObject(t *testing.T) {
	fs, mock := newTestFS()
	testErr := errors.New("PutObject access denied")
	mock.PutObjectErr = testErr

	f, _ := fs.OpenFile("file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("data"))

	err := f.Close()
	if err == nil {
		t.Error("Expected error from Close")
	}
	if err.Error() != testErr.Error() {
		t.Errorf("Error = %q, want %q", err.Error(), testErr.Error())
	}
}

func TestErrorPropagation_HeadObject(t *testing.T) {
	fs, mock := newTestFS()
	testErr := errors.New("HeadObject not found")
	mock.HeadObjectErr = testErr

	_, err := fs.Stat("file.txt")
	if err == nil {
		t.Error("Expected error from Stat")
	}
	if err.Error() != testErr.Error() {
		t.Errorf("Error = %q, want %q", err.Error(), testErr.Error())
	}
}

func TestErrorPropagation_ListObjects(t *testing.T) {
	fs, mock := newTestFS()
	testErr := errors.New("ListObjects throttled")
	mock.ListObjectsErr = testErr

	f, _ := fs.OpenFile("dir", os.O_RDONLY, 0)

	_, err := f.Readdir(-1)
	if err == nil {
		t.Error("Expected error from Readdir")
	}
	if err.Error() != testErr.Error() {
		t.Errorf("Error = %q, want %q", err.Error(), testErr.Error())
	}
}

func TestErrorPropagation_DeleteObject(t *testing.T) {
	fs, mock := newTestFS()
	testErr := errors.New("DeleteObject access denied")
	mock.DeleteObjectErr = testErr

	err := fs.Remove("file.txt")
	if err == nil {
		t.Error("Expected error from Remove")
	}
	if err.Error() != testErr.Error() {
		t.Errorf("Error = %q, want %q", err.Error(), testErr.Error())
	}
}

func TestErrorPropagation_CopyObject(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("src.txt", []byte("data"))
	testErr := errors.New("CopyObject region mismatch")
	mock.CopyObjectErr = testErr

	err := fs.Rename("src.txt", "dst.txt")
	if err == nil {
		t.Error("Expected error from Rename")
	}
	if err.Error() != testErr.Error() {
		t.Errorf("Error = %q, want %q", err.Error(), testErr.Error())
	}
}

// --- NewWithClient Tests ---

func TestNewWithClient(t *testing.T) {
	mock := NewMockS3Client()
	fs := NewWithClient(mock, "my-bucket")

	if fs == nil {
		t.Fatal("NewWithClient returned nil")
	}

	// Verify we can use the filesystem
	mock.PutTestObject("test.txt", []byte("hello"))

	info, err := fs.Stat("test.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Size() != 5 {
		t.Errorf("Size = %d, want 5", info.Size())
	}
}

func TestNewWithClientAndContext(t *testing.T) {
	mock := NewMockS3Client()
	ctx := context.Background()
	fs := NewWithClientAndContext(ctx, mock, "ctx-bucket")

	if fs == nil {
		t.Fatal("NewWithClientAndContext returned nil")
	}

	// Verify we can use the filesystem
	f, err := fs.OpenFile("ctx-test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	f.Write([]byte("context test"))
	f.Close()

	if !mock.HasObject("ctx-test.txt") {
		t.Error("Object should exist after write")
	}
}

// --- Mock Client Tests ---

func TestMockClient_Reset(t *testing.T) {
	mock := NewMockS3Client()
	mock.PutTestObject("file.txt", []byte("data"))
	mock.GetObjectErr = errors.New("error")

	mock.Reset()

	if mock.ObjectCount() != 0 {
		t.Error("Reset should clear all objects")
	}
	if mock.GetObjectErr != nil {
		t.Error("Reset should clear error injection")
	}
}

func TestMockClient_CallTracking(t *testing.T) {
	mock := NewMockS3Client()
	fs := NewWithClient(mock, "test")

	mock.PutTestObject("test.txt", []byte("data"))

	// Perform various operations
	fs.Stat("test.txt")
	fs.Stat("other.txt") // Will fail but still tracked

	f, _ := fs.OpenFile("test.txt", os.O_RDONLY, 0)
	f.Read(make([]byte, 10))

	if len(mock.HeadObjectCalls) != 2 {
		t.Errorf("HeadObjectCalls = %d, want 2", len(mock.HeadObjectCalls))
	}

	if len(mock.GetObjectCalls) != 1 {
		t.Errorf("GetObjectCalls = %d, want 1", len(mock.GetObjectCalls))
	}
}

// --- Large File Tests ---

func TestLargeWrite(t *testing.T) {
	fs, mock := newTestFS()

	f, _ := fs.OpenFile("large.bin", os.O_CREATE|os.O_WRONLY, 0644)

	// Write 1MB
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	n, err := f.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}

	err = f.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify
	stored, ok := mock.GetTestObject("large.bin")
	if !ok {
		t.Fatal("Large file not stored")
	}
	if len(stored) != len(data) {
		t.Errorf("Stored size = %d, want %d", len(stored), len(data))
	}
}

// --- Edge Cases for fileInfo ---

func TestFileInfo_Directory(t *testing.T) {
	fi := &fileInfo{
		name:  "mydir/",
		size:  0,
		isDir: true,
	}

	if !fi.IsDir() {
		t.Error("IsDir should return true")
	}
	if fi.Mode() != 0644 {
		t.Errorf("Mode = %o, want 0644", fi.Mode())
	}
}

func TestFileInfo_ZeroSize(t *testing.T) {
	fi := &fileInfo{
		name: "empty.txt",
		size: 0,
	}

	if fi.Size() != 0 {
		t.Errorf("Size = %d, want 0", fi.Size())
	}
}

func TestFileInfo_LargeSize(t *testing.T) {
	fi := &fileInfo{
		name: "large.bin",
		size: 5 * 1024 * 1024 * 1024, // 5GB
	}

	if fi.Size() != 5*1024*1024*1024 {
		t.Errorf("Size = %d, want 5GB", fi.Size())
	}
}
