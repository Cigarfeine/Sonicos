package idempotency

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type memoryStore struct {
	mu   sync.RWMutex
	keys map[string]memoryEntry
}

type memoryEntry struct {
	token  string
	expiry time.Time
}

func NewMemory() *memoryStore {
	return &memoryStore{keys: make(map[string]memoryEntry)}
}

func (s *memoryStore) Begin(ctx context.Context, messageID string) (string, bool, error) {
	var rawToken [16]byte
	if _, err := rand.Read(rawToken[:]); err != nil {
		return "", false, fmt.Errorf("generate idempotency token: %w", err)
	}
	token := hex.EncodeToString(rawToken[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, e := range s.keys {
		if now.After(e.expiry) {
			delete(s.keys, k)
		}
	}
	if _, exists := s.keys[messageID]; exists {
		return "", false, nil
	}
	s.keys[messageID] = memoryEntry{token: token, expiry: now.Add(2 * time.Minute)}
	return token, true, nil
}

func (s *memoryStore) Complete(ctx context.Context, messageID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.keys[messageID]
	if !ok || e.token != token {
		return nil
	}
	e.expiry = time.Now().Add(24 * time.Hour)
	s.keys[messageID] = e
	return nil
}

func (s *memoryStore) Release(ctx context.Context, messageID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, messageID)
	return nil
}

func (s *memoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = make(map[string]memoryEntry)
	return nil
}
