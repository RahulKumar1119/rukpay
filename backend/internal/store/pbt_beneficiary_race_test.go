package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Property 27, store half. The goroutines below start together on one channel and
// the tests run under -race. A MemoryStore call runs in microseconds and the fake
// serializes DynamoDBStore calls, so what is checked is the outcome that no
// interleaving may change: exactly one writer wins.

// raceRecord returns a record whose label tells the writer that made it, so the
// winner can be told apart in the stored record.
func raceRecord(base BeneficiaryRecord, writer int) BeneficiaryRecord {
	next := beneCopy(base)
	next.Label = fmt.Sprintf("writer-%02d", writer)
	next.PaymentCount = base.PaymentCount + writer + 1
	return next
}

// raceOutcome is what one racing goroutine did and got back.
type raceOutcome struct {
	del bool
	err error
}

// Feature: risk-intelligence, Property 27: Racing writers produce one winner
//
// For any stored record and 20 to 32 goroutines released together, each of which
// replaces it with a different record or deletes it, all at the version they
// read: exactly one call succeeds. Every other call returns an error of kind
// ErrConditionFailed when the winner was a replace, and ErrNotFound when the
// winner was a delete, because the record is gone then. No call returns another
// error. The stored record equals the winner's record with the version of the
// read plus 1 and a fresh expiry, or no record when the winner was a delete, and
// no loser's write is visible. Both adapters, with the clock moved by any amount
// before the race, as long as the record is still live.
//
// **Validates: Requirements 20.3, 20.9, 27.10**
func TestBeneficiaryRacingWritersProduceOneWinner(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(rt, env)
		payer := benePayerGen().Draw(rt, "payer")
		rec := beneRecordGen().Draw(rt, "record")
		beneMustCreate(rt, bs, payer, rec)
		// A clock move inside the record's life changes the expiry a winner sets.
		env.clock.Advance(rapid.SampledFrom([]time.Duration{0, time.Second, 90 * time.Minute}).Draw(rt, "advance"))

		writers := rapid.IntRange(20, 32).Draw(rt, "writers")
		deletes := rapid.IntRange(0, writers).Draw(rt, "deleters")
		outcomes := make([]raceOutcome, writers)

		var (
			ready, done sync.WaitGroup
			start       = make(chan struct{})
		)
		ready.Add(writers)
		done.Add(writers)
		for i := 0; i < writers; i++ {
			i := i
			outcomes[i].del = i < deletes
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				if outcomes[i].del {
					outcomes[i].err = bs.DeleteBeneficiary(ctx, payer, rec.ID, rec.Version)
					return
				}
				outcomes[i].err = bs.ReplaceBeneficiary(ctx, payer, rec.ID, rec.Version, raceRecord(rec, i))
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		winner := -1
		for i, o := range outcomes {
			if o.err == nil {
				if winner != -1 {
					rt.Fatalf("writers %d and %d both succeeded", winner, i)
				}
				winner = i
			}
		}
		if winner == -1 {
			rt.Fatalf("no writer succeeded: %v", outcomes)
		}

		wantKind := ErrConditionFailed
		if outcomes[winner].del {
			wantKind = ErrNotFound
		}
		for i, o := range outcomes {
			if i == winner {
				continue
			}
			if !errors.Is(o.err, wantKind) {
				rt.Fatalf("loser %d (delete %v) returned %v, want an error of kind %q (the winner %d deleted: %v)",
					i, o.del, o.err, wantKind, winner, outcomes[winner].del)
			}
		}

		if outcomes[winner].del {
			if _, err := bs.GetBeneficiary(ctx, payer, rec.ID); !errors.Is(err, ErrNotFound) {
				rt.Fatalf("Get after a winning delete = %v, want ErrNotFound", err)
			}
			beneWantList(rt, bs, payer, []BeneficiaryRecord{})
			return
		}
		want := beneStored(raceRecord(rec, winner), env.clock.Now().Unix())
		want.Version = rec.Version + 1
		beneWantGet(rt, bs, payer, rec.ID, want)
		beneWantList(rt, bs, payer, []BeneficiaryRecord{want})
	})
}

// Feature: risk-intelligence, Property 27: Racing writers produce one winner
//
// For any stored record and 20 to 22 goroutines that each read the record, add 1
// to its payment count and replace it at the version they read, retrying on a
// refused condition: no update is lost. The final payment count is the first one
// plus the number of goroutines, and the final version is the first one plus the
// same number, because every successful replace adds exactly 1 to the version and
// no two succeed from one version.
//
// **Validates: Requirements 20.3, 20.9, 27.10**
func TestBeneficiaryConcurrentReadModifyWriteLosesNoUpdate(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(rt, env)
		payer := benePayerGen().Draw(rt, "payer")
		rec := beneRecordGen().Draw(rt, "record")
		if len(rec.History) > 3 {
			rec.History = rec.History[:3] // every attempt decodes the history: keep the test fast
		}
		beneMustCreate(rt, bs, payer, rec)

		writers := rapid.IntRange(20, 22).Draw(rt, "writers")
		errs := make([]error, writers)

		var (
			ready, done sync.WaitGroup
			start       = make(chan struct{})
		)
		ready.Add(writers)
		done.Add(writers)
		for i := 0; i < writers; i++ {
			i := i
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				// Every refused attempt means another writer won, so there are at most
				// writers - 1 refusals. A store that refuses a right version would
				// make the loop run for ever, so it stops after many more.
				for attempt := 0; attempt < 100*writers; attempt++ {
					cur, err := bs.GetBeneficiary(ctx, payer, rec.ID)
					if err != nil {
						errs[i] = err
						return
					}
					next := beneCopy(cur)
					next.PaymentCount++
					err = bs.ReplaceBeneficiary(ctx, payer, rec.ID, cur.Version, next)
					if err == nil {
						return
					}
					if !errors.Is(err, ErrConditionFailed) {
						errs[i] = err
						return
					}
				}
				errs[i] = errors.New("gave up: the store kept refusing the version that it had just returned")
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()
		for i, err := range errs {
			if err != nil {
				rt.Fatalf("writer %d stopped with %v, want it to finish", i, err)
			}
		}

		got, err := bs.GetBeneficiary(ctx, payer, rec.ID)
		if err != nil {
			rt.Fatalf("Get = %v, want nil", err)
		}
		if got.PaymentCount != rec.PaymentCount+writers || got.Version != rec.Version+writers {
			rt.Fatalf("after %d increments: payment count %d (want %d), version %d (want %d)",
				writers, got.PaymentCount, rec.PaymentCount+writers, got.Version, rec.Version+writers)
		}
		if !reflect.DeepEqual(got.History, rec.History) {
			rt.Fatalf("the history changed under concurrent updates: %#v, want %#v", got.History, rec.History)
		}
	})
}
