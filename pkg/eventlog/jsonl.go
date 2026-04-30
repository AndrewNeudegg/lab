package eventlog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	dir string
	mu  sync.Mutex
}

type MatchFunc func(Event) bool

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Append(ctx context.Context, event Event) error {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.dir, event.Time.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *Store) ReadDay(day time.Time) ([]Event, error) {
	path := filepath.Join(s.dir, day.Format("2006-01-02")+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []Event
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var event Event
			if decodeErr := json.Unmarshal(line, &event); decodeErr != nil {
				return nil, decodeErr
			}
			events = append(events, event)
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return nil, err
	}
	return events, nil
}

func (s *Store) DeleteMatching(ctx context.Context, match MatchFunc) (int, error) {
	if match == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return removed, ctx.Err()
		default:
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		fileRemoved, err := deleteMatchingFromFile(path, match)
		removed += fileRemoved
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func deleteMatchingFromFile(path string, match MatchFunc) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var kept bytes.Buffer
	removed := 0
	for {
		line, err := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var event Event
			if decodeErr := json.Unmarshal(trimmed, &event); decodeErr != nil {
				return removed, decodeErr
			}
			if match(event) {
				removed++
			} else {
				kept.Write(trimmed)
				kept.WriteByte('\n')
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return removed, err
	}
	if removed == 0 {
		return 0, nil
	}
	if kept.Len() == 0 {
		return removed, os.Remove(path)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, kept.Bytes(), 0o644); err != nil {
		return removed, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return removed, err
	}
	return removed, nil
}
