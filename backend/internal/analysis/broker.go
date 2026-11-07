package analysis

import (
	"sync"

	"github.com/google/uuid"

	"leadintel/backend/internal/api"
)

// Event names on the analysis event stream (SPEC.md §7.4).
const (
	EventSnapshot  = "snapshot"
	EventProgress  = "progress"
	EventCompleted = "completed"
	EventFailed    = "failed"
	EventCancelled = "cancelled"
)

// StreamEvent is one server-sent event: Name is the SSE event type and Data
// is marshalled to JSON as its payload (api.AnalysisProgressEvent for
// `progress`, Job for the others).
type StreamEvent struct {
	Name string
	Data any
}

// terminalEvent returns the event name that ends the stream for a job in
// status s, or "" while the job is still active.
func terminalEvent(s Status) string {
	switch s {
	case api.JobSucceeded:
		return EventCompleted
	case api.JobFailed:
		return EventFailed
	case api.JobCancelled:
		return EventCancelled
	default:
		return ""
	}
}

// subscriberBuffer is how many events a subscriber may fall behind before it
// is dropped. A job emits at most ~20 events, so only a stuck client hits it.
const subscriberBuffer = 32

// Broker fans job events out to SSE subscribers in this process. It is a
// notification channel only: the database stays the source of truth, and
// every stream starts with a snapshot read from it, so a dropped event or
// subscriber costs nothing a reconnect does not repair.
type Broker struct {
	mu     sync.Mutex
	subs   map[uuid.UUID]map[chan StreamEvent]struct{}
	closed bool
}

func NewBroker() *Broker {
	return &Broker{subs: map[uuid.UUID]map[chan StreamEvent]struct{}{}}
}

// Subscribe registers a subscriber for the job's events. The channel is closed
// when the subscriber is dropped for falling behind, when the broker closes,
// or by unsubscribe, which is safe to call more than once.
func (b *Broker) Subscribe(jobID uuid.UUID) (<-chan StreamEvent, func()) {
	ch := make(chan StreamEvent, subscriberBuffer)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return ch, func() {}
	}
	if b.subs[jobID] == nil {
		b.subs[jobID] = map[chan StreamEvent]struct{}{}
	}
	b.subs[jobID][ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.remove(jobID, ch)
	}
}

// Publish delivers ev to every subscriber of the job without blocking. A
// subscriber whose buffer is full is dropped; its client reconnects and
// resumes from a fresh snapshot.
func (b *Broker) Publish(jobID uuid.UUID, ev StreamEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[jobID] {
		select {
		case ch <- ev:
		default:
			b.remove(jobID, ch)
		}
	}
}

// Close ends every stream and rejects new subscriptions. It is called on
// server shutdown so open SSE connections do not hold up the drain.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for jobID, set := range b.subs {
		for ch := range set {
			b.remove(jobID, ch)
		}
	}
}

// remove closes and forgets one subscriber. The caller holds b.mu.
func (b *Broker) remove(jobID uuid.UUID, ch chan StreamEvent) {
	set := b.subs[jobID]
	if _, ok := set[ch]; !ok {
		return
	}
	delete(set, ch)
	close(ch)
	if len(set) == 0 {
		delete(b.subs, jobID)
	}
}
