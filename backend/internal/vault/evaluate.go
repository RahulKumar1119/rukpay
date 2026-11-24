package vault

import (
	"context"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Evaluate scores a payment, creates its vault, records the payment in the
// payer's history and returns the first View (Requirement 1.1). The score, the
// state and the Explanation come from risk.Assess and never from the caller
// (Requirement 2.2). The Explanation is written to the record by the same Create
// call that creates the vault, so no vault exists without one, and later reads
// return it as stored (Requirements 2.3, 2.7).
//
// The steps run in this order, in line, with no goroutine and no timer:
//
//  1. check the identifiers and the signals, before anything is read
//  2. read the clock once; that one time is the evaluation time of every step
//  3. read the History_Snapshot once, before this payment is recorded
//  4. score with risk.Assess, using the snapshot, the time and the recipient
//  5. create the vault, with its Explanation, in one write
//  6. only then, record one History_Event for the payment
//
// It returns store.ErrUnmaskedIdentifier when an identifier is not masked, and a
// *risk.ValidationError when a signal is invalid. Both are returned before any
// record is read or created, and neither comes with a View or an Explanation
// (Requirements 1.6, 1.7, 2.11, 12.2). A failed Create returns the store's error
// and no View, and records no event.
//
// History never makes an evaluation fail. A snapshot that cannot be read counts
// as empty, so the payment is scored on its signals alone (Requirement 12.7). An
// event that cannot be written is dropped, and the vault that was already
// created is returned as it would have been after a successful write
// (Requirement 12.8).
//
// The vault reason is written from the signals and from the behavioural lines of
// the Explanation. With no behavioural line, and for a payment that passes at
// once, it is the same text as before the Explanation existed (Requirements 3.8,
// 3.9, 3.11).
func (s *Service) Evaluate(ctx context.Context, req EvaluateRequest) (View, error) {
	rec := store.VaultRecord{
		PayerAccountMasked: req.PayerAccountMasked,
		BeneficiaryMasked:  req.BeneficiaryMasked,
		UPIMasked:          req.UPIMasked,
		PhoneMasked:        req.PhoneMasked,
	}
	if err := store.ValidateMasked(rec); err != nil {
		return View{}, err
	}
	if err := risk.Validate(req.Signals); err != nil {
		return View{}, err
	}
	id, err := newID()
	if err != nil {
		return View{}, err
	}

	// One reading of the clock serves the snapshot window, the score, the record
	// and the History_Event (Requirement 12.1).
	now := s.nowUnix()
	ref := beneficiaryRef(req)
	a, err := risk.Assess(risk.Input{
		Signals:        req.Signals,
		Now:            now,
		History:        s.snapshot(ctx, now),
		BeneficiaryRef: ref,
	})
	if err != nil {
		return View{}, err
	}

	rec.ID = id
	rec.State = a.State
	rec.RiskScore = a.Score
	rec.AmountINR = req.AmountINR
	rec.CreatedAt = time.Unix(now, 0).UTC()
	rec.Version = 1
	rec.Settled = a.State == store.StateInstantPass
	rec.Reason = reasonForExplained(a.State, req.Signals, a.Explanation)
	rec.Explanation = a.Explanation
	rec.ExpiresAt = time.Unix(now+ttlMargin, 0).UTC()
	if a.State == store.StateLocked15M {
		rec.LockDeadline = now + LockDuration
		rec.TokenExpiry = rec.LockDeadline
		rec.ExpiresAt = time.Unix(rec.LockDeadline+ttlMargin, 0).UTC()
	}

	if err := s.store.Create(ctx, rec); err != nil {
		return View{}, err
	}
	s.recordHistory(ctx, now, req.AmountINR, ref)
	return s.view(rec, now), nil
}

// beneficiaryRef returns the Beneficiary_Ref of a request: the first non-empty
// value among beneficiary_masked, upi_masked and phone_masked, checked in that
// order, and "" when all three are empty (Requirement 12.5). The payer's own
// account is never a recipient, so payer_account_masked plays no part. Evaluate
// has already checked that each of the three is a masked identifier or empty, so
// the result is one of those two and never raw text.
func beneficiaryRef(req EvaluateRequest) string {
	for _, ref := range []string{req.BeneficiaryMasked, req.UPIMasked, req.PhoneMasked} {
		if ref != "" {
			return ref
		}
	}
	return ""
}

// snapshot builds the History_Snapshot of an evaluation at time now: the
// History_Events of the Demo_Payer that are within the History_Retention, oldest
// first, as the store lists them (Requirement 12.4). It reads the store once.
//
// A service with no history store, or a store that fails, gives an empty
// snapshot, so scoring then rests on the signals alone (Requirement 12.7). The
// error is dropped on purpose: the vault package has no logger, and what the
// caller needs is the same whatever the cause.
//
// The retention test is made here, on the event time, and not left to the
// store. A store removes an expired event only after its TTL passes and may be
// late, so an event that is still stored but older than the retention must not
// be scored (Requirement 12.4).
func (s *Service) snapshot(ctx context.Context, now int64) []risk.HistoryEvent {
	if s.history == nil {
		return nil
	}
	events, err := s.history.RecentHistory(ctx, store.PayerKey, store.SnapshotLimit)
	if err != nil {
		return nil
	}

	snap := make([]risk.HistoryEvent, 0, len(events))
	for _, ev := range events {
		if !withinRetention(ev.At, now) {
			continue
		}
		snap = append(snap, risk.HistoryEvent{At: ev.At, AmountINR: ev.AmountINR, BeneficiaryRef: ev.BeneficiaryRef})
	}
	return snap
}

// withinRetention reports whether an event at time at is within the
// History_Retention of an evaluation at time now: its age is less than
// store.HistoryRetentionSeconds. An event exactly that old is out, and an event
// later than now is in (Requirement 12.4).
//
// The subtraction runs in uint64 after the at > now test. With at <= now the
// true difference is from 0 to 2^64-1, which fits, so no pair of int64 values
// can overflow it.
func withinRetention(at, now int64) bool {
	if at > now {
		return true
	}
	return uint64(now)-uint64(at) < store.HistoryRetentionSeconds
}

// recordHistory appends the History_Event of the evaluation at time now, after
// its vault is stored. The event holds a time, an amount, a Beneficiary_Ref that
// is masked or empty, an id of its own and the expiry that this service sets;
// the store keeps that expiry as given (Requirements 12.1, 12.6, 12.10).
//
// The id is 128 random bits and is not the vault id. A vault id is a capability
// that opens the vault, so it must not appear in a second place. Two evaluations
// never share an event id, so concurrent writes never overwrite one another
// (Requirement 12.9).
//
// The write runs in line, never in a goroutine: a Lambda instance can freeze as
// soon as the response is sent, and a background write would be lost. A failure
// is ignored, because the vault is already stored and the caller must see the
// same result as after a successful write (Requirement 12.8). With no history
// store nothing is recorded.
func (s *Service) recordHistory(ctx context.Context, now, amountINR int64, ref string) {
	if s.history == nil {
		return
	}
	id, err := newID()
	if err != nil {
		return
	}
	ev := store.HistoryEvent{
		ID:             id,
		At:             now,
		AmountINR:      amountINR,
		BeneficiaryRef: ref,
		ExpiresAt:      store.HistoryExpiry(now),
	}
	_ = s.history.AppendHistory(ctx, store.PayerKey, ev) // best effort, see above
}
