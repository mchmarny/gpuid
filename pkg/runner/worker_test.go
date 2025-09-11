package runner

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestUIDSet_ConcurrentAccess tests that uidSet handles concurrent access safely
func TestUIDSet_ConcurrentAccess(t *testing.T) {
	set := newUIDSet()
	const numGoroutines = 100
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple goroutines that concurrently add and check UIDs
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				uid := fmt.Sprintf("uid-%d-%d", id, j)

				// Add the UID
				set.Add(uid)

				// Check if it exists
				if !set.Has(uid) {
					t.Errorf("UID %s should exist after adding", uid)
				}

				// Add some old UIDs to trigger cleanup during Has() calls
				if j%10 == 0 {
					oldUID := fmt.Sprintf("old-uid-%d-%d", id, j)
					// Directly access internal map for testing (normally wouldn't do this)
					set.mu.Lock()
					set.m[oldUID] = time.Now().Add(-35 * time.Minute) // Older than 30 minutes
					set.mu.Unlock()
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
}

// TestUIDSet_ExpirationCleanup tests that old entries are properly cleaned up
func TestUIDSet_ExpirationCleanup(t *testing.T) {
	set := newUIDSet()

	// Add some UIDs with different timestamps
	currentTime := time.Now()
	recentUID := "recent-uid"
	oldUID := "old-uid"

	set.Add(recentUID)
	// Add old UID directly for testing
	set.mu.Lock()
	set.m[oldUID] = currentTime.Add(-35 * time.Minute) // Older than 30 minutes
	set.mu.Unlock()

	// Check that recent UID exists and old UID gets cleaned up
	if !set.Has(recentUID) {
		t.Error("Recent UID should still exist")
	}

	// The Has() call should have cleaned up the old UID
	set.mu.Lock()
	_, exists := set.m[oldUID]
	set.mu.Unlock()

	if exists {
		t.Error("Old UID should have been cleaned up")
	}
}

// TestUIDSet_BasicOperations tests basic add and check operations
func TestUIDSet_BasicOperations(t *testing.T) {
	set := newUIDSet()

	uid := "test-uid"

	// Initially should not exist
	if set.Has(uid) {
		t.Error("UID should not exist initially")
	}

	// Add the UID
	set.Add(uid)

	// Should now exist
	if !set.Has(uid) {
		t.Error("UID should exist after adding")
	}

	// Should still exist on subsequent checks
	if !set.Has(uid) {
		t.Error("UID should still exist on second check")
	}
}
