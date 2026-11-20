package store

import (
	"context"
	"errors"
)

// VaultStore persists vault records. It does persistence only: UpdateIf
// accepts any pair of expected and next states without checking whether the
// transition is allowed. Transition rules belong to the later vault package
// (Requirement 7.9).
//
// Every method honors ctx and returns ctx.Err() when the context is already
// cancelled (Requirement 7.10).
type VaultStore interface {
	// Create stores rec. It returns ErrAlreadyExists when rec.ID is already
	// stored and leaves the stored record unchanged (Requirement 7.3).
	Create(ctx context.Context, rec VaultRecord) error

	// Get returns the record stored under id. It returns ErrNotFound when id
	// is not stored or the record has expired (Requirement 7.5).
	Get(ctx context.Context, id string) (VaultRecord, error)

	// UpdateIf replaces the record stored under id with next in one atomic
	// operation, but only when the stored state equals expected. It returns
	// ErrConditionFailed when the states differ (Requirement 7.7) and
	// ErrNotFound when id is not stored (Requirement 7.8).
	UpdateIf(ctx context.Context, id string, expected VaultState, next VaultRecord) error
}

// Sentinel errors returned by VaultStore implementations, wrapped in an
// opError. Callers check them with errors.Is.
var (
	ErrAlreadyExists      = errors.New("vault already exists")
	ErrNotFound           = errors.New("vault not found")
	ErrConditionFailed    = errors.New("vault state condition failed")
	ErrUnmaskedIdentifier = errors.New("unmasked identifier")
)

// opError carries the operation and vault id only, never record field values
// (Requirement 12.6). Err is a sentinel or a wrapped client error.
type opError struct {
	Op  string
	ID  string
	Err error
}

func (e *opError) Error() string { return e.Op + " " + e.ID + ": " + e.Err.Error() }

func (e *opError) Unwrap() error { return e.Err }
