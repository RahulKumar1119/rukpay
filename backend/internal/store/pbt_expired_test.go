package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Feature: behavioral-risk-engine, Property 12: Expired records are not found
//
// A record is found up to and including the instant of its expires_at, and not
// found from one second after it.
//
// **Validates: Requirements 7.4, 9.4**
func TestExpiredRecordsAreNotFound(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		rec := contractRecordGen().Draw(rt, "record")
		pastBy := rapid.OneOf(
			rapid.Just(int64(1)),
			rapid.Int64Range(1, 30*86400),
		).Draw(rt, "past_by_s")

		contractMustCreate(rt, env.vs, rec)

		// Before expiry the record is there.
		contractWantRecord(rt, env.vs, rec.ID, rec)

		// At the instant of expiry it has not yet passed.
		env.clock.Set(rec.ExpiresAt)
		contractWantRecord(rt, env.vs, rec.ID, rec)

		// Once the clock is later than expires_at the record is gone.
		env.clock.Set(rec.ExpiresAt.Add(time.Duration(pastBy) * time.Second))
		_, err := env.vs.Get(context.Background(), rec.ID)
		if !errors.Is(err, ErrNotFound) {
			rt.Fatalf("Get(id %q) at %v after expires_at %v = %v, want ErrNotFound",
				rec.ID, env.clock.Now(), rec.ExpiresAt, err)
		}
	})
}
