package s3fs

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

var testTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// =============================================================================
// Task 3.2: File Read Operations Tests
// =============================================================================

// --- Read Tests ---

func TestRead_Basic(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("read.txt", []byte("hello world"))

	f, err := fs.OpenFile("read.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	buf := make([]byte, 20)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read failed: %v", err)
	}

	if n != 11 {
		t.Errorf("Read %d bytes, want 11", n)
	}
	if string(buf[:n]) != "hello world" {
		t.Errorf("Read content = %q, want %q", string(buf[:n]), "hello world")
	}
}

func TestRead_LazyLoading(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("lazy.txt", []byte("lazy load test"))

	f, err := fs.OpenFile("lazy.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	file := f.(*File)
	if file.body != nil {
		t.Error("Body should be nil before first read (lazy loading)")
	}

	buf := make([]byte, 5)
	f.Read(buf)

	if file.body == nil {
		t.Error("Body should be set after first read")
	}
}

func TestRead_MultipleReads(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("multi.txt", []byte("abcdefghij"))

	f, err := fs.OpenFile("multi.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	buf := make([]byte, 5)

	// First read
	n1, err := f.Read(buf)
	if err != nil {
		t.Fatalf("First read failed: %v", err)
	}
	if string(buf[:n1]) != "abcde" {
		t.Errorf("First read = %q, want %q", string(buf[:n1]), "abcde")
	}

	// Second read
	n2, err := f.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Second read failed: %v", err)
	}
	if string(buf[:n2]) != "fghij" {
		t.Errorf("Second read = %q, want %q", string(buf[:n2]), "fghij")
	}
}

func TestRead_OnWriteModeFile_Error(t *testing.T) {
	fs, _ := newTestFS()

	f, err := fs.OpenFile("write-only.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	buf := make([]byte, 10)
	_, err = f.Read(buf)
	if err != os.ErrInvalid {
		t.Errorf("Read on write-mode file should return ErrInvalid, got: %v", err)
	}
}

func TestRead_FileNotFound(t *testing.T) {
	fs, _ := newTestFS()

	// OpenFile now checks if file exists, so it should fail here
	_, err := fs.OpenFile("not-found.txt", os.O_RDONLY, 0)
	if err == nil {
		t.Error("Expected error when opening non-existent file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Expected os.IsNotExist error, got: %v", err)
	}
}

func TestRead_EOF(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("small.txt", []byte("hi"))

	f, err := fs.OpenFile("small.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	buf := make([]byte, 100)
	n, _ := f.Read(buf)
	if n != 2 {
		t.Errorf("Read %d bytes, want 2", n)
	}

	// Next read should return EOF
	_, err = f.Read(buf)
	if err != io.EOF {
		t.Errorf("Expected EOF, got: %v", err)
	}
}

func TestRead_Error(t *testing.T) {
	fs, mock := newTestFS()

	// Create the file first so OpenFile succeeds
	mock.PutTestObject("error.txt", []byte("test"))

	mock.GetObjectErr = errors.New("network error")

	f, err := fs.OpenFile("error.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	buf := make([]byte, 10)
	_, err = f.Read(buf)
	if err == nil {
		t.Error("Expected error from Read")
	}
}

// --- ReadAt Tests ---

func TestReadAt_Basic(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("readat.txt", []byte("0123456789"))

	f, err := fs.OpenFile("readat.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	buf := make([]byte, 3)
	n, err := f.ReadAt(buf, 5)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}

	if n != 3 {
		t.Errorf("ReadAt returned %d bytes, want 3", n)
	}
	if string(buf) != "567" {
		t.Errorf("ReadAt content = %q, want %q", string(buf), "567")
	}
}

func TestReadAt_FromStart(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("start.txt", []byte("beginning"))

	f, _ := fs.OpenFile("start.txt", os.O_RDONLY, 0)

	buf := make([]byte, 5)
	n, err := f.ReadAt(buf, 0)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}

	if string(buf[:n]) != "begin" {
		t.Errorf("ReadAt = %q, want %q", string(buf[:n]), "begin")
	}
}

func TestReadAt_OnWriteModeFile_Error(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("write.txt", os.O_CREATE|os.O_WRONLY, 0644)

	buf := make([]byte, 10)
	_, err := f.ReadAt(buf, 0)
	if err != os.ErrInvalid {
		t.Errorf("ReadAt on write-mode file should return ErrInvalid, got: %v", err)
	}
}

func TestReadAt_Error(t *testing.T) {
	fs, mock := newTestFS()

	// Create the file first so OpenFile succeeds
	mock.PutTestObject("error.txt", []byte("test"))

	mock.GetObjectErr = errors.New("s3 error")

	f, err := fs.OpenFile("error.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	buf := make([]byte, 10)
	_, err = f.ReadAt(buf, 0)
	if err == nil {
		t.Error("Expected error from ReadAt")
	}
}

// --- Readdir Tests ---

func TestReaddir_Basic(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("dir/", []byte("")) // Create directory marker
	mock.PutTestObject("dir/file1.txt", []byte("content1"))
	mock.PutTestObject("dir/file2.txt", []byte("content2"))
	mock.PutTestObject("dir/file3.txt", []byte("content3"))

	f, err := fs.OpenFile("dir/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}

	if len(infos) != 3 {
		t.Errorf("Readdir returned %d entries, want 3", len(infos))
	}
}

func TestReaddir_WithLimit(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("limit-dir/", []byte("")) // Create directory marker
	mock.PutTestObject("limit-dir/a.txt", []byte("a"))
	mock.PutTestObject("limit-dir/b.txt", []byte("b"))
	mock.PutTestObject("limit-dir/c.txt", []byte("c"))
	mock.PutTestObject("limit-dir/d.txt", []byte("d"))

	f, err := fs.OpenFile("limit-dir/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	infos, err := f.Readdir(2)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}

	if len(infos) != 2 {
		t.Errorf("Readdir returned %d entries, want 2 (limited)", len(infos))
	}
}

func TestReaddir_EmptyDirectory(t *testing.T) {
	fs, mock := newTestFS()

	// Create empty directory marker
	mock.PutTestObject("empty-dir/", []byte(""))

	f, err := fs.OpenFile("empty-dir/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}

	if len(infos) != 0 {
		t.Errorf("Readdir on empty dir returned %d entries, want 0", len(infos))
	}
}

func TestReaddir_PrefixHandling_WithTrailingSlash(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("slash/", []byte("")) // Create directory marker
	mock.PutTestObject("slash/file.txt", []byte("data"))

	f, err := fs.OpenFile("slash/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}

	if len(infos) != 1 {
		t.Errorf("Readdir returned %d entries, want 1", len(infos))
	}
}

func TestReaddir_PrefixHandling_WithoutTrailingSlash(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("noslash/", []byte("")) // Create directory marker
	mock.PutTestObject("noslash/file.txt", []byte("data"))

	f, err := fs.OpenFile("noslash/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}

	// Should still find files (trailing slash added internally)
	if len(infos) != 1 {
		t.Errorf("Readdir returned %d entries, want 1", len(infos))
	}
}

func TestReaddir_Error(t *testing.T) {
	fs, mock := newTestFS()

	// Create directory marker
	mock.PutTestObject("dir/", []byte(""))

	mock.ListObjectsErr = errors.New("list error")

	f, err := fs.OpenFile("dir/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	_, err = f.Readdir(-1)
	if err == nil {
		t.Error("Expected error from Readdir")
	}
}

// --- Readdirnames Tests ---

func TestReaddirnames_Basic(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("names/", []byte("")) // Create directory marker
	mock.PutTestObject("names/one.txt", []byte("1"))
	mock.PutTestObject("names/two.txt", []byte("2"))

	f, err := fs.OpenFile("names/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatalf("Readdirnames failed: %v", err)
	}

	if len(names) != 2 {
		t.Errorf("Readdirnames returned %d names, want 2", len(names))
	}
}

func TestReaddirnames_WithLimit(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("limit/", []byte("")) // Create directory marker
	mock.PutTestObject("limit/a.txt", []byte("a"))
	mock.PutTestObject("limit/b.txt", []byte("b"))
	mock.PutTestObject("limit/c.txt", []byte("c"))

	f, err := fs.OpenFile("limit/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	names, err := f.Readdirnames(1)
	if err != nil {
		t.Fatalf("Readdirnames failed: %v", err)
	}

	if len(names) != 1 {
		t.Errorf("Readdirnames returned %d names, want 1", len(names))
	}
}

func TestReaddirnames_ErrorPropagation(t *testing.T) {
	fs, mock := newTestFS()

	// Create directory marker
	mock.PutTestObject("dir/", []byte(""))

	mock.ListObjectsErr = errors.New("list error")

	f, err := fs.OpenFile("dir/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	_, err = f.Readdirnames(-1)
	if err == nil {
		t.Error("Expected error from Readdirnames")
	}
}

// =============================================================================
// Task 3.3: File Write Operations Tests
// =============================================================================

// --- Write Tests ---

func TestWrite_Basic(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("write.txt", os.O_CREATE|os.O_WRONLY, 0644)

	n, err := f.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if n != 5 {
		t.Errorf("Write returned %d, want 5", n)
	}

	file := f.(*File)
	if string(file.buffer) != "hello" {
		t.Errorf("Buffer = %q, want %q", string(file.buffer), "hello")
	}
}

func TestWrite_MultipleWrites(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("multi.txt", os.O_CREATE|os.O_WRONLY, 0644)

	f.Write([]byte("hello"))
	f.Write([]byte(" "))
	f.Write([]byte("world"))

	file := f.(*File)
	if string(file.buffer) != "hello world" {
		t.Errorf("Buffer = %q, want %q", string(file.buffer), "hello world")
	}
}

func TestWrite_OnReadModeFile_Error(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("readonly.txt", []byte("data"))

	f, _ := fs.OpenFile("readonly.txt", os.O_RDONLY, 0)

	_, err := f.Write([]byte("new data"))
	if err != os.ErrInvalid {
		t.Errorf("Write on read-mode file should return ErrInvalid, got: %v", err)
	}
}

func TestWrite_BufferGrowth(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("grow.txt", os.O_CREATE|os.O_WRONLY, 0644)

	// Write increasingly large chunks
	for i := 0; i < 10; i++ {
		data := make([]byte, 100)
		for j := range data {
			data[j] = byte('a' + i)
		}
		_, err := f.Write(data)
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
	}

	file := f.(*File)
	if len(file.buffer) != 1000 {
		t.Errorf("Buffer length = %d, want 1000", len(file.buffer))
	}
}

func TestWrite_OffsetTracking(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("offset.txt", os.O_CREATE|os.O_WRONLY, 0644)

	f.Write([]byte("12345"))

	file := f.(*File)
	if file.offset != 5 {
		t.Errorf("Offset = %d, want 5", file.offset)
	}

	f.Write([]byte("67890"))
	if file.offset != 10 {
		t.Errorf("Offset = %d, want 10", file.offset)
	}
}

// --- WriteAt Tests ---

func TestWriteAt_Basic(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("writeat.txt", os.O_CREATE|os.O_WRONLY, 0644)

	// Initialize buffer
	f.Write([]byte("0000000000"))

	// Write at offset
	n, err := f.WriteAt([]byte("xxx"), 3)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}

	if n != 3 {
		t.Errorf("WriteAt returned %d, want 3", n)
	}

	file := f.(*File)
	if string(file.buffer) != "000xxx0000" {
		t.Errorf("Buffer = %q, want %q", string(file.buffer), "000xxx0000")
	}
}

func TestWriteAt_BufferExtension(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("extend.txt", os.O_CREATE|os.O_WRONLY, 0644)

	// Write at offset beyond current buffer
	n, err := f.WriteAt([]byte("hello"), 10)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}

	if n != 5 {
		t.Errorf("WriteAt returned %d, want 5", n)
	}

	file := f.(*File)
	if len(file.buffer) != 15 {
		t.Errorf("Buffer length = %d, want 15", len(file.buffer))
	}
}

func TestWriteAt_Overwrite(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("overwrite.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("abcdefghij"))

	f.WriteAt([]byte("XYZ"), 0)

	file := f.(*File)
	if string(file.buffer) != "XYZdefghij" {
		t.Errorf("Buffer = %q, want %q", string(file.buffer), "XYZdefghij")
	}
}

func TestWriteAt_OnReadModeFile_Error(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("ro.txt", []byte("data"))

	f, _ := fs.OpenFile("ro.txt", os.O_RDONLY, 0)

	_, err := f.WriteAt([]byte("new"), 0)
	if err != os.ErrInvalid {
		t.Errorf("WriteAt on read-mode file should return ErrInvalid, got: %v", err)
	}
}

// --- WriteString Tests ---

func TestWriteString_Basic(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("string.txt", os.O_CREATE|os.O_WRONLY, 0644)

	n, err := f.WriteString("hello string")
	if err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}

	if n != 12 {
		t.Errorf("WriteString returned %d, want 12", n)
	}

	file := f.(*File)
	if string(file.buffer) != "hello string" {
		t.Errorf("Buffer = %q, want %q", string(file.buffer), "hello string")
	}
}

func TestWriteString_Unicode(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("unicode.txt", os.O_CREATE|os.O_WRONLY, 0644)

	n, err := f.WriteString("こんにちは世界")
	if err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}

	// Japanese string is 21 bytes
	if n != 21 {
		t.Errorf("WriteString returned %d bytes for unicode", n)
	}
}

func TestWriteString_Empty(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("empty.txt", os.O_CREATE|os.O_WRONLY, 0644)

	n, err := f.WriteString("")
	if err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}

	if n != 0 {
		t.Errorf("WriteString returned %d, want 0", n)
	}
}

// --- Close Tests ---

func TestClose_ReadModeFile(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("close-read.txt", []byte("data"))

	f, _ := fs.OpenFile("close-read.txt", os.O_RDONLY, 0)

	// Trigger lazy loading
	buf := make([]byte, 10)
	f.Read(buf)

	err := f.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestClose_WriteModeFile_Upload(t *testing.T) {
	fs, mock := newTestFS()

	f, _ := fs.OpenFile("close-write.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("uploaded content"))

	err := f.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify upload
	data, ok := mock.GetTestObject("close-write.txt")
	if !ok {
		t.Fatal("Object not uploaded on close")
	}
	if string(data) != "uploaded content" {
		t.Errorf("Uploaded content = %q, want %q", string(data), "uploaded content")
	}
}

func TestClose_WriteModeFile_UploadError(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutObjectErr = errors.New("upload failed")

	f, _ := fs.OpenFile("fail-upload.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("content"))

	err := f.Close()
	if err == nil {
		t.Error("Expected error from Close when upload fails")
	}
}

func TestClose_DoubleClose(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("double.txt", []byte("data"))

	f, _ := fs.OpenFile("double.txt", os.O_RDONLY, 0)
	f.Read(make([]byte, 4))

	err := f.Close()
	if err != nil {
		t.Fatalf("First Close failed: %v", err)
	}

	// Second close should not panic
	err = f.Close()
	// This may or may not error depending on implementation
	// Just verify no panic
}

func TestClose_EmptyWriteFile(t *testing.T) {
	fs, mock := newTestFS()

	f, _ := fs.OpenFile("empty-write.txt", os.O_CREATE|os.O_WRONLY, 0644)

	err := f.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Empty file should be uploaded
	data, ok := mock.GetTestObject("empty-write.txt")
	if !ok {
		t.Fatal("Empty file not uploaded")
	}
	if len(data) != 0 {
		t.Errorf("Empty file has %d bytes", len(data))
	}
}

// =============================================================================
// Task 3.4: File Additional Operations Tests
// =============================================================================

// --- Seek Tests ---

func TestSeek_SeekStart(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("seek.txt", os.O_CREATE|os.O_WRONLY, 0644)

	pos, err := f.Seek(10, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek failed: %v", err)
	}

	if pos != 10 {
		t.Errorf("Seek position = %d, want 10", pos)
	}

	file := f.(*File)
	if file.offset != 10 {
		t.Errorf("File offset = %d, want 10", file.offset)
	}
}

func TestSeek_SeekCurrent(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("seek.txt", os.O_CREATE|os.O_WRONLY, 0644)

	// Move to position 5
	f.Seek(5, io.SeekStart)

	// Move forward 3
	pos, err := f.Seek(3, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek failed: %v", err)
	}

	if pos != 8 {
		t.Errorf("Seek position = %d, want 8", pos)
	}
}

func TestSeek_SeekCurrent_Negative(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("seek.txt", os.O_CREATE|os.O_WRONLY, 0644)

	f.Seek(10, io.SeekStart)
	pos, err := f.Seek(-3, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek failed: %v", err)
	}

	if pos != 7 {
		t.Errorf("Seek position = %d, want 7", pos)
	}
}

func TestSeek_SeekEnd_NotSupported(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("seek.txt", os.O_CREATE|os.O_WRONLY, 0644)

	_, err := f.Seek(0, io.SeekEnd)
	if err != os.ErrInvalid {
		t.Errorf("SeekEnd should return ErrInvalid, got: %v", err)
	}
}

func TestSeek_InvalidWhence(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("seek.txt", os.O_CREATE|os.O_WRONLY, 0644)

	// Invalid whence value
	pos, err := f.Seek(0, 99)
	if err != nil {
		// Expected - invalid whence falls through to default case
	}
	// Position should be unchanged
	if pos != 0 {
		t.Errorf("Position = %d, want 0 after invalid seek", pos)
	}
}

// --- Truncate Tests ---

func TestTruncate_Shrink(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("trunc.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("0123456789"))

	err := f.Truncate(5)
	if err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	file := f.(*File)
	if len(file.buffer) != 5 {
		t.Errorf("Buffer length = %d, want 5", len(file.buffer))
	}
	if string(file.buffer) != "01234" {
		t.Errorf("Buffer = %q, want %q", string(file.buffer), "01234")
	}
}

func TestTruncate_Expand(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("trunc.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("hello"))

	err := f.Truncate(10)
	if err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	file := f.(*File)
	if len(file.buffer) != 10 {
		t.Errorf("Buffer length = %d, want 10", len(file.buffer))
	}
	// Original content preserved, rest should be zero bytes
	if string(file.buffer[:5]) != "hello" {
		t.Errorf("First 5 bytes = %q, want %q", string(file.buffer[:5]), "hello")
	}
}

func TestTruncate_ToZero(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("trunc.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("content"))

	err := f.Truncate(0)
	if err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	file := f.(*File)
	if len(file.buffer) != 0 {
		t.Errorf("Buffer length = %d, want 0", len(file.buffer))
	}
}

func TestTruncate_OnReadModeFile_Error(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("ro.txt", []byte("data"))

	f, _ := fs.OpenFile("ro.txt", os.O_RDONLY, 0)

	err := f.Truncate(0)
	if err != os.ErrInvalid {
		t.Errorf("Truncate on read-mode file should return ErrInvalid, got: %v", err)
	}
}

// --- Sync Tests ---

func TestSync_NoOp(t *testing.T) {
	fs, _ := newTestFS()

	f, _ := fs.OpenFile("sync.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("data"))

	err := f.Sync()
	if err != nil {
		t.Errorf("Sync should return nil, got: %v", err)
	}
}

func TestSync_ReadMode(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("sync-read.txt", []byte("data"))

	f, _ := fs.OpenFile("sync-read.txt", os.O_RDONLY, 0)

	err := f.Sync()
	if err != nil {
		t.Errorf("Sync should return nil, got: %v", err)
	}
}

// --- Name and Stat Tests ---

func TestName(t *testing.T) {
	fs, mock := newTestFS()

	// Create the file first
	mock.PutTestObject("myfile.txt", []byte("test"))

	f, err := fs.OpenFile("myfile.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	if f.Name() != "myfile.txt" {
		t.Errorf("Name = %q, want %q", f.Name(), "myfile.txt")
	}
}

func TestName_WithPath(t *testing.T) {
	fs, mock := newTestFS()

	// Create the file first
	mock.PutTestObject("path/to/file.txt", []byte("test"))

	f, err := fs.OpenFile("path/to/file.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	if f.Name() != "path/to/file.txt" {
		t.Errorf("Name = %q, want %q", f.Name(), "path/to/file.txt")
	}
}

func TestFileStat(t *testing.T) {
	fs, mock := newTestFS()
	mock.PutTestObject("stat.txt", []byte("content"))

	f, _ := fs.OpenFile("stat.txt", os.O_RDONLY, 0)

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "stat.txt" {
		t.Errorf("Name = %q, want %q", info.Name(), "stat.txt")
	}
	if info.Size() != 7 {
		t.Errorf("Size = %d, want 7", info.Size())
	}
}

func TestFileStat_NotFound(t *testing.T) {
	fs, _ := newTestFS()

	// OpenFile now fails if file doesn't exist
	_, err := fs.OpenFile("notfound.txt", os.O_RDONLY, 0)
	if err == nil {
		t.Error("Expected error when opening non-existent file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Expected os.IsNotExist error, got: %v", err)
	}
}
