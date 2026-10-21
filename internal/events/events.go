// Package events provides EventBus, a typed fan-in/fan-out event bus used for
// cross-cutting notices such as compaction. Event order is preserved per
// subscriber.
package events

import "sync"

// EventBus[T] distributes events of type T to every current subscriber.
// Subscribers receive events on a buffered channel; slow subscribers do not
// block publishers (a full channel drops the event for that subscriber).
type EventBus[T any] struct {
	mu   sync.Mutex
	subs map[chan T]struct{}
	buf  int
}

// NewEventBus creates an EventBus with the given per-subscriber channel buffer.
func NewEventBus[T any](buf int) *EventBus[T] {
	return &EventBus[T]{subs: make(map[chan T]struct{}), buf: buf}
}

// Subscribe registers a new subscriber and returns its receive channel together
// with an unsubscribe function. Events emitted after subscription are delivered
// in order.
func (b *EventBus[T]) Subscribe() (<-chan T, func()) {
	ch := make(chan T, b.buf)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() { b.drop(ch) }
}

// drop removes a subscriber and closes its channel. Callers must invoke the
// unsubscribe function returned by Subscribe rather than calling drop directly.
func (b *EventBus[T]) drop(ch chan T) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
	close(ch)
}

// Emit sends evt to every subscriber. A subscriber whose channel is full does
// not receive this event (non-blocking send).
func (b *EventBus[T]) Emit(evt T) {
	b.mu.Lock()
	subs := make([]chan T, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
		}
	}
}
