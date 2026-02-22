package state

import (
	"encoding/json"
	"os"
	"sync"
)

// Subscriptions maps "channel:chatID" → tmux session name.
type Subscriptions struct {
	mu   sync.RWMutex
	data map[string]string
	path string
}

func NewSubscriptions(path string) (*Subscriptions, error) {
	s := &Subscriptions{
		data: make(map[string]string),
		path: path,
	}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *Subscriptions) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

func (s *Subscriptions) Set(key, session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = session
	s.save()
}

func (s *Subscriptions) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	s.save()
}

func (s *Subscriptions) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

func (s *Subscriptions) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &s.data)
}

func (s *Subscriptions) save() {
	data, _ := json.MarshalIndent(s.data, "", "  ")
	os.WriteFile(s.path, data, 0600)
}
