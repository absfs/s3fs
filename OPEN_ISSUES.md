# Open Issues

This document lists all open issues for the s3fs project.

## Critical Issues

### Issue #11: CRITICAL: ReadAt range string conversion completely broken
The ReadAt function at s3file.go:57 has a critical bug that makes all range reads fail. The code converts integers to runes and then to strings, which produces garbage characters instead of numeric strings. For example, `string(rune(100))` produces "d" (Unicode character for code point 100), not "100". This sends malformed HTTP Range headers to S3, causing all range reads to fail. ReadAt is completely non-functional. Any code using range reads will fail.

### Issue #12: CRITICAL: Nil pointer dereferences in Stat and Readdir
Multiple locations dereference AWS SDK pointers without nil checks, which can cause panics. In s3fs.go:146-147 (Stat function) and s3file.go:183-184 (Readdir function), ContentLength, LastModified, Size, and other pointers are dereferenced without nil checks. Panics occur when S3 returns incomplete metadata, which can happen in various scenarios.

### Issue #13: CRITICAL: CopySource format incorrect in Rename
The Rename function at s3fs.go:117 uses path.Join to construct the CopySource parameter, which is incorrect. S3 CopySource requires format `/bucket/key`, but path.Join may produce incorrect separators on different platforms (especially Windows) and doesn't add the leading slash. Rename operations will fail, especially on Windows or with certain path patterns.

### Issue #14: CRITICAL: No path validation or sanitization
No validation or sanitization of file paths before sending to S3. All file operation methods (OpenFile, Mkdir, Remove, Rename, Stat) accept raw user input without validation. This allows potential path traversal attacks if user-controlled input is passed to these methods. Security vulnerability - malicious paths could potentially access unintended S3 objects.

## Major Issues

### Issue #2: MAJOR: Readdir pagination missing - large directories truncated
The Readdir function uses ListObjectsV2 but doesn't handle pagination. S3 returns a maximum of 1000 objects per request, and subsequent pages are silently ignored. Directories with more than 1000 objects are silently truncated, causing data completeness issues.

### Issue #3: MAJOR: Rename operation is non-atomic with no rollback
The Rename function implements rename as Copy then Delete, which is not atomic. If Delete fails after Copy succeeds, the file is duplicated. No rollback mechanism or transactional semantics exist. Race conditions are possible in concurrent scenarios, leading to potential data duplication or loss.

### Issue #4: MAJOR: OpenFile doesn't validate file exists for read operations
OpenFile returns a File object immediately without checking if the file exists when opened for reading. The file existence is only checked on the first Read() call. This deviates from standard os.Open behavior which validates file existence immediately. Errors are delayed until Read(), making error handling inconsistent with standard library.

### Issue #5: MAJOR: Missing O_TRUNC and O_APPEND flag support
OpenFile doesn't handle important file opening flags. O_TRUNC should truncate file if it exists but current implementation doesn't check for this flag. O_APPEND has no tracking in File struct, and Write() always appends. Incorrect file opening semantics cause behavior to differ from standard os.OpenFile.

### Issue #6: MAJOR: Mkdir and Remove have incorrect semantics
Two functions have semantics that differ from standard os package. Mkdir doesn't check if directory exists (os.Mkdir returns os.ErrExist if directory already exists, but this implementation silently overwrites). Remove doesn't error on missing files (os.Remove returns error if file doesn't exist, but this implementation succeeds even if file was never there). Non-standard behavior breaks compatibility with code expecting standard semantics.

### Issue #7: MAJOR: File struct not thread-safe
The File struct has mutable fields (buffer, offset, body) that are accessed without synchronization. Multiple concurrent operations (Read/Write/Seek) on the same File instance will race, causing undefined behavior and potential data corruption. FileSystem is thread-safe (AWS SDK v2 client is thread-safe), but individual File instances are not.

### Issue #15: MAJOR: All writes buffered in memory - OOM risk for large files
The Write implementation buffers the entire file in memory. The buffer is only uploaded on Close(), meaning large files exhaust memory with no streaming upload support. This is unsuitable for large file uploads and can crash with out-of-memory errors.

### Issue #16: MAJOR: Minimal test coverage
The test file has only 4 trivial tests that check struct field assignment and basic SDK helpers. No tests exist for actual S3 operations (read, write, delete, rename), error handling, edge cases, concurrent operations, large files, pagination, or the critical bugs found in code review. No confidence in functionality as critical bugs are not caught by tests.

## Minor Issues

### Issue #8: Context cannot be updated after initialization
The FileSystem.ctx field is created as context.Background() in New() and never updated. All S3 operations use this context, meaning no way to cancel operations, no way to enforce timeouts at the filesystem level, and violation of Go context patterns. Cannot cancel long-running operations or enforce timeouts.

### Issue #9: Missing error context wrapping
AWS SDK errors are returned directly without wrapping or context. Callers don't know which operation failed or what path was involved. This makes debugging difficult, especially in production logs. Poor error context makes debugging harder.

### Issue #10: Missing godoc comments on public API
Many public types and functions lack godoc comments. File struct and most File methods lack documentation. No documentation of thread-safety guarantees or limitations (e.g., Seek with SeekEnd). No package-level examples or documentation of S3-specific limitations. Poor API usability as users can't understand limitations from documentation.

### Issue #17: ReadAt creates new S3 connection per call - performance issue
The ReadAt function creates a new GetObject request for every call, differing from Read() which caches the body. Each ReadAt call creates new HTTP connection to S3, sends new GetObject request, reads data, and closes connection. Performance degradation for repeated ReadAt calls due to network overhead.

### Issue #18: Update absfs dependency from 2020
The absfs dependency in go.mod is from June 2020 (over 4 years old). Potential issues include missing bug fixes, missing security patches, potential incompatibilities with newer Go versions, and missing new features. Dependency maintenance issue requiring update to latest version if available.
