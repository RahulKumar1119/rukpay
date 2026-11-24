package store

import (
	"context"
	"slices"
)

// historyKey identifies one History_Event within a payer: its time and id, as
// in the DynamoDB sort key.
type historyKey struct {
	at int64
	id string
}

// AppendHistory stores a copy of ev under payer. See HistoryStore.
func (s *MemoryStore) AppendHistory(ctx context.Context, payer string, ev HistoryEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateHistoryEvent(ev); err != nil {
		return &opError{Op: opAppendHistory, ID: ev.ID, Err: err}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := historyKey{at: ev.At, id: ev.ID}
	events := s.history[payer]
	if _, ok := events[key]; ok {
		return &opError{Op: opAppendHistory, ID: ev.ID, Err: ErrAlreadyExists}
	}
	if events == nil {
		events = make(map[historyKey]HistoryEvent)
		s.history[payer] = events
	}
	events[key] = ev
	return nil
}

// RecentHistory returns the unexpired events among the limit newest events of
// payer, oldest first. See HistoryStore. Expired events are hidden but not
// removed, as for vault records. The result is a fresh slice of copies.
func (s *MemoryStore) RecentHistory(ctx context.Context, payer string, limit int) ([]HistoryEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 {
		return []HistoryEvent{}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	stored := s.history[payer]
	newest := make([]HistoryEvent, 0, len(stored))
	for _, ev := range stored {
		newest = append(newest, ev)
	}
	// Newest first, so the first limit events are the ones to examine.
	slices.SortFunc(newest, func(a, b HistoryEvent) int { return compareHistory(b, a) })
	if len(newest) > limit {
		newest = newest[:limit]
	}

	now := s.now()
	out := make([]HistoryEvent, 0, len(newest))
	for i := len(newest) - 1; i >= 0; i-- {
		if !historyExpired(newest[i], now) {
			out = append(out, newest[i])
		}
	}
	return out, nil
}
