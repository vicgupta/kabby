package events

import (
	"reflect"
	"testing"
)

func TestEventBusFanOutAndOrder(t *testing.T) {
	bus := NewEventBus[int](4)
	a, unsubA := bus.Subscribe()
	b, unsubB := bus.Subscribe()

	bus.Emit(1)
	bus.Emit(2)
	bus.Emit(3)

	if got := <-a; got != 1 {
		t.Fatalf("a first = %d, want 1", got)
	}
	if got := <-b; got != 1 {
		t.Fatalf("b first = %d, want 1", got)
	}
	if got := <-a; got != 2 {
		t.Fatalf("a second = %d, want 2", got)
	}
	if got := <-b; got != 2 {
		t.Fatalf("b second = %d, want 2", got)
	}
	if got := <-a; got != 3 {
		t.Fatalf("a third = %d, want 3", got)
	}
	if got := <-b; got != 3 {
		t.Fatalf("b third = %d, want 3", got)
	}

	unsubA()
	bus.Emit(4)

	// b still gets 4...
	if got := <-b; got != 4 {
		t.Fatalf("b after unsubscribing a = %d, want 4", got)
	}
	// ...a does not: it was drained and closed, so receive yields (0, false).
	select {
	case got, open := <-a:
		if open {
			t.Fatalf("a after unsubscribe got %d, want nothing", got)
		}
	default:
	}

	unsubB()
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	bus := NewEventBus[string](2)
	ch, unsub := bus.Subscribe()
	unsub()
	_, open := <-ch
	if open {
		t.Fatal("unsubscribed channel should be closed")
	}
}

func TestStructEvent(t *testing.T) {
	// Ensures EventBus is usable with a struct event type (e.g. compaction).
	type compaction struct{ Reason string }
	bus := NewEventBus[compaction](2)
	ch, unsub := bus.Subscribe()
	bus.Emit(compaction{Reason: "budget"})
	got := <-ch
	if !reflect.DeepEqual(got, compaction{Reason: "budget"}) {
		t.Fatalf("got %+v", got)
	}
	unsub()
}
