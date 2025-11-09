package s3fs

import (
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// MkdirAll creates a directory path and all parent directories if they don't exist.
// It's similar to os.MkdirAll but for S3. Since S3 doesn't have real directories,
// this creates zero-byte marker objects for each directory level.
func (fs *FileSystem) MkdirAll(name string, perm os.FileMode) error {
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." {
		return nil
	}

	// Ensure it ends with /
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}

	// Create all parent directories
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	for i := range parts {
		dir := strings.Join(parts[:i+1], "/") + "/"

		// Check if directory already exists
		exists, err := fs.Exists(dir)
		if err != nil {
			return err
		}
		if exists {
			continue
		}

		// Create the directory marker
		if err := fs.Mkdir(dir, perm); err != nil {
			return err
		}
	}

	return nil
}

// RemoveAll removes a path and all its children.
// For files, it's equivalent to Remove. For directories, it deletes all objects
// with the directory as a prefix.
func (fs *FileSystem) RemoveAll(name string) error {
	name = strings.TrimPrefix(name, "/")

	// Check if it's a directory
	if !strings.HasSuffix(name, "/") {
		// Try as directory first
		dirName := name + "/"
		isDir, err := fs.isDirectory(dirName)
		if err == nil && isDir {
			name = dirName
		}
	}

	// If it's a directory, delete all objects with this prefix
	if strings.HasSuffix(name, "/") {
		return fs.removePrefix(name)
	}

	// Otherwise, just remove the single file
	return fs.Remove(name)
}

// Exists checks if a file or directory exists in S3.
func (fs *FileSystem) Exists(name string) (bool, error) {
	_, err := fs.Stat(name)
	if err != nil {
		// Check if it's a "not found" error
		// In S3, we consider the object doesn't exist if HeadObject fails
		return false, nil
	}
	return true, nil
}

// isDirectory checks if a path is a directory (has objects with it as prefix).
func (fs *FileSystem) isDirectory(name string) (bool, error) {
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}

	output, err := fs.client.ListObjectsV2(fs.ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(fs.bucket),
		Prefix:  aws.String(name),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return false, wrapError("isDirectory", name, err)
	}

	return len(output.Contents) > 0, nil
}

// removePrefix removes all objects with the given prefix.
func (fs *FileSystem) removePrefix(prefix string) error {
	var continuationToken *string

	for {
		output, err := fs.client.ListObjectsV2(fs.ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(fs.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return wrapError("removePrefix", prefix, err)
		}

		// Delete all objects in this batch
		for _, obj := range output.Contents {
			if err := fs.Remove(aws.ToString(obj.Key)); err != nil {
				return err
			}
		}

		// Check if there are more objects
		if !*output.IsTruncated {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	return nil
}

// Walk walks the file tree rooted at root, calling fn for each file or directory.
// This is similar to filepath.Walk but for S3.
func (fs *FileSystem) Walk(root string, fn func(path string, info os.FileInfo, err error) error) error {
	root = strings.TrimPrefix(root, "/")

	// Ensure root has trailing slash if it's meant to be a directory
	if root != "" && !strings.HasSuffix(root, "/") {
		// Check if it's a file or directory
		info, err := fs.Stat(root)
		if err == nil {
			// It's a file, call fn and return
			if !info.IsDir() {
				return fn(root, info, nil)
			}
			root += "/"
		} else {
			root += "/"
		}
	}

	var continuationToken *string
	visited := make(map[string]bool)

	for {
		output, err := fs.client.ListObjectsV2(fs.ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(fs.bucket),
			Prefix:            aws.String(root),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return fn(root, nil, wrapError("Walk", root, err))
		}

		// Process each object
		for _, obj := range output.Contents {
			key := aws.ToString(obj.Key)

			// Skip if already visited
			if visited[key] {
				continue
			}
			visited[key] = true

			// Create file info
			info := &fileInfo{
				name:    path.Base(key),
				size:    *obj.Size,
				modTime: *obj.LastModified,
				isDir:   strings.HasSuffix(key, "/"),
			}

			// Call the walk function
			if err := fn(key, info, nil); err != nil {
				return err
			}
		}

		// Check if there are more objects
		if !*output.IsTruncated {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	return nil
}
