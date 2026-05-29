package sync

import (
	"strings"
	"testing"
)

func TestAcquireSourceLock(t *testing.T) {
	setupTestStateDir(t)

	unlock, err := AcquireSourceLock("test-source")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer unlock()

	// Second acquire on same source should fail
	_, err = AcquireSourceLock("test-source")
	if err == nil {
		t.Fatal("expected error on second acquire, got nil")
	}
	if !strings.Contains(err.Error(), "locked by another process") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAcquireSourceLockDifferentSources(t *testing.T) {
	setupTestStateDir(t)

	unlock1, err := AcquireSourceLock("source-a")
	if err != nil {
		t.Fatalf("acquire source-a: %v", err)
	}
	defer unlock1()

	// Different source should succeed
	unlock2, err := AcquireSourceLock("source-b")
	if err != nil {
		t.Fatalf("acquire source-b: %v", err)
	}
	unlock2()
}

func TestAcquireSourceLockRelease(t *testing.T) {
	setupTestStateDir(t)

	unlock, err := AcquireSourceLock("reusable")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// Release
	unlock()

	// Should be able to re-acquire
	unlock2, err := AcquireSourceLock("reusable")
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	unlock2()
}
