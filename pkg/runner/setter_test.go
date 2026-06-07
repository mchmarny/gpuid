package runner

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestUIDSet_ConcurrentAccess tests that uidSet handles concurrent access safely.
func TestUIDSet_ConcurrentAccess(t *testing.T) {
	set := newUIDSet()
	const numGoroutines = 100
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			for j := range numOperations {
				uid := fmt.Sprintf("uid-%d-%d", id, j)
				set.Add(uid)

				if !set.Has(uid) {
					t.Errorf("UID %s should exist after adding", uid)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestUIDSet_ExpirationCleanup tests that old entries are treated as absent and
// removed by the sweeper.
func TestUIDSet_ExpirationCleanup(t *testing.T) {
	set := newUIDSet()

	recentUID := "recent-uid"
	oldUID := "old-uid"

	set.Add(recentUID)
	// Inject an old UID directly so it qualifies for expiry.
	set.mu.Lock()
	set.m[oldUID] = time.Now().Add(-(cacheTTL + time.Minute))
	set.mu.Unlock()

	if !set.Has(recentUID) {
		t.Error("Recent UID should still exist")
	}

	if set.Has(oldUID) {
		t.Error("Expired UID should report as absent")
	}

	// Force a sweep to verify the entry is also removed from the map.
	set.sweep()

	set.mu.RLock()
	_, exists := set.m[oldUID]
	set.mu.RUnlock()

	if exists {
		t.Error("Old UID should have been removed by sweep")
	}
}

// TestUIDSet_BasicOperations tests basic add and check operations.
func TestUIDSet_BasicOperations(t *testing.T) {
	set := newUIDSet()

	uid := "test-uid"

	if set.Has(uid) {
		t.Error("UID should not exist initially")
	}

	set.Add(uid)

	if !set.Has(uid) {
		t.Error("UID should exist after adding")
	}

	if !set.Has(uid) {
		t.Error("UID should still exist on second check")
	}
}
