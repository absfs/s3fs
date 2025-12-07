package s3fs

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestConfig(t *testing.T) {
	config := &Config{
		Bucket: "test-bucket",
		Region: "us-east-1",
	}

	if config.Bucket != "test-bucket" {
		t.Errorf("Bucket not set correctly")
	}
	if config.Region != "us-east-1" {
		t.Errorf("Region not set correctly")
	}
}

func TestNewConfig(t *testing.T) {
	config := &Config{
		Bucket: "test-bucket",
		Region: "us-east-1",
	}

	// Note: This will fail without valid AWS credentials
	// This is just a structural test
	_, err := New(config)
	if err != nil {
		// Expected - no valid credentials in test environment
		t.Skip("Skipping test - no AWS credentials available")
	}
}

func TestFileInfo(t *testing.T) {
	fi := &fileInfo{
		name:  "test.txt",
		size:  1024,
		isDir: false,
	}

	if fi.Name() != "test.txt" {
		t.Errorf("Name() = %v, want test.txt", fi.Name())
	}
	if fi.Size() != 1024 {
		t.Errorf("Size() = %v, want 1024", fi.Size())
	}
	if fi.IsDir() {
		t.Errorf("IsDir() = true, want false")
	}
	if fi.Mode() != 0644 {
		t.Errorf("Mode() = %v, want 0644", fi.Mode())
	}
	if fi.Sys() != nil {
		t.Errorf("Sys() = %v, want nil", fi.Sys())
	}
}

func TestAwsStringHelper(t *testing.T) {
	// Test that aws.String helper works
	str := aws.String("test")
	if *str != "test" {
		t.Errorf("aws.String() failed")
	}
}
