package guardian

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// hubEvent builds the event number n for a vault. The reason carries n so a
// subscriber can tell which events it received and in what order.
func hubEvent(id string, n int) vault.View {
	return vault.View{
		ID:            id,
		State:         store.StateLocked15M,
		Reason:        "event-" + strconv.Itoa(n),
		GuardianToken: "tok:" + id, // the hub must strip this
		Metadata:      vault.Metadata{AmountINR: int64(n)},
	}
}

// decodeEvent parses one event from a subscription channel.
func decodeEvent(rt *rapid.T, raw []byte) vault.View {
	rt.Helper()
	var v vault.View
	if err := json.Unmarshal(raw, &v); err != nil {
		rt.Fatalf("event %q is not a View: %v", raw, err)
	}
	return v
}

// publishWithin runs Publish and fails the test if it does not return promptly.
func publishWithin(rt *rapid.T, h *Hub, v vault.View) {
	rt.Helper()
	done := make(chan struct{})
	go func() {
		h.Publish(v)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		rt.Fatalf("Publish blocked")
	}
}

// Feature: cooling-off-vault, Property 19: Hub never blocks and cleans up
//
// Subscribers that read get every event for their own vault id, in order and
// without the guardian token, and none for another vault. Subscribers that never
// read cannot block Publish. After cancel the hub holds nothing for them.
//
// **Validates: Requirements 8.2, 8.3, 8.8**
func TestHubNeverBlocksAndCleansUp(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		vaults := rapid.IntRange(1, 3).Draw(rt, "vaults")
		readersPer := rapid.IntRange(0, 4).Draw(rt, "readers_per_vault")
		stuckPer := rapid.IntRange(0, 4).Draw(rt, "stuck_per_vault")
		events := rapid.IntRange(1, 25).Draw(rt, "events_per_vault")

		ids := make([]string, vaults)
		for i := range ids {
			ids[i] = "vault-" + strconv.Itoa(i)
		}

		h := NewHub()
		type sub struct {
			id     string
			ch     <-chan []byte
			cancel func()
			reader bool
			seen   []vault.View
		}
		var subs []*sub
		for _, id := range ids {
			for i := 0; i < readersPer+stuckPer; i++ {
				ch, cancel := h.Subscribe(id)
				subs = append(subs, &sub{id: id, ch: ch, cancel: cancel, reader: i < readersPer})
			}
		}
		for _, id := range ids {
			if got, want := h.Subscribers(id), readersPer+stuckPer; got != want {
				rt.Fatalf("Subscribers(%s) = %d, want %d", id, got, want)
			}
		}

		// Publish events for every vault in turn. Readers drain after each event.
		for n := 0; n < events; n++ {
			for _, id := range ids {
				publishWithin(rt, h, hubEvent(id, n))
				for _, s := range subs {
					if !s.reader {
						continue
					}
					for drained := false; !drained; {
						select {
						case raw := <-s.ch:
							s.seen = append(s.seen, decodeEvent(rt, raw))
						default:
							drained = true
						}
					}
				}
			}
		}

		for _, s := range subs {
			if s.reader {
				if len(s.seen) != events {
					rt.Fatalf("a reader of %s saw %d events, want %d", s.id, len(s.seen), events)
				}
				for n, v := range s.seen {
					if v.ID != s.id || v.Reason != "event-"+strconv.Itoa(n) || v.Metadata.AmountINR != int64(n) {
						rt.Fatalf("a reader of %s saw event %d = %+v", s.id, n, v)
					}
					if v.GuardianToken != "" {
						rt.Fatalf("an event carries the guardian token %q", v.GuardianToken)
					}
				}
				continue
			}

			// A subscriber that never reads holds the first events, up to the
			// buffer size, and misses the rest. It never blocked anyone.
			want := events
			if want > subscriberBuffer {
				want = subscriberBuffer
			}
			if got := len(s.ch); got != want {
				rt.Fatalf("a stuck subscriber of %s holds %d events, want %d", s.id, got, want)
			}
		}

		// Cancel everyone. The hub keeps nothing, and each channel is closed once
		// its remaining events are read.
		for _, s := range subs {
			s.cancel()
			s.cancel() // a second cancel is harmless
		}
		for _, id := range ids {
			if got := h.Subscribers(id); got != 0 {
				rt.Fatalf("Subscribers(%s) = %d after cancel, want 0", id, got)
			}
		}
		if len(h.subs) != 0 {
			rt.Fatalf("the hub still holds %d map entries after every cancel", len(h.subs))
		}
		for _, s := range subs {
			for range s.ch {
			}
		}

		// Publishing to a vault with no subscribers is harmless.
		publishWithin(rt, h, hubEvent(ids[0], 0))
	})
}

// A cancelled subscriber stops receiving while the others carry on.
func TestHubCancelStopsOnlyThatSubscriber(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe("v1")
	b, cancelB := h.Subscribe("v1")
	defer cancelB()

	h.Publish(hubEvent("v1", 1))
	cancelA()
	h.Publish(hubEvent("v1", 2))

	if got := len(b); got != 2 {
		t.Errorf("the remaining subscriber holds %d events, want 2", got)
	}
	var fromA int
	for range a {
		fromA++
	}
	if fromA != 1 {
		t.Errorf("the cancelled subscriber got %d events, want only the 1 sent before cancel", fromA)
	}
	if got := h.Subscribers("v1"); got != 1 {
		t.Errorf("Subscribers = %d, want 1", got)
	}
}

// TestHubConcurrentPublishAndCancel publishes and cancels from many goroutines.
// Run it with -race: a send on a closed channel would panic.
func TestHubConcurrentPublishAndCancel(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				h.Publish(hubEvent("v1", n))
			}
		}()
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				ch, cancel := h.Subscribe("v1")
				select {
				case <-ch:
				default:
				}
				cancel()
			}
		}()
	}
	wg.Wait()

	if got := h.Subscribers("v1"); got != 0 {
		t.Errorf("Subscribers = %d after every goroutine cancelled, want 0", got)
	}
}
