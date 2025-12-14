//go:build linux || darwin

package software

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestLockout_FailedAttemptsIncrement verifies that failed attempts increment the counter.
func TestLockout_FailedAttemptsIncrement(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	stateDir := filepath.Join(tmpDir, "state")

	// Set a fixed time for deterministic tests
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	// Check initial lockout state
	state, err := store.checkLockout()
	if err != nil {
		t.Fatalf("checkLockout() error = %v", err)
	}
	if state.FailedAttempts != 0 {
		t.Errorf("initial FailedAttempts = %d, want 0", state.FailedAttempts)
	}

	// Record first failure
	if err := store.recordFailure(state); err != nil {
		t.Fatalf("recordFailure() error = %v", err)
	}

	// Reload and verify
	state, err = store.checkLockout()
	if err != nil {
		t.Fatalf("checkLockout() after failure error = %v", err)
	}
	if state.FailedAttempts != 1 {
		t.Errorf("FailedAttempts after 1 failure = %d, want 1", state.FailedAttempts)
	}

	// Record more failures
	for i := 2; i <= 5; i++ {
		if err := store.recordFailure(state); err != nil {
			t.Fatalf("recordFailure() #%d error = %v", i, err)
		}
		state, err = store.checkLockout()
		if err != nil {
			t.Fatalf("checkLockout() #%d error = %v", i, err)
		}
		if state.FailedAttempts != i {
			t.Errorf("FailedAttempts after %d failures = %d, want %d", i, state.FailedAttempts, i)
		}
	}

	// Verify file was written
	data := readLockoutFile(t, stateDir)
	if data == nil {
		t.Error("lockout file not created")
	}
}

// TestLockout_TriggersAtThreshold verifies lockout triggers at maxAttemptsTotal (10).
func TestLockout_TriggersAtThreshold(t *testing.T) {
	store, _ := setupTestStore(t)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	state, err := store.checkLockout()
	if err != nil {
		t.Fatalf("checkLockout() error = %v", err)
	}

	// Record 9 failures (no lockout yet)
	for i := 1; i <= 9; i++ {
		err := store.recordFailure(state)
		if err != nil {
			t.Fatalf("recordFailure() #%d should not error before threshold: %v", i, err)
		}
		state, _ = store.checkLockout()
	}

	// 10th failure should trigger lockout
	err = store.recordFailure(state)
	if err == nil {
		t.Error("recordFailure() at threshold should return error")
	}

	// Verify we're locked out
	_, err = store.checkLockout()
	if err == nil {
		t.Error("checkLockout() should return error when locked out")
	}
}

// TestLockout_ExponentialBackoff verifies lockout duration doubles each cycle.
func TestLockout_ExponentialBackoff(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	stateDir := filepath.Join(tmpDir, "state")

	// We need to simulate multiple lockout cycles
	// Lockout happens at 10, 13, 16, 19... attempts (every 3 after 10)
	tests := []struct {
		attempts int
		minDur   time.Duration // Minimum expected duration (before jitter)
		maxDur   time.Duration // Maximum expected duration (with jitter)
	}{
		{10, 5 * time.Minute, 5*time.Minute + 250*time.Millisecond},   // First lockout: 5m
		{13, 10 * time.Minute, 10*time.Minute + 250*time.Millisecond}, // Second: 10m
		{16, 20 * time.Minute, 20*time.Minute + 250*time.Millisecond}, // Third: 20m
		{19, 40 * time.Minute, 40*time.Minute + 250*time.Millisecond}, // Fourth: 40m
		{22, 1 * time.Hour, 1*time.Hour + 250*time.Millisecond},       // Fifth+: capped at 1h
		{25, 1 * time.Hour, 1*time.Hour + 250*time.Millisecond},       // Still capped
	}

	for _, tt := range tests {
		t.Run("attempts_"+string(rune('0'+tt.attempts/10))+string(rune('0'+tt.attempts%10)), func(t *testing.T) {
			// Reset for each test
			deleteLockoutFile(t, stateDir)

			baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
			restore := setNowFunc(mockTime(baseTime))
			defer restore()

			state, _ := store.checkLockout()

			// Fast-forward to target attempts (simulate prior failures)
			state.FailedAttempts = tt.attempts - 1
			state.LastAttempt = baseTime

			// This failure should trigger lockout
			_ = store.recordFailure(state)

			// Read the lockout state and check duration
			data := readLockoutFile(t, stateDir)
			var readState lockoutState
			if err := json.Unmarshal(data, &readState); err != nil {
				t.Fatalf("unmarshal lockout: %v", err)
			}

			duration := readState.LockedUntil.Sub(baseTime)
			if duration < tt.minDur || duration > tt.maxDur {
				t.Errorf("lockout duration = %v, want between %v and %v", duration, tt.minDur, tt.maxDur)
			}
		})
	}
}

