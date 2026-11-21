package store

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Feature: behavioral-risk-engine, Property 11: Returned records are copies
//
// **Validates: Requirements 8.3**
func TestReturnedRecordsAreCopies(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		rec := contractRecordGen().Draw(rt, "record")
		want := rec

		contractMustCreate(rt, env.vs, rec)

		// Change every field of the returned record.
		got := contractMustGet(rt, env.vs, rec.ID)
		got.ID += "-changed"
		got.State = StateVetoed
		got.RiskScore++
		got.PayerAccountMasked = "changed"
		got.BeneficiaryMasked = "changed"
		got.UPIMasked = "changed"
		got.PhoneMasked = "changed"
		got.AmountINR++
		got.CreatedAt = got.CreatedAt.Add(time.Hour)
		got.Version++
		got.ExpiresAt = got.ExpiresAt.Add(time.Hour)
		got.LockDeadline++
		got.TokenExpiry++
		got.Settled = !got.Settled
		got.Reason += " changed"
		got.EvidenceSummary += " changed"

		// Change the record that was passed to Create, too.
		rec.State = StateVetoed
		rec.AmountINR++
		rec.Version++

		contractWantRecord(rt, env.vs, want.ID, want)
	})
}
