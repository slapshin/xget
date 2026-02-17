package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"xget/src/config"
)

// GenerateOutput represents the output structure for generated config.
type GenerateOutput struct {
	Files []config.FileEntry `yaml:"files"`
}

// generateConfig generates a config file by scanning a directory.
func generateConfig(dirPath string) ([]byte, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, fmt.Errorf("accessing directory: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", dirPath)
	}

	entries, err := walkDirectory(dirPath)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no files found in directory: %s", dirPath)
	}

	output := GenerateOutput{Files: entries}

	data, err := yaml.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("marshaling to yaml: %w", err)
	}

	return data, nil
}

// walkDirectory walks a directory tree and returns file entries.
func walkDirectory(baseDir string) ([]config.FileEntry, error) {
	var entries []config.FileEntry

	var warnings []string

	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			warning := fmt.Sprintf("warning: cannot access %s: %v", path, err)
			warnings = append(warnings, warning)

			return nil
		}

		if d.IsDir() {
			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		relPath, err := makeRelativePath(baseDir, path)
		if err != nil {
			warning := fmt.Sprintf("warning: cannot compute relative path for %s: %v", path, err)
			warnings = append(warnings, warning)

			return nil
		}

		hash, err := computeFileHash(path)
		if err != nil {
			warning := fmt.Sprintf("warning: cannot compute hash for %s: %v", path, err)
			warnings = append(warnings, warning)

			return nil
		}

		entry := config.FileEntry{
			URL:    "",
			Dest:   relPath,
			SHA256: hash,
		}
		entries = append(entries, entry)

		return nil
	})

	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, warning)
	}

	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	return entries, nil
}

// computeFileHash computes the SHA256 hash of a file.
func computeFileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}

	defer file.Close()

	h := sha256.New()

	_, err = io.Copy(h, file)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	hash := hex.EncodeToString(h.Sum(nil))

	return hash, nil
}

// makeRelativePath computes a relative path from base directory to file path.
func makeRelativePath(baseDir, filePath string) (string, error) {
	relPath, err := filepath.Rel(baseDir, filePath)
	if err != nil {
		return "", fmt.Errorf("computing relative path: %w", err)
	}

	return relPath, nil
}
