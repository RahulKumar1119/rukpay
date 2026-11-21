package store

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
)

// TestMemoryStoreUpdateIfConcurrentCallersOneWinner starts many goroutines that
// call UpdateIf on one id with the same expected state. Exactly one call
// succeeds and the others get ErrConditionFailed (Requirements 8.2, 8.4). Run it
// with go test -race.
func TestMemoryStoreUpdateIfConcurrentCallersOneWinner(t *testing.T) {
	const (
		callers = 50
		rounds  = 20
	)
	ctx := context.Background()

	for round := 0; round < rounds; round++ {
		id := "vault-" + strconv.Itoa(round)
		s := NewMemoryStore()
		if err := s.Create(ctx, examplesRecord(id, StateLocked15M)); err != nil {
			t.Fatalf("round %d: Create = %v, want nil", round, err)
		}

		var (
			ready sync.WaitGroup
			done  sync.WaitGroup
			start = make(chan struct{})
			errs  = make([]error, callers)
		)
		ready.Add(callers)
		done.Add(callers)
		for i := 0; i < callers; i++ {
			i := i
			go func() {
				defer done.Done()

				// Each caller proposes a different next record, tagged by
				// Version, so the winner can be told apart.
				next := examplesRecord(id, StateGuardianApproved)
				next.Version = i + 1

				ready.Done()
				<-start
				errs[i] = s.UpdateIf(ctx, id, StateLocked15M, next)
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		winner := -1
		for i, err := range errs {
			switch {
			case err == nil:
				if winner != -1 {
					t.Fatalf("round %d: callers %d and %d both succeeded", round, winner, i)
				}
				winner = i
			case !errors.Is(err, ErrConditionFailed):
				t.Fatalf("round %d: caller %d got %v, want nil or ErrConditionFailed", round, i, err)
			}
		}
		if winner == -1 {
			t.Fatalf("round %d: no caller succeeded", round)
		}

		got, err := s.Get(ctx, id)
		if err != nil {
			t.Fatalf("round %d: Get = %v, want nil", round, err)
		}
		if got.State != StateGuardianApproved || got.Version != winner+1 {
			t.Fatalf("round %d: stored state %q version %d, want %q version %d (the winner's record)",
				round, got.State, got.Version, StateGuardianApproved, winner+1)
		}
	}
}
