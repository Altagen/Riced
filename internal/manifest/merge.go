package manifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// pathFields lists the manifest fields that hold filesystem paths
// relative to the theme directory. The merge layer must rewrite these
// to absolute paths before handing values from a parent to a child --
// otherwise the child's Dir would be used to resolve a path that was
// authored relative to the parent's Dir.
//
// Listed explicitly (rather than discovered via reflection) so adding a
// new path field stays a deliberate two-touch change.
//
// Implemented as a function returning a fresh map (rather than a
// package-level var) so callers can't mutate the source of truth. The
// allocation cost is irrelevant -- we call this once per merge.
func pathFields() map[string][]string {
	return map[string][]string{
		"wallpapers": {"paths"},         // array of strings
		"panel":      {"launcher_icon"}, // single string
	}
}

// loadRawTOML reads a TOML file into an untyped map. We keep parsing untyped
// during the merge so that absent vs zero-value can be distinguished and so
// nested sections can be deep-merged section by section.
func loadRawTOML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	raw := map[string]any{}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return raw, nil
}

// absolutizePaths rewrites every relative path in pathFields to its absolute
// equivalent using baseDir. Mutates raw in place.
func absolutizePaths(raw map[string]any, baseDir string) {
	for section, keys := range pathFields() {
		sec, ok := raw[section].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range keys {
			val, present := sec[key]
			if !present {
				continue
			}
			switch v := val.(type) {
			case string:
				if v != "" && !filepath.IsAbs(v) {
					sec[key] = filepath.Join(baseDir, v)
				}
			case []any:
				for i, item := range v {
					s, ok := item.(string)
					if !ok || s == "" || filepath.IsAbs(s) {
						continue
					}
					v[i] = filepath.Join(baseDir, s)
				}
				sec[key] = v
			}
		}
	}
}

// deepMerge returns the union of parent and child, recursing into nested
// map[string]any values. Child wins on every scalar; arrays from the child
// replace arrays from the parent (no append -- too ambiguous to be safe).
//
// The function does not mutate its inputs.
func deepMerge(parent, child map[string]any) map[string]any {
	out := make(map[string]any, len(parent)+len(child))
	for k, v := range parent {
		out[k] = v
	}
	for k, v := range child {
		if childMap, ok := v.(map[string]any); ok {
			if parentVal, ok := out[k]; ok {
				if parentMap, ok := parentVal.(map[string]any); ok {
					out[k] = deepMerge(parentMap, childMap)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// decodeMerged round-trips a merged map back through TOML to take advantage
// of BurntSushi/toml's struct tag handling. The encode/decode pair is the
// simplest way to populate a Manifest from a map without writing reflection
// glue by hand.
func decodeMerged(raw map[string]any) (*Manifest, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(raw); err != nil {
		return nil, fmt.Errorf("re-encode merged manifest: %w", err)
	}
	var m Manifest
	if _, err := toml.Decode(buf.String(), &m); err != nil {
		return nil, fmt.Errorf("decode merged manifest: %w", err)
	}
	return &m, nil
}
