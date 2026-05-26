package manifest_test

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a tiny helper for fixture tests that need to compose a fake
// theme tree in t.TempDir(). It creates intermediate directories so callers
// can hand it a deep path without ceremony.
func writeFile(t *testing.T, path, body string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}
