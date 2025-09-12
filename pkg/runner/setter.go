package runner

import (
	"sync"
	"time"
)

var (
	// processed tracks UIDs that have already been processed to prevent duplicate executions.
	// Using a global variable here is safe because the controller ensures only one instance runs,
	// but in a multi-instance setup, this would need to be stored in a distributed cache.
	processed = newUIDSet()
)

// uidSet is a thread-safe set for tracking processed UIDs with expiration.
// This prevents duplicate processing of the same pod in case of re-queues or restarts.
// Entries expire after 30 minutes to prevent unbounded memory growth.
// In a multi-replica controller setup, this would need to be replaced with a distributed cache.
type uidSet struct {
	mu sync.RWMutex
	m  map[string]time.Time
}

func newUIDSet() *uidSet { return &uidSet{m: make(map[string]time.Time)} }

// Has checks if a UID has been processed and performs best-effort cleanup of old entries.
// The 30-minute expiration prevents unbounded memory growth while allowing reasonable
// protection against duplicate processing.
func (s *uidSet) Has(uid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Expire old entries (cleanup during read to spread the cost)
	now := time.Now()
	for k, t := range s.m {
		if now.Sub(t) > 30*time.Minute {
			delete(s.m, k)
		}
	}

	_, exists := s.m[uid]
	return exists
}

// Add marks a UID as processed with the current timestamp.
func (s *uidSet) Add(uid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[uid] = time.Now()
}
