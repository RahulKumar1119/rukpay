// Package vault holds the rules of the cooling-off vault: the permitted state
// changes, the 900-second lock, lazy expiry, the reality check, reconfirmation
// and the demo fast-forward.
//
// The package starts no goroutine and no timer. A locked vault becomes EXPIRED
// when it is next read or acted on after its deadline, so a frozen Lambda
// instance cannot skip an expiry (Requirements 4.2, 4.3). Every write goes
// through VaultStore.UpdateIf with the state the caller last read, so exactly
// one of several concurrent callers wins (Requirements 2.5 to 2.7).
package vault

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

const (
	// LockDuration is how long a locked vault holds the payment, in seconds.
	LockDuration = 900

	// fastForwardSeconds is how far from now the demo fast-forward moves a
	// lock deadline.
	fastForwardSeconds = 3

	// ttlMargin is how long a record outlives the moment it needs to exist, in
	// seconds, so that DynamoDB TTL never removes an active vault
	// (Requirement 1.8).
	ttlMargin = 3600

	// idBytes is the size of a vault id before hex encoding: 128 bits
	// (Requirement 1.5).
	idBytes = 16
)

// EvidenceBuilder writes the evidence summary for a vetoed vault. guardian
// supplies the real one through SetEvidenceBuilder, which keeps vault from
// importing guardian.
type EvidenceBuilder func(rec store.VaultRecord, vetoedAt time.Time) string

// Service runs the vault operations. Create it with New.
type Service struct {
	store    store.VaultStore
	clock    Clock
	demo     bool
	issuer   TokenIssuer
	observer Observer
	evidence EvidenceBuilder
}

// New returns a Service over s. A nil clock means time.Now. A nil issuer leaves
// the guardian token out of every View, and a nil observer drops the change
// notifications. demo turns on FastForward.
func New(s store.VaultStore, c Clock, demo bool, i TokenIssuer, o Observer) *Service {
	if c == nil {
		c = time.Now
	}
	if o == nil {
		o = noopObserver{}
	}
	return &Service{
		store:    s,
		clock:    c,
		demo:     demo,
		issuer:   i,
		observer: o,
		evidence: defaultEvidence,
	}
}

// SetEvidenceBuilder sets the function that writes the evidence summary of a
// vetoed vault. A nil builder restores the default. Call it once, before the
// service handles requests.
func (s *Service) SetEvidenceBuilder(b EvidenceBuilder) {
	if b == nil {
		b = defaultEvidence
	}
	s.evidence = b
}

// defaultEvidence is the summary used until guardian supplies its own. It holds
// the vault id, the time and the helpline details, and no identifier.
func defaultEvidence(rec store.VaultRecord, vetoedAt time.Time) string {
	return strings.Join([]string{
		msgEvidenceTitle,
		"Vault: " + rec.ID,
		"Stopped at: " + vetoedAt.UTC().Format(time.RFC3339),
		msgHelpline,
		msgReportOnline,
	}, "\n")
}

// nowUnix returns the current time in epoch seconds.
func (s *Service) nowUnix() int64 { return s.clock().Unix() }

// Get returns the current View of a vault, after lazy expiry (Requirement 7.5).
// It returns an error wrapping store.ErrNotFound for an unknown id.
func (s *Service) Get(ctx context.Context, id string) (View, error) {
	rec, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.view(rec, s.nowUnix()), nil
}

// RealityCheck evaluates the answers to the three safety questions for a vault
// in REALITY_CHECK (Requirement 3). A scam sign locks the vault for 900 seconds.
// Calm answers settle it and leave it in REALITY_CHECK.
//
// It returns ErrInvalidAnswers before touching the store when an answer is not
// YES or NO. It returns a *ConflictError when the vault is not in
// REALITY_CHECK, or has already passed its check, so a passed check cannot be
// submitted again with different answers (Requirement 3.8).
func (s *Service) RealityCheck(ctx context.Context, id string, a Answers) (View, error) {
	lock, err := CheckAnswers(a)
	if err != nil {
		return View{}, err
	}

	now := s.nowUnix()
	unsettled := func(rec store.VaultRecord) bool { return !rec.Settled }

	var rec store.VaultRecord
	if lock {
		rec, err = s.apply(ctx, id, store.StateRealityCheck, store.StateLocked15M, unsettled,
			func(r *store.VaultRecord) {
				r.LockDeadline = now + LockDuration
				r.TokenExpiry = r.LockDeadline
				r.ExpiresAt = time.Unix(r.LockDeadline+ttlMargin, 0).UTC()
				r.Settled = false
				r.Reason = reasonForAnswers(a)
			})
	} else {
		rec, err = s.apply(ctx, id, store.StateRealityCheck, store.StateRealityCheck, unsettled,
			func(r *store.VaultRecord) {
				r.Settled = true
				r.Reason = redact.Text(msgCheckPassed)
			})
	}
	if err != nil {
		return View{}, err
	}
	return s.view(rec, now), nil
}

// Reconfirm records the final calm confirmation of an EXPIRED vault. The vault
// stays EXPIRED and becomes settled (Requirement 4.5). It returns a
// *ConflictError for a vault in any other state (Requirement 4.6).
func (s *Service) Reconfirm(ctx context.Context, id string) (View, error) {
	rec, err := s.apply(ctx, id, store.StateExpired, store.StateExpired, nil,
		func(r *store.VaultRecord) {
			r.Settled = true
			r.Reason = redact.Text(msgReconfirmed)
		})
	if err != nil {
		return View{}, err
	}
	return s.view(rec, s.nowUnix()), nil
}

