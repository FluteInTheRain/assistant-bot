package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LoadJSONState reads the persisted state file into v. It returns
// (false, nil) when the file does not exist — the caller's "first run,
// take a silent baseline" case (design 5.3).
func LoadJSONState(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, err
	}
	return true, nil
}

// SaveJSONState writes v atomically (temp file + rename) so a crash never
// leaves a half-written state behind, and never fails on a missing directory.
func SaveJSONState(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
