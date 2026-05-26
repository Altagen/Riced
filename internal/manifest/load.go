package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// FileName is the well-known name of the manifest file in a theme directory.
const FileName = "theme.toml"

// Load reads <themeDir>/theme.toml and returns a parsed Manifest. The returned
// Manifest is NOT yet validated -- call Validate separately so callers can
// choose between "fail fast on first error" and "report all errors".
func Load(themeDir string) (*Manifest, error) {
	absDir, err := filepath.Abs(themeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve theme dir: %w", err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("stat theme dir %s: %w", absDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", absDir)
	}

	path := filepath.Join(absDir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var m Manifest
	meta, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		// Unknown keys are a soft error -- surface them as a validation issue
		// rather than a hard failure, so older Riced versions stay forward-
		// compatible with newer manifests that only add fields.
		// (For now we just stash this for Validate to read.)
		m.unknownKeys = make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			m.unknownKeys = append(m.unknownKeys, k.String())
		}
	}

	m.Dir = absDir
	return &m, nil
}
