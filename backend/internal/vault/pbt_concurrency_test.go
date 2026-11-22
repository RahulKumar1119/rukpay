package vault

import (
	"context"
	"errors"
	"sync"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Feature: cooling-off-vault, Property 5: Concurrent callers produce exactly one winner
//
// At least 50 goroutines approve or veto one locked vault at the same moment.
// Exactly one succeeds, the rest get a conflict, the final state is the
// winner's target, and only the winner's change is published. Run it with
// go test -race.
//
// **Validates: Requirements 2.5, 2.6, 2.7, 12.9**
func TestConcurrentCallersProduceExactlyOneWinner(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		approves := rapid.SliceOfN(rapid.Bool(), 50, 64).Draw(rt, "approve_or_veto")

		e := newEnv(false)
		v := e.create(rt, signalsLocked())
		published := e.obs.count()

		var (
			ready sync.WaitGroup
			done  sync.WaitGroup
			start = make(chan struct{})
			errs  = make([]error, len(approves))
		)
		ready.Add(len(approves))
		done.Add(len(approves))
		for i, approve := range approves {
			i, approve := i, approve
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				if approve {
					_, errs[i] = e.svc.Approve(context.Background(), v.ID)
				} else {
					_, errs[i] = e.svc.Veto(context.Background(), v.ID)
				}
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		winner, winners := -1, 0
		for i, err := range errs {
			if err == nil {
				winner, winners = i, winners+1
				continue
			}
			var conflict *ConflictError
			if !errors.As(err, &conflict) {
				rt.Fatalf("caller %d got %v, want nil or *ConflictError", i, err)
			}
		}
		if winners != 1 {
			rt.Fatalf("%d callers succeeded, want exactly 1", winners)
		}

		want := store.StateVetoed
		if approves[winner] {
			want = store.StateGuardianApproved
		}
		if rec := e.record(rt, v.ID); rec.State != want {
			rt.Fatalf("final state %s, want %s, the winner's target", rec.State, want)
		}
		if got := e.obs.count() - published; got != 1 {
			rt.Fatalf("%d changes were published, want 1", got)
		}
	})
}
