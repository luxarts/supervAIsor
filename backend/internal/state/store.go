package state

import (
	"fmt"
	"sync"

	"github.com/luxarts/supervaisor/internal/agent"
)

// Store is a thread-safe in-memory registry of agents.
type Store struct {
	mu     sync.RWMutex
	agents map[string]*agent.Agent
}

func NewStore() *Store {
	return &Store{agents: make(map[string]*agent.Agent)}
}

func (s *Store) Set(a *agent.Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[a.ID] = a
}

func (s *Store) Get(id string) (*agent.Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", id)
	}
	return a, nil
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, id)
}

func (s *Store) List() []*agent.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*agent.Agent, 0, len(s.agents))
	for _, a := range s.agents {
		list = append(list, a)
	}
	return list
}
