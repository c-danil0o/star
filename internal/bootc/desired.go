package bootc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type DesiredImage struct {
	ImageRef     string
	Digest       string
	RebootPolicy string
}

type DesiredStore struct {
	mu      sync.Mutex
	desired *DesiredImage
}

func NewDesiredStore() *DesiredStore {
	return &DesiredStore{}
}

func (s *DesiredStore) Set(desired DesiredImage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.desired = &desired
}

func (s *DesiredStore) Get() *DesiredImage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.desired
}

type Attempt struct {
	Image     string `json:"image"`
	Digest    string `json:"digest"`
	Result    string `json:"result"`
	Timestamp int64  `json:"timestamp"`
}

type AttemptStore struct {
	path string
	mu   sync.Mutex
}

func NewAttemptStore(stateDir string) *AttemptStore {
	return &AttemptStore{
		path: filepath.Join(stateDir, "os_attempt.json"),
	}
}

func (s *AttemptStore) Record(attempt Attempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func (s *AttemptStore) Last() *Attempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	attempt := &Attempt{}
	if err := json.Unmarshal(data, attempt); err != nil {
		return nil
	}
	return attempt
}

func (s *AttemptStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