// TestJitteredDuration verifies jitter is applied within expected range.
func TestJitteredDuration(t *testing.T) {
	base := 5 * time.Minute

	// Run multiple times to check jitter is applied
	seen := make(map[time.Duration]bool)
	for i := 0; i < 100; i++ {
		result := jitteredDuration(base)
		if result < base {
			t.Errorf("jitteredDuration(%v) = %v, should be >= base", base, result)
		}
		if result > base+250*time.Millisecond {
			t.Errorf("jitteredDuration(%v) = %v, should be <= base + 250ms", base, result)
		}
		seen[result] = true
	}

	// Should see some variation (not all the same)
	if len(seen) < 2 {
		t.Error("jitteredDuration() should produce varied results")
	}
}

// TestLockout_CounterDecaysAfterHour verifies counter resets after 1 hour.
func TestLockout_CounterDecaysAfterHour(t *testing.T) {
	store, _ := setupTestStore(t)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	// Record some failures
	state, _ := store.checkLockout()
	for i := 0; i < 5; i++ {
		_ = store.recordFailure(state)
		state, _ = store.checkLockout()
	}
	if state.FailedAttempts != 5 {
		t.Fatalf("setup: FailedAttempts = %d, want 5", state.FailedAttempts)
	}

	// Advance time by more than 1 hour
	restore()
	restore = setNowFunc(mockTime(baseTime.Add(time.Hour + time.Minute)))
	defer restore()

	// Check lockout - counter should decay to 0
	state, err := store.checkLockout()
	if err != nil {
		t.Fatalf("checkLockout() error = %v", err)
	}
	if state.FailedAttempts != 0 {
		t.Errorf("FailedAttempts after 1h decay = %d, want 0", state.FailedAttempts)
	}
}

// TestLockout_CounterPreservedWithinHour verifies counter is preserved within 1 hour.
func TestLockout_CounterPreservedWithinHour(t *testing.T) {
	store, _ := setupTestStore(t)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	// Record some failures
	state, _ := store.checkLockout()
	for i := 0; i < 5; i++ {
		_ = store.recordFailure(state)
		state, _ = store.checkLockout()
	}

	// Advance time by less than 1 hour
	restore()
	restore = setNowFunc(mockTime(baseTime.Add(59 * time.Minute)))
	defer restore()

	// Check lockout - counter should be preserved
	state, err := store.checkLockout()
	if err != nil {
		t.Fatalf("checkLockout() error = %v", err)
	}
	if state.FailedAttempts != 5 {
		t.Errorf("FailedAttempts within 1h = %d, want 5", state.FailedAttempts)
	}
}

