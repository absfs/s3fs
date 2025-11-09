package s3fs

import (
	"strings"
	"testing"
)

func TestTrimPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/test/path", "test/path"},
		{"test/path", "test/path"},
		{"/", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := trimPrefix(tt.input)
			if got != tt.want {
				t.Errorf("trimPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMkdirAllParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"a/b/c", []string{"a/", "a/b/", "a/b/c/"}},
		{"a/b/c/", []string{"a/", "a/b/", "a/b/c/"}},
		{"/a/b/c", []string{"a/", "a/b/", "a/b/c/"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name := strings.TrimPrefix(tt.input, "/")
			if name == "" || name == "." {
				return
			}

			if !strings.HasSuffix(name, "/") {
				name += "/"
			}

			parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
			var dirs []string
			for i := range parts {
				dir := strings.Join(parts[:i+1], "/") + "/"
				dirs = append(dirs, dir)
			}

			if len(dirs) != len(tt.expected) {
				t.Errorf("got %d dirs, want %d", len(dirs), len(tt.expected))
			}

			for i, dir := range dirs {
				if i >= len(tt.expected) {
					break
				}
				if dir != tt.expected[i] {
					t.Errorf("dir[%d] = %q, want %q", i, dir, tt.expected[i])
				}
			}
		})
	}
}
