package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestNewMemoryStoreWithClockExpiresRecordsByTheInjectedClock checks that the
// clock given to NewMemoryStoreWithClock, and no other, decides when a record
// has expired. The wall clock never matters, so the vault tests can drive record
// expiry and lock expiry from one fake clock.
func TestNewMemoryStoreWithClockExpiresRecordsByTheInjectedClock(t *testing.T) {
	ctx := context.Background()
	clock := newContractClock(contractEpoch())
	s := NewMemoryStoreWithClock(clock.Now)

	rec := examplesRecord("vault-a", StateLocked15M)
	rec.ExpiresAt = contractEpoch().Add(time.Hour)
	if err := s.Create(ctx, rec); err != nil {
		t.Fatalf("Create = %v, want nil", err)
	}

	steps := []struct {
		name    string
		set     time.Time
		wantErr error
	}{
		{name: "before expiry", set: rec.ExpiresAt.Add(-time.Second), wantErr: nil},
		{name: "at expiry", set: rec.ExpiresAt, wantErr: nil},
		{name: "after expiry", set: rec.ExpiresAt.Add(time.Second), wantErr: ErrNotFound},
		{name: "back before expiry", set: rec.ExpiresAt.Add(-time.Minute), wantErr: nil},
	}
	for _, step := range steps {
		clock.Set(step.set)
		_, err := s.Get(ctx, "vault-a")
		if !errors.Is(err, step.wantErr) && !(step.wantErr == nil && err == nil) {
			t.Errorf("%s: Get = %v, want %v", step.name, err, step.wantErr)
		}
	}

	// UpdateIf reads the same clock.
	clock.Set(rec.ExpiresAt.Add(time.Second))
	err := s.UpdateIf(ctx, "vault-a", StateLocked15M, examplesRecord("vault-a", StateVetoed))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateIf after expiry = %v, want ErrNotFound", err)
	}
}

// TestNewMemoryStoreDelegatesToTheWallClock checks that NewMemoryStore and a nil
// clock both fall back to time.Now: a record that expires far in the past is
// gone, and one far in the future is there.
func TestNewMemoryStoreDelegatesToTheWallClock(t *testing.T) {
	ctx := context.Background()
	stores := map[string]*MemoryStore{
		"NewMemoryStore":               NewMemoryStore(),
		"NewMemoryStoreWithClock(nil)": NewMemoryStoreWithClock(nil),
	}
	for name, s := range stores {
		past := examplesRecord("past", StateLocked15M)
		past.ExpiresAt = time.Now().Add(-time.Hour)
		future := examplesRecord("future", StateLocked15M)
		future.ExpiresAt = time.Now().Add(time.Hour)
		for _, rec := range []VaultRecord{past, future} {
			if err := s.Create(ctx, rec); err != nil {
				t.Fatalf("%s: Create(%s) = %v, want nil", name, rec.ID, err)
			}
		}

		if _, err := s.Get(ctx, "past"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: Get of an expired record = %v, want ErrNotFound", name, err)
		}
		if _, err := s.Get(ctx, "future"); err != nil {
			t.Errorf("%s: Get of a live record = %v, want nil", name, err)
		}
	}
}