// TestLockout_ConcurrentAccess verifies flock protects concurrent access.
func TestLockout_ConcurrentAccess(t *testing.T) {
	store, _ := setupTestStore(t)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	// Run 20 concurrent goroutines recording failures
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := store.checkLockout()
			if err != nil {
				// May be locked out, that's OK
				return
			}
			if err := store.recordFailure(state); err != nil {
				// Lockout triggered, that's OK
				return
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// Verify the final state is consistent
	state, err := store.checkLockout()
	if err != nil {
		// May be locked out from concurrent failures
		return
	}
	if state.FailedAttempts < 0 || state.FailedAttempts > 20 {
		t.Errorf("FailedAttempts = %d, should be 0-20", state.FailedAttempts)
	}
}

// TestLockout_TamperedFileTriggersMaxLockout verifies HMAC tamper detection.
func TestLockout_TamperedFileTriggersMaxLockout(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	stateDir := filepath.Join(tmpDir, "state")

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	// Record a failure to create a valid lockout file
	state, _ := store.checkLockout()
	_ = store.recordFailure(state)

	// Tamper with the file - modify FailedAttempts but keep the old signature
	data := readLockoutFile(t, stateDir)
	var tamperedState lockoutState
	if err := json.Unmarshal(data, &tamperedState); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tamperedState.FailedAttempts = 0 // Try to reset counter
	// Keep the old signature (it won't match)
	tampered, _ := json.Marshal(tamperedState)
	writeLockoutFile(t, stateDir, tampered)

	// Load the tampered state - should trigger max lockout
	_, err := store.checkLockout()
	if err == nil {
		t.Error("checkLockout() should return error for tampered file")
	}
}

// TestLockout_DeletedFileFreshState verifies deleted file is treated as fresh state.
func TestLockout_DeletedFileFreshState(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	stateDir := filepath.Join(tmpDir, "state")

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	// Record failures
	state, _ := store.checkLockout()
	for i := 0; i < 5; i++ {
		_ = store.recordFailure(state)
		state, _ = store.checkLockout()
	}

	// Delete the lockout file
	deleteLockoutFile(t, stateDir)

	// Check lockout - should be fresh state
	state, err := store.checkLockout()
	if err != nil {
		t.Fatalf("checkLockout() error = %v", err)
	}
	if state.FailedAttempts != 0 {
		t.Errorf("FailedAttempts after delete = %d, want 0", state.FailedAttempts)
	}
}

// TestLockout_HMACConstantTimeComparison verifies constant-time comparison.
// Note: We can't directly test timing, but we verify the code path uses hmac.Equal.
func TestLockout_HMACConstantTimeComparison(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	stateDir := filepath.Join(tmpDir, "state")
	configDir := filepath.Join(tmpDir, "config")

	// Create a public key to derive HMAC key
	pubKey := []byte("age1publickey123...")
	if err := os.WriteFile(filepath.Join(configDir, "age.pub"), pubKey, 0600); err != nil {
		t.Fatalf("write public key: %v", err)
	}

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	// Record a failure to create signed state
	state, _ := store.checkLockout()
	_ = store.recordFailure(state)

	// Read and verify the signature exists
	data := readLockoutFile(t, stateDir)
	var readState lockoutState
	if err := json.Unmarshal(data, &readState); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if readState.Signature == "" {
		t.Error("lockout state should have signature")
	}

	// Verify the signature is valid
	if !store.verifyLockout(&readState) {
		t.Error("verifyLockout() should return true for valid signature")
	}

	// Verify invalid signature is detected
	readState.Signature = "invalid"
	if store.verifyLockout(&readState) {
		t.Error("verifyLockout() should return false for invalid signature")
	}
}

// TestSignLockout_Deterministic verifies same input produces same signature.
func TestSignLockout_Deterministic(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	configDir := filepath.Join(tmpDir, "config")

	// Create a public key to derive HMAC key
	pubKey := []byte("age1publickey123...")
	if err := os.WriteFile(filepath.Join(configDir, "age.pub"), pubKey, 0600); err != nil {
		t.Fatalf("write public key: %v", err)
	}

	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	state := &lockoutState{
		FailedAttempts: 5,
		LastAttempt:    fixedTime,
		LockedUntil:    fixedTime.Add(5 * time.Minute),
	}

	sig1 := store.signLockout(state)
	sig2 := store.signLockout(state)

	if sig1 != sig2 {
		t.Errorf("signLockout() not deterministic: %q != %q", sig1, sig2)
	}

	// Different state should produce different signature
	state.FailedAttempts = 6
	sig3 := store.signLockout(state)
	if sig1 == sig3 {
		t.Error("signLockout() should produce different signature for different state")
	}
}

// TestLockout_SharedBetweenUnlockMethods verifies lockout is shared.
func TestLockout_SharedBetweenUnlockMethods(t *testing.T) {
	store, _ := setupTestStore(t)

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	// Record failures from checkLockout/recordFailure (used by both Unlock methods)
	state, _ := store.checkLockout()
	for i := 0; i < 10; i++ {
		_ = store.recordFailure(state)
		state, _ = store.checkLockout()
	}

	// Both Unlock paths should see the lockout
	_, err := store.checkLockout()
	if err == nil {
		t.Error("checkLockout() should return error after 10 failures")
	}
}

// TestValidatePassphrase_MinLength verifies 12 character minimum.
func TestValidatePassphrase_MinLength(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		wantErr    bool
	}{
		{"empty", "", true},
		{"too short - 1", "a", true},
		{"too short - 11", "12345678901", true},
		{"exact - 12", "123456789012", false},
		{"longer - 20", "12345678901234567890", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassphraseBytes([]byte(tt.passphrase), 12)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePassphraseBytes(%q) error = %v, wantErr = %v", tt.passphrase, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePassphrase_CustomMinLength(t *testing.T) {
	if err := validatePassphraseBytes([]byte("1234567890123456"), 16); err != nil {
		t.Fatalf("expected 16-char passphrase to pass: %v", err)
	}
	if err := validatePassphraseBytes([]byte("shortpass"), 16); err == nil {
		t.Fatalf("expected short passphrase to fail custom min length")
	}
}

// TestHMACKey_DerivedFromPublicKey verifies HMAC key derivation.
func TestHMACKey_DerivedFromPublicKey(t *testing.T) {
	store1, tmpDir1 := setupTestStore(t)
	store2, tmpDir2 := setupTestStore(t)

	configDir1 := filepath.Join(tmpDir1, "config")
	configDir2 := filepath.Join(tmpDir2, "config")

	// Create different public keys
	pubKey1 := []byte("age1publickey-one...")
	pubKey2 := []byte("age1publickey-two...")
	if err := os.WriteFile(filepath.Join(configDir1, "age.pub"), pubKey1, 0600); err != nil {
		t.Fatalf("write public key 1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir2, "age.pub"), pubKey2, 0600); err != nil {
		t.Fatalf("write public key 2: %v", err)
	}

	key1 := store1.hmacKey()
	key2 := store2.hmacKey()

	// Keys should be different
	if bytes.Equal(key1, key2) {
		t.Error("hmacKey() should produce different keys for different public keys")
	}

	// Same store should produce same key
	key1Again := store1.hmacKey()
	if !bytes.Equal(key1, key1Again) {
		t.Error("hmacKey() should be deterministic for same store")
	}
}

// TestClearLockout verifies lockout state is reset on success.
func TestClearLockout(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	stateDir := filepath.Join(tmpDir, "state")

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	restore := setNowFunc(mockTime(baseTime))
	defer restore()

	// Record some failures
	state, _ := store.checkLockout()
	for i := 0; i < 5; i++ {
		_ = store.recordFailure(state)
		state, _ = store.checkLockout()
	}

	// Clear the lockout
	store.clearLockout(state)

	// Verify state is reset
	state, err := store.checkLockout()
	if err != nil {
		t.Fatalf("checkLockout() error = %v", err)
	}
	if state.FailedAttempts != 0 {
		t.Errorf("FailedAttempts after clear = %d, want 0", state.FailedAttempts)
	}
	if !state.LockedUntil.IsZero() {
		t.Errorf("LockedUntil after clear = %v, want zero", state.LockedUntil)
	}

	// Verify file was updated
	data := readLockoutFile(t, stateDir)
	var readState lockoutState
	if err := json.Unmarshal(data, &readState); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if readState.FailedAttempts != 0 {
		t.Errorf("file FailedAttempts = %d, want 0", readState.FailedAttempts)
	}
}

// TestStore_Kind verifies the Kind() method returns Passphrase.
func TestStore_Kind(t *testing.T) {
	store, _ := setupTestStore(t)
	if store.Kind() != Passphrase {
		t.Errorf("Kind() = %v, want %v", store.Kind(), Passphrase)
	}
}
