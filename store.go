package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const maxHistoryEntries = 100

type AppState struct {
	History   []HistoryEntry `json:"history"`
	Bookmarks []HistoryEntry `json:"bookmarks"`
}

type Store struct {
	mu     sync.Mutex
	path   string
	state  AppState
	loaded bool
}

func NewStore() *Store {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}

	return &Store{
		path: filepath.Join(configDir, "FolderSearchLite", "state.json"),
	}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

func (s *Store) AddHistory(entry HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return err
	}

	if s.bookmarkIndexLocked(entry.ID) >= 0 {
		entry.Bookmarked = true
	}

	s.state.History = append([]HistoryEntry{entry}, s.state.History...)
	if len(s.state.History) > maxHistoryEntries {
		s.state.History = s.state.History[:maxHistoryEntries]
	}
	return s.saveLocked()
}

func (s *Store) GetHistory() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return copyHistory(s.state.History), nil
}

func (s *Store) ClearHistory() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	s.state.History = nil
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return []HistoryEntry{}, nil
}

func (s *Store) BookmarkHistory(id string) ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	index := s.historyIndexLocked(id)
	if index < 0 {
		return nil, errors.New("history entry was not found")
	}

	entry := s.state.History[index]
	entry.Bookmarked = true
	s.state.History[index].Bookmarked = true

	bookmarkIndex := s.bookmarkIndexLocked(id)
	if bookmarkIndex >= 0 {
		s.state.Bookmarks[bookmarkIndex] = entry
	} else {
		s.state.Bookmarks = append([]HistoryEntry{entry}, s.state.Bookmarks...)
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return copyHistory(s.state.Bookmarks), nil
}

func (s *Store) GetBookmarks() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return copyHistory(s.state.Bookmarks), nil
}

func (s *Store) RemoveBookmark(id string) ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	index := s.bookmarkIndexLocked(id)
	if index >= 0 {
		s.state.Bookmarks = append(s.state.Bookmarks[:index], s.state.Bookmarks[index+1:]...)
	}

	if historyIndex := s.historyIndexLocked(id); historyIndex >= 0 {
		s.state.History[historyIndex].Bookmarked = false
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return copyHistory(s.state.Bookmarks), nil
}

func (s *Store) loadLocked() error {
	if s.loaded {
		return nil
	}

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		s.loaded = true
		return nil
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return err
	}

	s.loaded = true
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}

func (s *Store) historyIndexLocked(id string) int {
	for index, entry := range s.state.History {
		if entry.ID == id {
			return index
		}
	}
	return -1
}

func (s *Store) bookmarkIndexLocked(id string) int {
	for index, entry := range s.state.Bookmarks {
		if entry.ID == id {
			return index
		}
	}
	return -1
}

func copyHistory(entries []HistoryEntry) []HistoryEntry {
	copied := make([]HistoryEntry, len(entries))
	copy(copied, entries)
	return copied
}
