package s3fs

import (
	"testing"
)

func TestFile_Name(t *testing.T) {
	f := &File{
		name: "test.txt",
	}

	if f.Name() != "test.txt" {
		t.Errorf("Name() = %v, want test.txt", f.Name())
	}
}

func TestFile_Write(t *testing.T) {
	f := &File{
		writing: true,
		buffer:  []byte{},
	}

	data := []byte("hello world")
	n, err := f.Write(data)
	if err != nil {
		t.Errorf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() = %v, want %v", n, len(data))
	}
	if string(f.buffer) != "hello world" {
		t.Errorf("buffer = %v, want 'hello world'", string(f.buffer))
	}
}

func TestFile_Write_ReadOnly(t *testing.T) {
	f := &File{
		writing: false,
	}

	_, err := f.Write([]byte("test"))
	if err != ErrWriteOnReadFile {
		t.Errorf("Write() on read-only file should return ErrWriteOnReadFile, got %v", err)
	}
}

func TestFile_WriteAt(t *testing.T) {
	f := &File{
		writing: true,
		buffer:  make([]byte, 10),
	}

	data := []byte("test")
	n, err := f.WriteAt(data, 3)
	if err != nil {
		t.Errorf("WriteAt() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("WriteAt() = %v, want %v", n, len(data))
	}
	if string(f.buffer[3:7]) != "test" {
		t.Errorf("buffer[3:7] = %v, want 'test'", string(f.buffer[3:7]))
	}
}

func TestFile_WriteAt_Expand(t *testing.T) {
	f := &File{
		writing: true,
		buffer:  make([]byte, 5),
	}

	data := []byte("test")
	n, err := f.WriteAt(data, 10)
	if err != nil {
		t.Errorf("WriteAt() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("WriteAt() = %v, want %v", n, len(data))
	}
	if len(f.buffer) != 14 {
		t.Errorf("buffer length = %v, want 14", len(f.buffer))
	}
}

func TestFile_WriteString(t *testing.T) {
	f := &File{
		writing: true,
		buffer:  []byte{},
	}

	n, err := f.WriteString("hello")
	if err != nil {
		t.Errorf("WriteString() error = %v", err)
	}
	if n != 5 {
		t.Errorf("WriteString() = %v, want 5", n)
	}
	if string(f.buffer) != "hello" {
		t.Errorf("buffer = %v, want 'hello'", string(f.buffer))
	}
}

func TestFile_Truncate(t *testing.T) {
	f := &File{
		writing: true,
		buffer:  []byte("hello world"),
	}

	err := f.Truncate(5)
	if err != nil {
		t.Errorf("Truncate() error = %v", err)
	}
	if len(f.buffer) != 5 {
		t.Errorf("buffer length = %v, want 5", len(f.buffer))
	}
	if string(f.buffer) != "hello" {
		t.Errorf("buffer = %v, want 'hello'", string(f.buffer))
	}
}

func TestFile_Truncate_Expand(t *testing.T) {
	f := &File{
		writing: true,
		buffer:  []byte("hello"),
	}

	err := f.Truncate(10)
	if err != nil {
		t.Errorf("Truncate() error = %v", err)
	}
	if len(f.buffer) != 10 {
		t.Errorf("buffer length = %v, want 10", len(f.buffer))
	}
}

func TestFile_Truncate_ReadOnly(t *testing.T) {
	f := &File{
		writing: false,
	}

	err := f.Truncate(5)
	if err != ErrWriteOnReadFile {
		t.Errorf("Truncate() on read-only file should return ErrWriteOnReadFile, got %v", err)
	}
}

func TestFile_Seek(t *testing.T) {
	tests := []struct {
		name    string
		offset  int64
		whence  int
		want    int64
		wantErr bool
	}{
		{"SeekStart", 10, 0, 10, false},
		{"SeekCurrent", 5, 1, 5, false},
		{"SeekEnd", 0, 2, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &File{offset: 0}
			got, err := f.Seek(tt.offset, tt.whence)
			if (err != nil) != tt.wantErr {
				t.Errorf("Seek() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Seek() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFile_Sync(t *testing.T) {
	f := &File{}
	if err := f.Sync(); err != nil {
		t.Errorf("Sync() error = %v", err)
	}
}
