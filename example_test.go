package s3fs_test

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/absfs/s3fs"
	"github.com/aws/aws-sdk-go-v2/config"
)

func ExampleNew() {
	// Create a new S3 filesystem
	fs, err := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Use the filesystem
	_ = fs
}

func ExampleNew_customConfig() {
	// Load custom AWS config
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create S3 filesystem with custom config
	fs, err := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Config: &cfg,
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = fs
}

func ExampleFileSystem_OpenFile() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Open a file for writing
	f, err := fs.OpenFile("path/to/file.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Write some data
	_, err = f.Write([]byte("Hello, S3!"))
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleFileSystem_OpenFile_read() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Open a file for reading
	f, err := fs.OpenFile("path/to/file.txt", os.O_RDONLY, 0)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	// Read the data
	buf := make([]byte, 1024)
	n, err := f.Read(buf)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Read %d bytes\n", n)
}

func ExampleFileSystem_Mkdir() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Create a directory
	err := fs.Mkdir("path/to/dir", 0755)
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleFileSystem_MkdirAll() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Create a directory and all parent directories
	err := fs.MkdirAll("path/to/nested/dir", 0755)
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleFileSystem_Remove() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Remove a file
	err := fs.Remove("path/to/file.txt")
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleFileSystem_RemoveAll() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Remove a directory and all its contents
	err := fs.RemoveAll("path/to/dir")
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleFileSystem_Rename() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Rename a file
	err := fs.Rename("old/path.txt", "new/path.txt")
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleFileSystem_Stat() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Get file information
	info, err := fs.Stat("path/to/file.txt")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Name: %s\n", info.Name())
	fmt.Printf("Size: %d bytes\n", info.Size())
	fmt.Printf("Modified: %s\n", info.ModTime())
}

func ExampleFileSystem_Exists() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Check if a file exists
	exists, err := fs.Exists("path/to/file.txt")
	if err != nil {
		log.Fatal(err)
	}

	if exists {
		fmt.Println("File exists")
	} else {
		fmt.Println("File does not exist")
	}
}

func ExampleFileSystem_WithContext() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30)
	defer cancel()

	// Use the filesystem with the context
	fsWithCtx := fs.WithContext(ctx)

	// All operations will now use the context
	_ = fsWithCtx
}

func ExampleFileSystem_Walk() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Walk all files in a directory
	err := fs.Walk("path/to/dir", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		fmt.Printf("Found: %s (%d bytes)\n", path, info.Size())
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleFileSystem_NewMultipartUpload() {
	fs, _ := s3fs.New(&s3fs.Config{
		Bucket: "my-bucket",
		Region: "us-east-1",
	})

	// Create a multipart upload for a large file
	upload, err := fs.NewMultipartUpload("path/to/large-file.bin")
	if err != nil {
		log.Fatal(err)
	}

	// Upload parts
	part1 := make([]byte, 10*1024*1024) // 10MB
	if err := upload.UploadPart(part1); err != nil {
		upload.Abort()
		log.Fatal(err)
	}

	part2 := make([]byte, 10*1024*1024) // 10MB
	if err := upload.UploadPart(part2); err != nil {
		upload.Abort()
		log.Fatal(err)
	}

	// Complete the upload
	if err := upload.Complete(); err != nil {
		log.Fatal(err)
	}
}
