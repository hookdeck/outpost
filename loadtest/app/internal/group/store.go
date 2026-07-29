package group

import (
	"fmt"
	"sync"
)

type Store struct {
	mu     sync.RWMutex
	groups map[string]*Group
}

func NewStore() *Store {
	return &Store{groups: make(map[string]*Group)}
}

func (s *Store) Create(cfg Config) (*Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.groups[cfg.Name]; exists {
		return nil, fmt.Errorf("group %q already exists", cfg.Name)
	}

	g := New(cfg)
	s.groups[cfg.Name] = g
	return g, nil
}

func (s *Store) Get(name string) (*Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g, ok := s.groups[name]
	if !ok {
		return nil, fmt.Errorf("group %q not found", name)
	}
	return g, nil
}

func (s *Store) List() []*Group {
	s.mu.RLock()
	defer s.mu.RUnlock()

	groups := make([]*Group, 0, len(s.groups))
	for _, g := range s.groups {
		groups = append(groups, g)
	}
	return groups
}

func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.groups[name]; !ok {
		return fmt.Errorf("group %q not found", name)
	}
	delete(s.groups, name)
	return nil
}
