package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
)

// VerifyFileSHA256 checks if a file matches the expected SHA256 hash.
func VerifyFileSHA256(path, expectedHash string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("opening file: %w", err)
	}

	defer file.Close()

	h := sha256.New()

	_, err = io.Copy(h, file)
	if err != nil {
		return false, fmt.Errorf("reading file: %w", err)
	}

	actualHash := hex.EncodeToString(h.Sum(nil))

	return actualHash == expectedHash, nil
}

// SHA256Writer wraps a writer and computes SHA256 hash of written data.
type SHA256Writer struct {
	writer io.Writer
	hash   hash.Hash
}

// NewSHA256Writer creates a new SHA256Writer.
func NewSHA256Writer(w io.Writer) *SHA256Writer {
	h := sha256.New()

	return &SHA256Writer{
		writer: io.MultiWriter(w, h),
		hash:   h,
	}
}

// Write implements io.Writer.
func (sha256Writer *SHA256Writer) Write(p []byte) (int, error) {
	return sha256Writer.writer.Write(p)
}

// Sum returns the hex-encoded SHA256 hash of all written data.
func (sha256Writer *SHA256Writer) Sum() string {
	return hex.EncodeToString(sha256Writer.hash.Sum(nil))
}
