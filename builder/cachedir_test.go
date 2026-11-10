package builder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCacheDir(t *testing.T) {
	t.Setenv("PACKER_CACHE_DIR", t.TempDir())
	if err := checkCacheDir(); err != nil {
		t.Fatalf("usable cache dir rejected: %v", err)
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACKER_CACHE_DIR", filepath.Join(file, "cache")) // cannot be created under a regular file
	if err := checkCacheDir(); err == nil {
		t.Fatal("unusable cache dir accepted")
	}
}
