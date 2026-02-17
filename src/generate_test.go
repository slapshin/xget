package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestComputeFileHash(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "simple content",
			content:  "hello world",
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:     "empty file",
			content:  "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "multiline content",
			content:  "line1\nline2\nline3\n",
			expected: "bfe2f1b2c9f8c7e1f5c8a3b0d9e8f7a6c5b4d3e2f1a0b9c8d7e6f5a4b3c2d1e0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.txt")

			err := os.WriteFile(filePath, []byte(tt.content), 0o600)
			if err != nil {
				t.Fatalf("failed to create test file: %v", err)
			}

			h := sha256.New()
			h.Write([]byte(tt.content))
			expectedHash := hex.EncodeToString(h.Sum(nil))

			hash, err := computeFileHash(filePath)
			if err != nil {
				t.Fatalf("computeFileHash() error = %v", err)
			}

			if hash != expectedHash {
				t.Errorf("computeFileHash() = %v, want %v", hash, expectedHash)
			}
		})
	}
}

func TestComputeFileHash_NonExistentFile(t *testing.T) {
	_, err := computeFileHash("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestMakeRelativePath(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		filePath string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple relative path",
			baseDir:  "/tmp/base",
			filePath: "/tmp/base/file.txt",
			expected: "file.txt",
			wantErr:  false,
		},
		{
			name:     "nested relative path",
			baseDir:  "/tmp/base",
			filePath: "/tmp/base/subdir/file.txt",
			expected: "subdir/file.txt",
			wantErr:  false,
		},
		{
			name:     "same directory",
			baseDir:  "/tmp/base",
			filePath: "/tmp/base",
			expected: ".",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relPath, err := makeRelativePath(tt.baseDir, tt.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("makeRelativePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if relPath != tt.expected {
				t.Errorf("makeRelativePath() = %v, want %v", relPath, tt.expected)
			}
		})
	}
}

func TestWalkDirectory_SingleFile(t *testing.T) {
	tmpDir := t.TempDir()

	content := "test content"
	filePath := filepath.Join(tmpDir, "file.txt")

	err := os.WriteFile(filePath, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	entries, err := walkDirectory(tmpDir)
	if err != nil {
		t.Fatalf("walkDirectory() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Dest != "file.txt" {
		t.Errorf("expected dest = 'file.txt', got %v", entries[0].Dest)
	}

	if entries[0].URL != "" {
		t.Errorf("expected empty URL, got %v", entries[0].URL)
	}

	if entries[0].SHA256 == "" {
		t.Error("expected non-empty SHA256")
	}
}

//nolint:cyclop // test function complexity is acceptable
func TestWalkDirectory_NestedStructure(t *testing.T) {
	tmpDir := t.TempDir()

	files := []struct {
		path    string
		content string
	}{
		{"file1.txt", "content1"},
		{"subdir/file2.txt", "content2"},
		{"subdir/nested/file3.txt", "content3"},
	}

	for _, f := range files {
		fullPath := filepath.Join(tmpDir, f.path)

		err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
		if err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}

		err = os.WriteFile(fullPath, []byte(f.content), 0o600)
		if err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	entries, err := walkDirectory(tmpDir)
	if err != nil {
		t.Fatalf("walkDirectory() error = %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	expectedPaths := map[string]bool{
		"file1.txt":               false,
		"subdir/file2.txt":        false,
		"subdir/nested/file3.txt": false,
	}

	for _, entry := range entries {
		normalizedDest := filepath.ToSlash(entry.Dest)

		if _, ok := expectedPaths[normalizedDest]; !ok {
			t.Errorf("unexpected dest path: %v", normalizedDest)
		}

		expectedPaths[normalizedDest] = true

		if entry.SHA256 == "" {
			t.Errorf("empty SHA256 for %v", entry.Dest)
		}
	}

	for path, found := range expectedPaths {
		if !found {
			t.Errorf("expected path not found: %v", path)
		}
	}
}

func TestWalkDirectory_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	entries, err := walkDirectory(tmpDir)
	if err != nil {
		t.Fatalf("walkDirectory() error = %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries in empty directory, got %d", len(entries))
	}
}

//nolint:cyclop // test function complexity is acceptable
func TestGenerateConfig_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	files := map[string]string{
		"file1.txt":               "content1",
		"subdir/file2.txt":        "content2",
		"subdir/nested/file3.bin": "binary\x00content",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)

		err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
		if err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}

		err = os.WriteFile(fullPath, []byte(content), 0o600)
		if err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	data, err := generateConfig(tmpDir)
	if err != nil {
		t.Fatalf("generateConfig() error = %v", err)
	}

	if len(data) == 0 {
		t.Fatal("generateConfig() returned empty data")
	}

	var output GenerateOutput

	err = yaml.Unmarshal(data, &output)
	if err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}

	if len(output.Files) != 3 {
		t.Fatalf("expected 3 files in output, got %d", len(output.Files))
	}

	for _, entry := range output.Files {
		if entry.URL != "" {
			t.Errorf("expected empty URL, got %v", entry.URL)
		}

		if entry.Dest == "" {
			t.Error("expected non-empty Dest")
		}

		if entry.SHA256 == "" {
			t.Error("expected non-empty SHA256")
		}

		if len(entry.SHA256) != 64 {
			t.Errorf("expected SHA256 length 64, got %d", len(entry.SHA256))
		}
	}

	yamlStr := string(data)
	if !strings.Contains(yamlStr, "files:") {
		t.Error("YAML output should contain 'files:' key")
	}
}

func TestGenerateConfig_NonExistentDirectory(t *testing.T) {
	_, err := generateConfig("/nonexistent/directory")
	if err == nil {
		t.Error("expected error for non-existent directory, got nil")
	}
}

func TestGenerateConfig_FileInsteadOfDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file.txt")

	err := os.WriteFile(filePath, []byte("content"), 0o600)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err = generateConfig(filePath)
	if err == nil {
		t.Error("expected error when path is a file, got nil")
	}

	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' error, got: %v", err)
	}
}

func TestGenerateConfig_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := generateConfig(tmpDir)
	if err == nil {
		t.Error("expected error for empty directory, got nil")
	}

	if !strings.Contains(err.Error(), "no files found") {
		t.Errorf("expected 'no files found' error, got: %v", err)
	}
}