// Approve moves a LOCKED_15M vault to GUARDIAN_APPROVED and settles it
// (Requirement 6.1). The caller has already checked the guardian token. It
// returns a *ConflictError when the vault is in any other state, which includes
// a vault whose deadline has passed (Requirement 12.10).
func (s *Service) Approve(ctx context.Context, id string) (View, error) {
	rec, err := s.apply(ctx, id, store.StateLocked15M, store.StateGuardianApproved, nil,
		func(r *store.VaultRecord) {
			r.Settled = true
			r.Reason = redact.Text(msgApproved)
		})
	if err != nil {
		return View{}, err
	}
	return s.view(rec, s.nowUnix()), nil
}

// Veto moves a LOCKED_15M vault to VETOED, cancels the payment and stores an
// evidence summary (Requirement 6.3). The caller has already checked the
// guardian token. It returns a *ConflictError when the vault is in any other
// state.
func (s *Service) Veto(ctx context.Context, id string) (View, error) {
	at := time.Unix(s.nowUnix(), 0).UTC()
	rec, err := s.apply(ctx, id, store.StateLocked15M, store.StateVetoed, nil,
		func(r *store.VaultRecord) {
			r.Settled = false
			r.Reason = redact.Text(msgVetoed)
			r.EvidenceSummary = redact.Text(s.evidence(*r, at))
		})
	if err != nil {
		return View{}, err
	}
	return s.view(rec, s.nowUnix()), nil
}

// FastForward shortens the lock of a LOCKED_15M vault to three seconds from now,
// for the demo (Requirement 9.1). It never moves the deadline later and never
// changes TokenExpiry, so the guardian token keeps working (Requirements 9.5,
// 9.6). A deadline that is already within three seconds is left alone and
// nothing is written (Requirement 9.4).
//
// It returns ErrNotDemoMode, before reading the vault, when Demo_Mode is off,
// and a *ConflictError when the vault is not in LOCKED_15M.
func (s *Service) FastForward(ctx context.Context, id string) (View, error) {
	if !s.demo {
		return View{}, ErrNotDemoMode
	}

	rec, err := s.load(ctx, id)
	if err != nil {
		return View{}, err
	}
	if rec.State != store.StateLocked15M {
		return View{}, s.conflict(rec)
	}

	now := s.nowUnix()
	target := now + fastForwardSeconds
	if rec.LockDeadline <= target {
		return s.view(rec, now), nil
	}

	rec, err = s.apply(ctx, id, store.StateLocked15M, store.StateLocked15M, nil,
		func(r *store.VaultRecord) {
			if r.LockDeadline > target {
				r.LockDeadline = target
			}
		})
	if err != nil {
		return View{}, err
	}
	return s.view(rec, now), nil
}

// load reads a record and applies lazy expiry. A LOCKED_15M record at or past
// its deadline is moved to EXPIRED through UpdateIf before the caller sees it
// (Requirement 4.2). If another caller changes the record first, load returns
// what that caller wrote.
func (s *Service) load(ctx context.Context, id string) (store.VaultRecord, error) {
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		return rec, err
	}
	now := s.nowUnix()
	if rec.State != store.StateLocked15M || now < rec.LockDeadline {
		return rec, nil
	}

	next := rec
	next.State = store.StateExpired
	next.Settled = false
	next.Version = rec.Version + 1
	next.Reason = redact.Text(msgExpired)

	err = s.store.UpdateIf(ctx, id, store.StateLocked15M, next)
	switch {
	case err == nil:
		s.observer.Publish(s.view(next, now))
		return next, nil
	case errors.Is(err, store.ErrConditionFailed):
		return s.store.Get(ctx, id)
	default:
		return rec, err
	}
}

// apply runs one guarded change to the record under id.
//
// The record must be in from. When to differs from from, the move must be one
// that CanTransition allows. When it is the same, from must be a state that
// allows an in-place change. Either check fails with ErrInvalidTransition
// before the store is read or written (Requirement 2.3). guard, when not nil,
// is an extra condition on the loaded record. mutate edits a copy of the
// record, and apply then sets its state to to and adds one to its Version.
//
// It returns a *ConflictError carrying the current View when the record is not
// in from, when guard rejects it, or when UpdateIf loses a race
// (Requirement 2.6). It tells the observer after a successful write.
func (s *Service) apply(
	ctx context.Context,
	id string,
	from, to store.VaultState,
	guard func(store.VaultRecord) bool,
	mutate func(*store.VaultRecord),
) (store.VaultRecord, error) {
	if to != from && !CanTransition(from, to) {
		return store.VaultRecord{}, ErrInvalidTransition
	}
	if to == from && !canStay(from) {
		return store.VaultRecord{}, ErrInvalidTransition
	}

	rec, err := s.load(ctx, id)
	if err != nil {
		return rec, err
	}
	if rec.State != from || (guard != nil && !guard(rec)) {
		return rec, s.conflict(rec)
	}

	next := rec
	if mutate != nil {
		mutate(&next)
	}
	next.State = to
	next.Version = rec.Version + 1

	err = s.store.UpdateIf(ctx, id, from, next)
	switch {
	case err == nil:
		s.observer.Publish(s.view(next, s.nowUnix()))
		return next, nil
	case errors.Is(err, store.ErrConditionFailed):
		cur, gerr := s.load(ctx, id)
		if gerr != nil {
			return cur, gerr
		}
		return cur, s.conflict(cur)
	default:
		return rec, err
	}
}

// conflict builds the error for a vault that is not in the state an action
// needs. It carries the current View.
func (s *Service) conflict(rec store.VaultRecord) error {
	return &ConflictError{Current: s.view(rec, s.nowUnix())}
}

// newID returns a random vault id: 16 bytes from crypto/rand, hex encoded, so
// 32 characters of 128-bit entropy (Requirement 1.5).
func newID() (string, error) {
	var b [idBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("vault: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
