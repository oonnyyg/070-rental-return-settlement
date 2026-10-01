package rental

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const stateFileName = "state.json"

// LoadStore reads the store from dir, creating an empty one if the
// state file does not exist yet.
func LoadStore(dir string) (*Store, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create state dir: %w", err)
	}
	path := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return NewStore(), path, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read state: %w", err)
	}
	s := NewStore()
	if err := json.Unmarshal(data, s); err != nil {
		return nil, "", fmt.Errorf("parse state: %w", err)
	}
	if s.Rentals == nil {
		s.Rentals = map[string]*Rental{}
	}
	return s, path, nil
}

// SaveTo atomically persists the store to path.
func (s *Store) SaveTo(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}