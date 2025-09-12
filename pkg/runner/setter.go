package runner

import (
	"sync"
	"time"
)

var (
	// processed tracks UIDs that have already been processed to prevent duplicate executions.
	// The exporter are idempotent, still, this prevents unnecessary load and duplicate exports.
	// To enable multi-replica controller setups migrated to distributed caches like Redis or Memcached.
	processed = newUIDSet()

	// cacheTTL is the duration for how long UIDs in the set will be kept before expired.
	cacheTTL = 30 * time.Minute
)

// uidSet is a thread-safe set for tracking processed UIDs with expiration.
// This prevents duplicate processing of the same pod in case of re-queues or restarts.
// Entries expire after cacheTTL to prevent unbounded memory growth.
type uidSet struct {
	mu sync.RWMutex
	m  map[string]time.Time
}

func newUIDSet() *uidSet { return &uidSet{m: make(map[string]time.Time)} }

// Has checks if a UID has been processed and performs best-effort cleanup of old entries.
// The cacheTTL expiration prevents unbounded memory growth while allowing reasonable
// protection against duplicate processing.
func (s *uidSet) Has(uid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Expire old entries (cleanup during read to spread the cost)
	now := time.Now()
	for k, t := range s.m {
		if now.Sub(t) > cacheTTL {
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
