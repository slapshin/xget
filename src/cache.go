package main

import (
	"context"
	"fmt"
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

// NewSource creates a download source for the cached object stored under the
// given SHA256 hash.
func (cache *Cache) NewSource(ctx context.Context, sha256Hash string) (storage.Source, error) {
	source, err := storage.NewS3SourceFromAlias(ctx, cache.alias, sha256Hash)
	if err != nil {
		return nil, fmt.Errorf("creating S3 source: %w", err)
	}

	return source, nil
}

// Has reports whether an object with the given SHA256 hash is present in cache.
func (cache *Cache) Has(ctx context.Context, sha256Hash string) (bool, error) {
	source, err := storage.NewS3SourceFromAlias(ctx, cache.alias, sha256Hash)
	if err != nil {
		return false, fmt.Errorf("creating S3 source: %w", err)
	}

	exists, err := source.Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("checking cache: %w", err)
	}

	return exists, nil
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
