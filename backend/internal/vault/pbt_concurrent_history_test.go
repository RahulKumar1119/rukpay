package vault

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"pgregory.net/rapid"
)

// Feature: risk-intelligence, Property 18: Concurrent evaluations leave one event each
//
// At least 50, and up to 200, evaluations that pass validation are released at
// the same moment against one store in which no write fails. Every one creates a
// vault, and after all of them return the store holds exactly one new event for
// each: the count counted in the store grows by the number of evaluations, no two
// events share an id, no event id is a vault id, and the events hold exactly the
// time, amount and recipient of the requests, as a multiset. All of them share one
// clock time, so the events differ only by id and none overwrites another. Each
// returned Explanation adds up to its score. Run it with go test -race.
//
// **Validates: Requirements 12.9, 27.10**
func TestConcurrentEvaluationsLeaveOneEventEach(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Most runs use the smallest crowd the requirement names, which keeps 1,000
		// checks quick under the race detector, and one in six reaches 200.
		n := rapid.OneOf(
			rapid.IntRange(50, 64), rapid.IntRange(50, 64), rapid.IntRange(50, 64),
			rapid.IntRange(50, 64), rapid.IntRange(50, 64), rapid.IntRange(50, 200),
		).Draw(rt, "evaluations")
		// A few drawn requests, shared out among the goroutines, keep the cost of
		// generating inputs small next to the cost of the evaluations themselves.
		templates := make([]EvaluateRequest, rapid.IntRange(1, 6).Draw(rt, "distinct_requests"))
		for i := range templates {
			templates[i] = pooledIdentifiersGen().Draw(rt, "identifiers")
			templates[i].Signals = explainSignalsGen().Draw(rt, "signals")
		}
		reqs := make([]EvaluateRequest, n)
		for i := range reqs {
			reqs[i] = templates[i%len(templates)]
		}

		e := newHistEnv(false)
		for i, m := 0, rapid.IntRange(0, 10).Draw(rt, "seed_count"); i < m; i++ {
			e.seed(rt, eventGen(seedID("r", i)).Draw(rt, fmt.Sprintf("seed_%d", i)))
		}
		before := len(e.stored(rt))

		var (
			ready sync.WaitGroup
			done  sync.WaitGroup
			start = make(chan struct{})
			views = make([]View, n)
			errs  = make([]error, n)
		)
		ready.Add(n)
		done.Add(n)
		for i := range reqs {
			i := i
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				views[i], errs[i] = e.svc.Evaluate(context.Background(), reqs[i])
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		vaultIDs := make(map[string]bool, n)
		for i, err := range errs {
			if err != nil {
				rt.Fatalf("evaluation %d = %v, want nil", i, err)
			}
			if vaultIDs[views[i].ID] {
				rt.Fatalf("vault id %q was issued twice", views[i].ID)
			}
			vaultIDs[views[i].ID] = true

			sum := 0
			for _, c := range *views[i].Explanation {
				sum += c.Points
			}
			if sum != views[i].RiskScore {
				rt.Fatalf("evaluation %d: the points add up to %d but the score is %d", i, sum, views[i].RiskScore)
			}
		}

		// The count in the store, and not in a snapshot, grew by exactly n.
		if got := len(e.stored(rt)) - before; got != n {
			rt.Fatalf("the store holds %d new events after %d evaluations, want %d", got, n, n)
		}

		// The events written are n distinct ones, none of them a vault id, that
		// say what the requests said.
		type shape struct {
			at, amount int64
			ref        string
		}
		want := make(map[shape]int, n)
		for _, req := range reqs {
			want[shape{histEpoch(), req.AmountINR, wantRef(req)}]++
		}
		appends := e.hist.appendCalls()
		if len(appends) != n {
			rt.Fatalf("%d event writes were made, want %d", len(appends), n)
		}
		eventIDs := make(map[string]bool, n)
		for _, a := range appends {
			if eventIDs[a.ev.ID] || vaultIDs[a.ev.ID] {
				rt.Fatalf("event id %q is shared with another event or a vault", a.ev.ID)
			}
			eventIDs[a.ev.ID] = true
			want[shape{a.ev.At, a.ev.AmountINR, a.ev.BeneficiaryRef}]--
		}
		for s, left := range want {
			if left != 0 {
				rt.Fatalf("the events and the requests differ by %d for %+v", left, s)
			}
		}

		// Every event written is in the store.
		held := make(map[string]bool, n)
		for _, ev := range e.stored(rt) {
			held[ev.ID] = true
		}
		for id := range eventIDs {
			if !held[id] {
				rt.Fatalf("event %q was written but is not in the store", id)
			}
		}
	})
}

// The history writes of concurrent evaluations never overwrite one another, even
// when the clock stands still: 64 payments of the same amount to the same
// recipient at one second leave 64 events.
func TestConcurrentIdenticalPaymentsAtOneSecondAllSurvive(t *testing.T) {
	const n = 64
	e := newHistEnv(false)
	req := EvaluateRequest{Signals: signalsInstantPass(), UPIMasked: "r***@okaxis"}

	var (
		ready, done sync.WaitGroup
		start       = make(chan struct{})
		errs        = make([]error, n)
	)
	ready.Add(n)
	done.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			_, errs[i] = e.svc.Evaluate(context.Background(), req)
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("evaluation %d = %v, want nil", i, err)
		}
	}
	events := e.stored(t)
	if len(events) != n {
		t.Fatalf("the store holds %d events, want %d", len(events), n)
	}
	ids := make(map[string]bool, n)
	for _, ev := range events {
		if ev.At != histEpoch() || ev.AmountINR != req.AmountINR || ev.BeneficiaryRef != req.UPIMasked || ids[ev.ID] {
			t.Fatalf("event %+v is wrong or repeats an id", ev)
		}
		ids[ev.ID] = true
	}
}
