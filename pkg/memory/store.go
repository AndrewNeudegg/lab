package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Read(name string) (string, error) {
	path, err := s.safePath(name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func (s *Store) ProposeWrite(name, content string) (string, error) {
	path, err := s.safePath(name + ".proposal")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(content), 0o644)
}

func (s *Store) CommitWrite(name, content string) error {
	path, err := s.safePath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func (s *Store) safePath(name string) (string, error) {
	if name == "" || strings.Contains(name, "..") || filepath.IsAbs(name) {
		return "", errors.New("unsafe memory path")
	}
	path := filepath.Clean(filepath.Join(s.dir, name))
	root := filepath.Clean(s.dir)
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("memory path escapes root")
	}
	return path, nil
}
