package store

import (
	"context"
	"sort"
)

// CreateBeneficiary stores a copy of rec under payer with the store's ExpiresAt.
// See BeneficiaryStore. An expired record still counts as stored, as for vaults
// and history events.
func (s *MemoryStore) CreateBeneficiary(ctx context.Context, payer string, rec BeneficiaryRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateMaskedIdentifier(rec.IdentifierMasked); err != nil {
		return &opError{Op: opCreateBeneficiary, ID: rec.ID, Err: err}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	records := s.bene[payer]
	if _, ok := records[rec.ID]; ok {
		return &opError{Op: opCreateBeneficiary, ID: rec.ID, Err: ErrAlreadyExists}
	}
	if records == nil {
		records = make(map[string]BeneficiaryRecord)
		s.bene[payer] = records
	}
	stored := rec.clone()
	stored.ExpiresAt = beneficiaryExpiry(s.now())
	records[rec.ID] = stored
	return nil
}

// GetBeneficiary returns a copy of the unexpired record stored under (payer,
// id). See BeneficiaryStore.
func (s *MemoryStore) GetBeneficiary(ctx context.Context, payer, id string) (BeneficiaryRecord, error) {
	if err := ctx.Err(); err != nil {
		return BeneficiaryRecord{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.bene[payer][id]
	if !ok || beneficiaryExpired(rec, s.now()) {
		return BeneficiaryRecord{}, &opError{Op: opGetBeneficiary, ID: id, Err: ErrNotFound}
	}
	return rec.clone(), nil
}

// ListBeneficiaries returns copies of the unexpired records of payer in
// ascending id order. See BeneficiaryStore.
func (s *MemoryStore) ListBeneficiaries(ctx context.Context, payer string) ([]BeneficiaryRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	stored := s.bene[payer]
	ids := make([]string, 0, len(stored))
	now := s.now()
	for id, rec := range stored {
		if !beneficiaryExpired(rec, now) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	out := make([]BeneficiaryRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, stored[id].clone())
	}
	return out, nil
}

// ReplaceBeneficiary replaces the record under (payer, id) with a copy of next
// when the stored version equals expectedVersion. The missing-or-expired check,
// the version check and the write happen under one write lock, so exactly one of
// many concurrent callers with the same expected version wins. See
// BeneficiaryStore.
func (s *MemoryStore) ReplaceBeneficiary(ctx context.Context, payer, id string, expectedVersion int, next BeneficiaryRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateMaskedIdentifier(next.IdentifierMasked); err != nil {
		return &opError{Op: opReplaceBeneficiary, ID: id, Err: err}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	cur, ok := s.bene[payer][id]
	if !ok || beneficiaryExpired(cur, now) {
		return &opError{Op: opReplaceBeneficiary, ID: id, Err: ErrNotFound}
	}
	if cur.Version != expectedVersion {
		return &opError{Op: opReplaceBeneficiary, ID: id, Err: ErrConditionFailed}
	}
	stored := next.clone()
	stored.Version = expectedVersion + 1
	stored.ExpiresAt = beneficiaryExpiry(now)
	s.bene[payer][id] = stored
	return nil
}

// DeleteBeneficiary removes the record under (payer, id) when the stored version
// equals expectedVersion, under the same lock discipline as ReplaceBeneficiary.
// See BeneficiaryStore.
func (s *MemoryStore) DeleteBeneficiary(ctx context.Context, payer, id string, expectedVersion int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.bene[payer][id]
	if !ok || beneficiaryExpired(cur, s.now()) {
		return &opError{Op: opDeleteBeneficiary, ID: id, Err: ErrNotFound}
	}
	if cur.Version != expectedVersion {
		return &opError{Op: opDeleteBeneficiary, ID: id, Err: ErrConditionFailed}
	}
	delete(s.bene[payer], id)
	return nil
}
