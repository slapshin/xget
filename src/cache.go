package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"xget/src/config"
	"xget/src/storage"
)

// Cache provides caching functionality using S3 storage.
type Cache struct {
	alias config.Alias
}

// NewCache creates a new Cache from config.
// Returns nil if cache is not enabled.
func NewCache(cfg *config.Config) *Cache {
	alias, ok := cfg.GetCacheAlias()
	if !ok {
		return nil
	}

	return &Cache{alias: alias}
}

// Get retrieves a file from cache by its SHA256 hash.
// Returns true if file was found in cache and downloaded successfully.
func (cache *Cache) Get(ctx context.Context, sha256Hash, destPath string) (bool, error) {
	source, err := storage.NewS3SourceFromAlias(ctx, cache.alias, sha256Hash)
	if err != nil {
		return false, fmt.Errorf("creating S3 source: %w", err)
	}

	// Check if file exists in cache.
	exists, err := source.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("checking cache: %w", err)
	}

	if !exists {
		return false, nil
	}

	// Download from cache.
	reader, _, err := source.Download(ctx, 0)
	if err != nil {
		return false, fmt.Errorf("downloading from cache: %w", err)
	}

	defer reader.Close()

	// Create destination file.
	file, err := os.Create(destPath)
	if err != nil {
		return false, fmt.Errorf("creating destination file: %w", err)
	}

	defer file.Close()

	// Copy content.
	_, err = io.Copy(file, reader)
	if err != nil {
		os.Remove(destPath)

		return false, fmt.Errorf("writing file: %w", err)
	}

	// Verify checksum.
	valid, err := VerifyFileSHA256(destPath, sha256Hash)
	if err != nil {
		os.Remove(destPath)

		return false, fmt.Errorf("verifying checksum: %w", err)
	}

	if !valid {
		os.Remove(destPath)

		return false, fmt.Errorf("checksum mismatch from cache")
	}

	return true, nil
}

// Put uploads a file to cache with its SHA256 hash as the key.
func (cache *Cache) Put(ctx context.Context, sha256Hash, sourcePath string) error {
	source, err := storage.NewS3SourceFromAlias(ctx, cache.alias, sha256Hash)
	if err != nil {
		return fmt.Errorf("creating S3 source: %w", err)
	}

	// Check if already in cache.
	exists, err := source.Exists(ctx)
	if err != nil {
		return fmt.Errorf("checking cache: %w", err)
	}

	if exists {
		return nil
	}

	// Open source file.
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("opening source file: %w", err)
	}

	defer file.Close()

	// Upload to cache.
	err = source.Upload(ctx, file)
	if err != nil {
		return fmt.Errorf("uploading to cache: %w", err)
	}

	return nil
}
