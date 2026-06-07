package runner

import (
	"sync"
	"time"
)

var (
	// processed tracks UIDs that have already been processed to prevent duplicate executions.
	// The exporters are idempotent, but this prevents unnecessary load and duplicate exports.
	// For multi-replica controller setups migrate to a distributed cache like Redis or Memcached.
	processed = newUIDSet()

	// cacheTTL is the duration for how long UIDs in the set will be kept before expired.
	cacheTTL = 30 * time.Minute

	// cacheSweepInterval determines how often the cleaner goroutine evicts expired UIDs.
	cacheSweepInterval = 5 * time.Minute
)

// uidSet is a thread-safe set for tracking processed UIDs with expiration.
// Entries expire after cacheTTL via a background sweeper so read paths don't
// have to scan the entire map under a write lock.
type uidSet struct {
	mu sync.RWMutex
	m  map[string]time.Time
}

func newUIDSet() *uidSet {
	s := &uidSet{m: make(map[string]time.Time)}
	go s.sweepLoop()
	return s
}

// Has returns whether a UID has been recorded. Stale entries are treated as absent
// so an expired entry behaves like a fresh pod even before the sweeper removes it.
func (s *uidSet) Has(uid string) bool {
	s.mu.RLock()
	t, exists := s.m[uid]
	s.mu.RUnlock()
	if !exists {
		return false
	}
	return time.Since(t) <= cacheTTL
}

// Add marks a UID as processed with the current timestamp.
func (s *uidSet) Add(uid string) {
	s.mu.Lock()
	s.m[uid] = time.Now()
	s.mu.Unlock()
}

// sweep removes entries older than cacheTTL.
func (s *uidSet) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, t := range s.m {
		if now.Sub(t) > cacheTTL {
			delete(s.m, k)
		}
	}
}

// sweepLoop runs sweep at cacheSweepInterval cadence. Lives for the process
// lifetime — this is a singleton.
func (s *uidSet) sweepLoop() {
	t := time.NewTicker(cacheSweepInterval)
	defer t.Stop()
	for range t.C {
		s.sweep()
	}
}
