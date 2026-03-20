package s3fs

import (
	"context"
	"errors"
	"os"
	"strings"
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
	fs, mock := newTestFS()

	paths := []struct {
		input    string
		cleanKey string
	}{
		{"./current.txt", "current.txt"},
		{"../parent.txt", "parent.txt"},
		{"dir/./file.txt", "dir/file.txt"},
		{"dir/../sibling.txt", "sibling.txt"},
	}

	for _, tc := range paths {
		f, err := fs.OpenFile(tc.input, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			t.Errorf("OpenFile(%q) failed: %v", tc.input, err)
			continue
		}
		f.Close()

		if !mock.HasObject(tc.cleanKey) {
			t.Errorf("OpenFile(%q): expected object at clean key %q", tc.input, tc.cleanKey)
		}
	}
}

func TestSanitizePath_Traversal(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"file.txt", "file.txt", true},
		{"/file.txt", "file.txt", true},
		{"./file.txt", "file.txt", true},
		{"dir/../file.txt", "file.txt", true},
		{"dir/./file.txt", "dir/file.txt", true},
		{"../file.txt", "file.txt", true},
		{"a/b/../c/d", "a/c/d", true},
		{"a/b/c/../../d", "a/d", true},
		{"dir with spaces/file.txt", "dir with spaces/file.txt", true},
		{"", "", true},
	}

	for _, tc := range tests {
		got, err := sanitizePath(tc.input)
		if tc.ok && err != nil {
			t.Errorf("sanitizePath(%q) unexpected error: %v", tc.input, err)
		} else if !tc.ok && err == nil {
			t.Errorf("sanitizePath(%q) expected error, got %q", tc.input, got)
		} else if tc.ok && got != tc.want {
			t.Errorf("sanitizePath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSanitizePath_AllOperations(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("safe.txt", []byte("data"))
	mock.PutTestObject("safe/", []byte(""))

	_, err := fs.Stat("./safe.txt")
	if err != nil {
		t.Errorf("Stat with dot path failed: %v", err)
	}

	_, err = fs.ReadFile("./safe.txt")
	if err != nil {
		t.Errorf("ReadFile with dot path failed: %v", err)
	}

	_, err = fs.ReadDir("./safe")
	if err != nil {
		t.Errorf("ReadDir with dot path failed: %v", err)
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

	// Create the file first so OpenFile succeeds
	mock.PutTestObject("file.txt", []byte("test data"))

	testErr := errors.New("GetObject network error")
	mock.GetObjectErr = testErr

	f, err := fs.OpenFile("file.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	buf := make([]byte, 10)
	_, err = f.Read(buf)
	if err == nil {
		t.Error("Expected error from Read")
	}
	// Error is now wrapped in PathError
	if !strings.Contains(err.Error(), testErr.Error()) {
		t.Errorf("Error = %q, want to contain %q", err.Error(), testErr.Error())
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
	// Error is now wrapped in PathError
	if !strings.Contains(err.Error(), testErr.Error()) {
		t.Errorf("Error = %q, want to contain %q", err.Error(), testErr.Error())
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
	// Error is now wrapped in PathError
	if !strings.Contains(err.Error(), testErr.Error()) {
		t.Errorf("Error = %q, want to contain %q", err.Error(), testErr.Error())
	}
}

func TestErrorPropagation_ListObjects(t *testing.T) {
	fs, mock := newTestFS()

	// Create a directory marker so OpenFile succeeds
	mock.PutTestObject("dir/", []byte(""))

	testErr := errors.New("ListObjects throttled")
	mock.ListObjectsErr = testErr

	f, err := fs.OpenFile("dir/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	_, err = f.Readdir(-1)
	if err == nil {
		t.Error("Expected error from Readdir")
	}
	// Error is now wrapped in PathError
	if !strings.Contains(err.Error(), testErr.Error()) {
		t.Errorf("Error = %q, want to contain %q", err.Error(), testErr.Error())
	}
}

func TestErrorPropagation_DeleteObject(t *testing.T) {
	fs, mock := newTestFS()

	// Create the file first so Remove can check it exists
	mock.PutTestObject("file.txt", []byte("data"))

	testErr := errors.New("DeleteObject access denied")
	mock.DeleteObjectErr = testErr

	err := fs.Remove("file.txt")
	if err == nil {
		t.Error("Expected error from Remove")
	}
	// Error is now wrapped in PathError
	if !strings.Contains(err.Error(), testErr.Error()) {
		t.Errorf("Error = %q, want to contain %q", err.Error(), testErr.Error())
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
	// Error is now wrapped in PathError
	if !strings.Contains(err.Error(), testErr.Error()) {
		t.Errorf("Error = %q, want to contain %q", err.Error(), testErr.Error())
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
	fs.Stat("test.txt")                             // HeadObject call 1
	fs.Stat("other.txt")                            // HeadObject call 2 (will fail), call 3 (checks for directory)
	f, _ := fs.OpenFile("test.txt", os.O_RDONLY, 0) // HeadObject call 4 (verifies file exists)
	f.Read(make([]byte, 10))                        // GetObject call 1

	// Stat now checks for directories when file not found, so we have 4 HeadObject calls
	if len(mock.HeadObjectCalls) != 4 {
		t.Errorf("HeadObjectCalls = %d, want 4", len(mock.HeadObjectCalls))
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

func TestConcurrency_SameFileWrite(t *testing.T) {
	fs, mock := newTestFS()

	f, err := fs.OpenFile("shared.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.Write([]byte("x"))
		}()
	}

	wg.Wait()
	f.Close()

	data, ok := mock.GetTestObject("shared.txt")
	if !ok {
		t.Fatal("File should exist")
	}
	if len(data) != 100 {
		t.Errorf("Expected 100 bytes written, got %d", len(data))
	}
}

// --- O_TRUNC and O_APPEND Tests (Issue #5) ---

func TestOpenFile_O_TRUNC(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("existing.txt", []byte("original content"))

	f, err := fs.OpenFile("existing.txt", os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("OpenFile with O_TRUNC failed: %v", err)
	}

	file := f.(*File)
	if len(file.buffer) != 0 {
		t.Errorf("O_TRUNC buffer should be empty, got %d bytes", len(file.buffer))
	}

	f.Write([]byte("new"))
	f.Close()

	data, _ := mock.GetTestObject("existing.txt")
	if string(data) != "new" {
		t.Errorf("After O_TRUNC write, content = %q, want %q", string(data), "new")
	}
}

func TestOpenFile_O_APPEND(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("existing.txt", []byte("hello"))

	f, err := fs.OpenFile("existing.txt", os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("OpenFile with O_APPEND failed: %v", err)
	}

	file := f.(*File)
	if string(file.buffer) != "hello" {
		t.Errorf("O_APPEND buffer = %q, want %q", string(file.buffer), "hello")
	}
	if !file.append {
		t.Error("O_APPEND should set append flag")
	}
	if file.offset != 5 {
		t.Errorf("O_APPEND offset = %d, want 5", file.offset)
	}

	f.Write([]byte(" world"))
	f.Close()

	data, _ := mock.GetTestObject("existing.txt")
	if string(data) != "hello world" {
		t.Errorf("After O_APPEND write, content = %q, want %q", string(data), "hello world")
	}
}

func TestOpenFile_O_APPEND_NewFile(t *testing.T) {
	fs, mock := newTestFS()

	f, err := fs.OpenFile("new.txt", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		t.Fatalf("OpenFile with O_APPEND|O_CREATE failed: %v", err)
	}

	f.Write([]byte("content"))
	f.Close()

	data, ok := mock.GetTestObject("new.txt")
	if !ok {
		t.Fatal("File should exist after write")
	}
	if string(data) != "content" {
		t.Errorf("Content = %q, want %q", string(data), "content")
	}
}

func TestOpenFile_WriteWithoutTrunc(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("existing.txt", []byte("original"))

	f, err := fs.OpenFile("existing.txt", os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	file := f.(*File)
	if string(file.buffer) != "original" {
		t.Errorf("Buffer should contain existing content %q, got %q", "original", string(file.buffer))
	}
}
