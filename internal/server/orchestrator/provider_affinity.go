package orchestrator

import (
	"container/list"
	"context"
	"sync"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

const defaultProviderAffinityMaxEntries = 1024

type providerAffinityEntry struct {
	key         string
	providerKey string
}

type ProviderAffinityStore struct {
	mu         sync.RWMutex
	maxEntries int
	entries    map[string]*list.Element
	order      *list.List
}

func NewProviderAffinityStore(maxEntries int) *ProviderAffinityStore {
	if maxEntries <= 0 {
		maxEntries = defaultProviderAffinityMaxEntries
	}

	return &ProviderAffinityStore{
		maxEntries: maxEntries,
		entries:    make(map[string]*list.Element),
		order:      list.New(),
	}
}

func ProviderKey(c *ChannelModelsCandidate) string {
	return string(c.Channel.Type)
}

func (s *ProviderAffinityStore) Get(scope, sessionID string) (string, bool) {
	key := s.affinityKey(scope, sessionID)
	if key == "" {
		return "", false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	element, ok := s.entries[key]
	if !ok {
		return "", false
	}

	entry := element.Value.(providerAffinityEntry)
	return entry.providerKey, true
}

func (s *ProviderAffinityStore) Set(scope, sessionID, providerKey string) {
	key := s.affinityKey(scope, sessionID)
	if key == "" || providerKey == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if element, ok := s.entries[key]; ok {
		element.Value = providerAffinityEntry{key: key, providerKey: providerKey}
		return
	}

	element := s.order.PushBack(providerAffinityEntry{key: key, providerKey: providerKey})
	s.entries[key] = element

	if len(s.entries) > s.maxEntries {
		s.evictOldest()
	}
}

func (s *ProviderAffinityStore) Delete(scope, sessionID string) {
	key := s.affinityKey(scope, sessionID)
	if key == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	element, ok := s.entries[key]
	if !ok {
		return
	}

	s.order.Remove(element)
	delete(s.entries, key)
}

func (s *ProviderAffinityStore) WithScopedSessionID(ctx context.Context) (string, string, bool) {
	scope, scopeOK := shared.GetSessionScope(ctx)
	sessionID, sessionOK := shared.GetSessionID(ctx)
	if !scopeOK || !sessionOK || scope == "" || sessionID == "" {
		return "", "", false
	}

	return scope, sessionID, true
}

func (s *ProviderAffinityStore) affinityKey(scope, sessionID string) string {
	if scope == "" || sessionID == "" {
		return ""
	}

	return scope + "\x00" + sessionID
}

func (s *ProviderAffinityStore) evictOldest() {
	oldest := s.order.Front()
	if oldest == nil {
		return
	}

	entry := oldest.Value.(providerAffinityEntry)
	s.order.Remove(oldest)
	delete(s.entries, entry.key)
}
