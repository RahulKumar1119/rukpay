package guardian

import (
	"encoding/json"
	"sync"

	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// subscriberBuffer is the capacity of each subscription's channel. A subscriber
// that falls further behind than this loses the newest events (Requirement 8.8).
const subscriberBuffer = 8

// Hub fans vault changes out to the open event streams, keyed by vault id. It
// is in-process: on Lambda every instance has its own hub, so the client falls
// back to polling (Requirement 8.6).
//
// The zero value is ready to use. Hub implements vault.Observer.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan []byte]struct{}
}

var _ vault.Observer = (*Hub)(nil)

// NewHub returns an empty Hub.
func NewHub() *Hub { return &Hub{} }

// Subscribe registers a subscriber for the vault id and returns the channel its
// events arrive on, and a cancel function. Each event is the JSON form of a
// vault.View without the guardian token. The channel holds up to
// subscriberBuffer events.
//
// cancel removes the subscription, closes the channel and drops the vault's map
// entry when no subscriber is left (Requirement 8.3). It is safe to call more
// than once.
func (h *Hub) Subscribe(id string) (events <-chan []byte, cancel func()) {
	ch := make(chan []byte, subscriberBuffer)

	h.mu.Lock()
	if h.subs == nil {
		h.subs = make(map[string]map[chan []byte]struct{})
	}
	if h.subs[id] == nil {
		h.subs[id] = make(map[chan []byte]struct{})
	}
	h.subs[id][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel = func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if set, ok := h.subs[id]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(h.subs, id)
				}
			}
			close(ch)
		})
	}
	return ch, cancel
}

// Publish sends v to every subscriber of v.ID. It marshals the event once,
// without the guardian token: an event carries the state, and the guardian panel
// already holds its token from the read endpoint (Requirement 8.9).
//
// Publish never blocks. A subscriber whose channel is full misses the event
// (Requirement 8.8). Sends happen under the lock, which cancel also takes before
// it closes a channel, so a send never reaches a closed channel.
func (h *Hub) Publish(v vault.View) {
	v.GuardianToken = ""
	event, err := json.Marshal(v)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[v.ID] {
		select {
		case ch <- event:
		default:
		}
	}
}

// Subscribers returns how many subscribers are registered for the vault id.
func (h *Hub) Subscribers(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[id])
}
