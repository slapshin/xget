package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"xget/src/config"
)

// verification is the sidecar content: the hash a file was verified against
// together with the size and modification time it had at that moment. While
// size and mtime are unchanged the file is trusted without re-hashing it.
type verification struct {
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime_ns"`
}

// sidecarPath returns the path of the verification sidecar for dest.
func sidecarPath(dest string) string {
	return dest + config.SidecarSuffix
}

// writeSidecar records that dest, as described by info, was verified against
// sha256Hash. info must be taken before hashing and after dest reached its
// final path: a change made while hashing then fails the size/mtime check on
// later runs instead of being trusted.
func writeSidecar(dest, sha256Hash string, info os.FileInfo) error {
	data, err := json.Marshal(verification{
		SHA256:  sha256Hash,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
	})
	if err != nil {
		return fmt.Errorf("encoding sidecar: %w", err)
	}

	return replaceFile(sidecarPath(dest), data)
}

// sidecarMatches reports whether the sidecar of dest records a verification
// against sha256Hash that still matches the current size and mtime in info.
// A missing or unreadable sidecar is not an error: it just does not match.
func sidecarMatches(dest, sha256Hash string, info os.FileInfo) bool {
	data, err := os.ReadFile(sidecarPath(dest))
	if err != nil {
		return false
	}

	var recorded verification

	err = json.Unmarshal(data, &recorded)
	if err != nil {
		return false
	}

	if recorded.SHA256 != sha256Hash {
		return false
	}

	if recorded.Size != info.Size() {
		return false
	}

	return recorded.ModTime == info.ModTime().UnixNano()
}

// replaceFile writes data to a temp file next to path and renames it over
// path. Unlike writing path directly, this never follows a symlink planted at
// path, and readers never see a half-written file.
func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("creating temp sidecar: %w", err)
	}

	defer os.Remove(tmp.Name())

	_, err = tmp.Write(data)
	if err != nil {
		tmp.Close()

		return fmt.Errorf("writing temp sidecar: %w", err)
	}

	err = tmp.Close()
	if err != nil {
		return fmt.Errorf("closing temp sidecar: %w", err)
	}

	err = os.Rename(tmp.Name(), path)
	if err != nil {
		return fmt.Errorf("replacing sidecar: %w", err)
	}

	return nil
}

// removeSidecar deletes the sidecar of dest, ignoring a missing one.
func removeSidecar(dest string) {
	os.Remove(sidecarPath(dest))
}
