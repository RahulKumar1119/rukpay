package vault

import (
	"context"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Evaluate scores a payment, creates its vault and returns the first View
// (Requirement 1.1). The score, the state and the Explanation come from
// risk.Assess and never from the caller (Requirement 2.2). The Explanation is
// written to the record by the same Create call that creates the vault, so no
// vault exists without one, and later reads return it as stored
// (Requirements 2.3, 2.7).
//
// It returns store.ErrUnmaskedIdentifier when an identifier is not masked, and
// a *risk.ValidationError when a signal is invalid. Both are returned before
// any record is created, and neither comes with a View or an Explanation
// (Requirements 1.6, 1.7, 2.11). A failed Create returns the store's error and
// no View.
//
// The vault reason is still written by reasonFor from the signals alone, so it
// is the same text as before the Explanation existed (Requirements 3.9, 3.11).
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
	a, err := risk.Assess(risk.Input{Signals: req.Signals})
	if err != nil {
		return View{}, err
	}
	id, err := newID()
	if err != nil {
		return View{}, err
	}

	now := s.nowUnix()
	rec.ID = id
	rec.State = a.State
	rec.RiskScore = a.Score
	rec.AmountINR = req.AmountINR
	rec.CreatedAt = time.Unix(now, 0).UTC()
	rec.Version = 1
	rec.Settled = a.State == store.StateInstantPass
	rec.Reason = reasonFor(a.State, req.Signals)
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
	return s.view(rec, now), nil
}
