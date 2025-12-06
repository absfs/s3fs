//go:build integration

package s3fs

import (
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Integration tests require a local S3-compatible service like MinIO.
//
// To run integration tests:
//   1. Start MinIO: docker run -p 9000:9000 -p 9001:9001 minio/minio server /data --console-address ":9001"
//   2. Set environment variables:
//      - S3_ENDPOINT: http://localhost:9000
//      - S3_BUCKET: test-bucket
//      - S3_ACCESS_KEY: minioadmin (default)
//      - S3_SECRET_KEY: minioadmin (default)
//   3. Create the test bucket: aws --endpoint-url http://localhost:9000 s3 mb s3://test-bucket
//   4. Run tests: go test -tags=integration -v ./...

func getIntegrationConfig(t *testing.T) *FileSystem {
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}

	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		bucket = "test-bucket"
	}

	accessKey := os.Getenv("S3_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}

	secretKey := os.Getenv("S3_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	cfg, err := config.LoadDefaultConfig(t.Context(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	return NewWithClient(client, bucket)
}

func TestIntegration_OpenFile(t *testing.T) {
	fs := getIntegrationConfig(t)

	// Write a file
	f, err := fs.OpenFile("integration-test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile (write) failed: %v", err)
	}

	_, err = f.Write([]byte("Hello, Integration Test!"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	err = f.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read the file back
	f, err = fs.OpenFile("integration-test.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile (read) failed: %v", err)
	}

	buf := make([]byte, 100)
	n, err := f.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if string(buf[:n]) != "Hello, Integration Test!" {
		t.Errorf("Read content = %q, want %q", string(buf[:n]), "Hello, Integration Test!")
	}

	// Cleanup
	err = fs.Remove("integration-test.txt")
	if err != nil {
		t.Errorf("Remove failed: %v", err)
	}
}

func TestIntegration_Stat(t *testing.T) {
	fs := getIntegrationConfig(t)

	// Create a test file
	f, err := fs.OpenFile("stat-test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	f.Write([]byte("test content"))
	f.Close()

	// Stat the file
	info, err := fs.Stat("stat-test.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "stat-test.txt" {
		t.Errorf("Name = %q, want %q", info.Name(), "stat-test.txt")
	}

	if info.Size() != 12 {
		t.Errorf("Size = %d, want 12", info.Size())
	}

	// Cleanup
	fs.Remove("stat-test.txt")
}

func TestIntegration_Mkdir(t *testing.T) {
	fs := getIntegrationConfig(t)

	err := fs.Mkdir("test-dir", 0755)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	// Verify directory exists
	info, err := fs.Stat("test-dir/")
	if err != nil {
		t.Fatalf("Stat on directory failed: %v", err)
	}

	if !info.IsDir() {
		t.Errorf("IsDir = false, want true")
	}

	// Cleanup
	fs.Remove("test-dir/")
}

func TestIntegration_Rename(t *testing.T) {
	fs := getIntegrationConfig(t)

	// Create original file
	f, err := fs.OpenFile("rename-original.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	f.Write([]byte("rename test"))
	f.Close()

	// Rename
	err = fs.Rename("rename-original.txt", "rename-new.txt")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	// Verify new file exists
	info, err := fs.Stat("rename-new.txt")
	if err != nil {
		t.Fatalf("Stat on new file failed: %v", err)
	}

	if info.Size() != 11 {
		t.Errorf("Size = %d, want 11", info.Size())
	}

	// Verify old file is gone
	_, err = fs.Stat("rename-original.txt")
	if err == nil {
		t.Errorf("Old file still exists after rename")
	}

	// Cleanup
	fs.Remove("rename-new.txt")
}

func TestIntegration_Readdir(t *testing.T) {
	fs := getIntegrationConfig(t)

	// Create test directory with files
	fs.Mkdir("readdir-test", 0755)

	for i := 0; i < 3; i++ {
		f, _ := fs.OpenFile("readdir-test/file"+string(rune('0'+i))+".txt", os.O_CREATE|os.O_WRONLY, 0644)
		f.Write([]byte("content"))
		f.Close()
	}

	// Open directory and list contents
	dir, err := fs.OpenFile("readdir-test", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile on directory failed: %v", err)
	}

	entries, err := dir.Readdir(-1)
	if err != nil {
		t.Fatalf("Readdir failed: %v", err)
	}

	// Should have at least 3 files (plus possibly the directory marker)
	if len(entries) < 3 {
		t.Errorf("Readdir returned %d entries, want at least 3", len(entries))
	}

	// Cleanup
	for i := 0; i < 3; i++ {
		fs.Remove("readdir-test/file" + string(rune('0'+i)) + ".txt")
	}
	fs.Remove("readdir-test/")
}
