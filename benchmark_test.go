package s3fs

import (
	"testing"
)

func BenchmarkFileWrite(b *testing.B) {
	f := &File{
		writing: true,
		buffer:  make([]byte, 0, 1024),
	}

	data := []byte("hello world")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.buffer = f.buffer[:0]
		_, _ = f.Write(data)
	}
}

func BenchmarkFileWriteAt(b *testing.B) {
	f := &File{
		writing: true,
		buffer:  make([]byte, 1024),
	}

	data := []byte("test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.WriteAt(data, 100)
	}
}

func BenchmarkFileWriteString(b *testing.B) {
	f := &File{
		writing: true,
		buffer:  make([]byte, 0, 1024),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.buffer = f.buffer[:0]
		_, _ = f.WriteString("hello world")
	}
}

func BenchmarkFileTruncate(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f := &File{
			writing: true,
			buffer:  make([]byte, 1024),
		}
		_ = f.Truncate(512)
	}
}

func BenchmarkFileSeek(b *testing.B) {
	f := &File{
		offset: 0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Seek(100, 0)
	}
}

func BenchmarkTrimPrefix(b *testing.B) {
	paths := []string{
		"/test/path/to/file.txt",
		"test/path/to/file.txt",
		"/",
		"",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = trimPrefix(paths[i%len(paths)])
	}
}
