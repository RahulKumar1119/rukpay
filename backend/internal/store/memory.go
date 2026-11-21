package store

import (
	"context"
	"sync"
	"time"
)

var _ VaultStore = (*MemoryStore)(nil)

// MemoryStore is a thread-safe, in-memory VaultStore for local runs and tests
// (Requirements 8.1, 8.2). Get takes the read lock. Create and UpdateIf take
// the write lock, so the state check and the replacement in UpdateIf happen
// under one lock and exactly one concurrent caller wins (Requirement 8.4).
//
// Records are stored and returned by value. VaultRecord holds only strings,
// integers and time.Time values, so a struct copy is a deep copy and callers
// cannot reach stored data through a returned record (Requirement 8.3).
//
// Expired records are hidden from Get and UpdateIf but are not removed from
// the map: there is no sweeper, and a Create for the id of an expired record
// still returns ErrAlreadyExists, as DynamoDB does until TTL deletes the item.
//
// Use NewMemoryStore to construct a MemoryStore. The zero value is not ready
// for use.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]VaultRecord
	now  func() time.Time // injectable clock, defaults to time.Now
}

// NewMemoryStore returns an empty MemoryStore that reads time from time.Now.
func NewMemoryStore() *MemoryStore {
	return NewMemoryStoreWithClock(time.Now)
}

// NewMemoryStoreWithClock returns an empty MemoryStore that reads time from
// now. The clock decides which records have expired in Get and UpdateIf, so a
// test can drive record expiry and the vault's lock expiry from one fake clock.
// A nil now means time.Now.
func NewMemoryStoreWithClock(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{
		data: make(map[string]VaultRecord),
		now:  now,
	}
}

// Create stores a copy of rec. It returns ErrUnmaskedIdentifier when an
// identifier field is not masked and ErrAlreadyExists when rec.ID is already
// stored. Both leave the store unchanged.
func (s *MemoryStore) Create(ctx context.Context, rec VaultRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateMasked(rec); err != nil {
		return &opError{Op: "create", ID: rec.ID, Err: err}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data[rec.ID]; ok {
		return &opError{Op: "create", ID: rec.ID, Err: ErrAlreadyExists}
	}
	s.data[rec.ID] = rec
	return nil
}

// Get returns a copy of the record stored under id. It returns ErrNotFound
// when id is not stored or the record has expired.
func (s *MemoryStore) Get(ctx context.Context, id string) (VaultRecord, error) {
	if err := ctx.Err(); err != nil {
		return VaultRecord{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.data[id]
	if !ok || s.isExpired(rec) {
		return VaultRecord{}, &opError{Op: "get", ID: id, Err: ErrNotFound}
	}
	return rec, nil
}

// UpdateIf replaces the record stored under id with a copy of next when the
// stored state equals expected. It accepts any pair of states and never
// changes next.Version (Requirement 7.9). It returns ErrUnmaskedIdentifier
// when next has an unmasked identifier, ErrNotFound when id is not stored or
// has expired, and ErrConditionFailed when the stored state differs from
// expected. Every error leaves the stored record unchanged.
func (s *MemoryStore) UpdateIf(ctx context.Context, id string, expected VaultState, next VaultRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateMasked(next); err != nil {
		return &opError{Op: "update", ID: id, Err: err}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.data[id]
	if !ok || s.isExpired(cur) {
		return &opError{Op: "update", ID: id, Err: ErrNotFound}
	}
	if cur.State != expected {
		return &opError{Op: "update", ID: id, Err: ErrConditionFailed}
	}
	s.data[id] = next
	return nil
}

// isExpired reports whether rec has a non-zero ExpiresAt earlier than the
// current time. A zero ExpiresAt means the record never expires. The caller
// must hold s.mu.
func (s *MemoryStore) isExpired(rec VaultRecord) bool {
	return !rec.ExpiresAt.IsZero() && rec.ExpiresAt.Before(s.now())
}
