package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSidecarMatches(t *testing.T) {
	content := []byte("verified content")

	tests := []struct {
		name string
		// prepare runs after the sidecar was written and may change the file,
		// the sidecar or the hash the check is made against.
		prepare func(t *testing.T, dest string) string
		want    bool
	}{
		{
			name:    "unchanged file",
			prepare: func(_ *testing.T, _ string) string { return hashOf(content) },
			want:    true,
		},
		{
			name: "missing sidecar",
			prepare: func(_ *testing.T, dest string) string {
				removeSidecar(dest)

				return hashOf(content)
			},
		},
		{
			name: "corrupt sidecar",
			prepare: func(t *testing.T, dest string) string {
				t.Helper()
				writeTestFile(t, sidecarPath(dest), []byte("{not json"))

				return hashOf(content)
			},
		},
		{
			name:    "different expected hash",
			prepare: func(_ *testing.T, _ string) string { return hashOf([]byte("other")) },
		},
		{
			name: "size changed",
			prepare: func(t *testing.T, dest string) string {
				t.Helper()

				info := statTestFile(t, dest)
				writeTestFile(t, dest, append(content, '!'))
				setTestModTime(t, dest, info.ModTime())

				return hashOf(content)
			},
		},
		{
			name: "mtime changed",
			prepare: func(t *testing.T, dest string) string {
				t.Helper()
				setTestModTime(t, dest, statTestFile(t, dest).ModTime().Add(time.Second))

				return hashOf(content)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "file.bin")
			writeTestFile(t, dest, content)

			err := writeSidecar(dest, hashOf(content), statTestFile(t, dest))
			if err != nil {
				t.Fatalf("writeSidecar: %v", err)
			}

			expectedHash := test.prepare(t, dest)

			got := sidecarMatches(dest, expectedHash, statTestFile(t, dest))
			if got != test.want {
				t.Errorf("sidecarMatches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWriteSidecarReplacesSymlink(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "file.bin")
	victim := filepath.Join(dir, "victim.txt")
	content := []byte("verified content")

	writeTestFile(t, dest, content)
	writeTestFile(t, victim, []byte("keep me"))

	err := os.Symlink(victim, sidecarPath(dest))
	if err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	err = writeSidecar(dest, hashOf(content), statTestFile(t, dest))
	if err != nil {
		t.Fatalf("writeSidecar: %v", err)
	}

	assertFileContent(t, victim, []byte("keep me"))

	info, err := os.Lstat(sidecarPath(dest))
	if err != nil {
		t.Fatalf("stating sidecar: %v", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("sidecar is still a symlink")
	}

	if !sidecarMatches(dest, hashOf(content), statTestFile(t, dest)) {
		t.Error("sidecar does not match after replacing the symlink")
	}
}
