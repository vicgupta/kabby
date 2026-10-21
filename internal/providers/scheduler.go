package providers

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrSlotBusy is returned when a background caller cannot get the inference slot.
var ErrSlotBusy = errors.New("inference slot busy")

// Scheduler ensures one inference slot per server URL per process.
// Coding (foreground) calls always win; background callers defer or cancel.
type Scheduler struct {
	mu    sync.Mutex
	slots map[string]*inferenceSlot
}

type inferenceSlot struct {
	mu    sync.Mutex
	cond  *sync.Cond
	inUse bool
	owner string // "foreground" or "background"
}

// NewScheduler creates a Scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{slots: make(map[string]*inferenceSlot)}
}

func (s *Scheduler) getOrCreateSlot(url string) *inferenceSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, ok := s.slots[url]
	if !ok {
		slot = &inferenceSlot{}
		slot.cond = sync.NewCond(&slot.mu)
		s.slots[url] = slot
	}
	return slot
}

// RunForeground executes fn with exclusive access to the inference slot for url.
// Blocks until the slot is available or ctx is cancelled.
func (s *Scheduler) RunForeground(ctx context.Context, url string, fn func() error) error {
	slot := s.getOrCreateSlot(url)

	slot.mu.Lock()
	for slot.inUse {
		// Wait with ctx awareness: unlock, wait briefly, re-check.
		slot.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		slot.mu.Lock()
	}
	slot.inUse = true
	slot.owner = "foreground"
	slot.mu.Unlock()

	defer func() {
		slot.mu.Lock()
		slot.inUse = false
		slot.owner = ""
		slot.mu.Unlock()
		slot.cond.Broadcast()
	}()

	return fn()
}

// RunBackground attempts to execute fn on the inference slot for url.
// If the slot is in use by a foreground call, returns ErrSlotBusy immediately.
// If in use by another background call, waits briefly or returns ErrSlotBusy.
func (s *Scheduler) RunBackground(ctx context.Context, url string, fn func() error) error {
	slot := s.getOrCreateSlot(url)

	slot.mu.Lock()
	if slot.inUse && slot.owner == "foreground" {
		slot.mu.Unlock()
		return ErrSlotBusy
	}
	for slot.inUse {
		slot.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		slot.mu.Lock()
		if slot.inUse && slot.owner == "foreground" {
			slot.mu.Unlock()
			return ErrSlotBusy
		}
	}
	slot.inUse = true
	slot.owner = "background"
	slot.mu.Unlock()

	defer func() {
		slot.mu.Lock()
		slot.inUse = false
		slot.owner = ""
		slot.mu.Unlock()
		slot.cond.Broadcast()
	}()

	return fn()
}
