// Package events provides the in-process event bus that powers the activity
// feed and the Server-Sent Events endpoints (spec §32).
//
// Two distinct streams exist:
//
//   - Bus: low volume orchestration events. They are persisted for history and
//     fanned out to SSE subscribers.
//   - StreamHub: high volume execution output. It is persisted separately in
//     execution_events with a retention limit and streamed live; it is *not*
//     duplicated into the orchestration event table.
package events

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/orxest/orxest/internal/domain"
)

// Sink persists orchestration events. It is satisfied by
// repository.EventRepository; the bus only needs Append.
type Sink interface {
	Append(ctx context.Context, e *domain.Event) error
}

// Bus is a fan-out bus for orchestration events.
type Bus struct {
	mu     sync.RWMutex
	subs   map[int]*Subscription
	nextID int
	buffer int
	sink   Sink
	now    func() time.Time
	newID  func() string
	log    *slog.Logger
	// Dropped counts events that could not be delivered to a slow subscriber.
	droppedMu sync.Mutex
	dropped   int64
}

// NewBus creates a bus. buffer is the per-subscriber channel capacity.
func NewBus(sink Sink, buffer int, log *slog.Logger) *Bus {
	if buffer <= 0 {
		buffer = 256
	}
	if log == nil {
		log = slog.Default()
	}
	return &Bus{
		subs:   map[int]*Subscription{},
		buffer: buffer,
		sink:   sink,
		now:    func() time.Time { return time.Now().UTC() },
		newID:  domain.NewEventID,
		log:    log,
	}
}

// Publish persists then delivers an event. Delivery never blocks the caller:
// a subscriber that cannot keep up loses events and is flagged.
func (b *Bus) Publish(ctx context.Context, evt domain.Event) {
	if evt.ID == "" {
		evt.ID = b.newID()
	}
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = b.now()
	}
	if b.sink != nil {
		if err := b.sink.Append(ctx, &evt); err != nil {
			b.log.WarnContext(ctx, "persisting orchestration event failed",
				slog.String("event_type", evt.Type),
				slog.String("event_id", evt.ID),
				slog.String("error", err.Error()))
		}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subs {
		if !sub.matches(evt) {
			continue
		}
		select {
		case sub.ch <- evt:
		default:
			sub.dropOne()
		}
	}
}

// Subscription is one consumer of the bus.
type Subscription struct {
	// ID identifies the subscription.
	ID int
	// ProjectID scopes the subscription; empty means "all projects".
	ProjectID string
	ch        chan domain.Event
	bus       *Bus
	closeOnce sync.Once
	dropped   int64
}

func (s *Subscription) matches(evt domain.Event) bool {
	if s.ProjectID == "" {
		return true
	}
	return evt.ProjectID == s.ProjectID
}

func (s *Subscription) dropOne() {
	s.dropped++
}

// Events returns the delivery channel.
func (s *Subscription) Events() <-chan domain.Event { return s.ch }

// Dropped reports how many events were skipped for this subscriber.
func (s *Subscription) Dropped() int64 { return s.dropped }

// Close removes the subscription from the bus.
func (s *Subscription) Close() {
	s.closeOnce.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s.ID)
		s.bus.mu.Unlock()
		close(s.ch)
	})
}

// Subscribe registers a subscriber. Call Close to release it.
func (b *Bus) Subscribe(projectID string) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	sub := &Subscription{
		ID:        b.nextID,
		ProjectID: projectID,
		ch:        make(chan domain.Event, b.buffer),
		bus:       b,
	}
	b.subs[sub.ID] = sub
	return sub
}

// StreamHub fans out high volume execution output.
type StreamHub struct {
	mu          sync.RWMutex
	byExecution map[string]map[int]chan domain.ExecutionEvent
	byProject   map[string]map[int]chan domain.ExecutionEvent
	nextID      int
	buffer      int
}

// NewStreamHub creates a hub with the given per-subscriber buffer.
func NewStreamHub(buffer int) *StreamHub {
	if buffer <= 0 {
		buffer = 1024
	}
	return &StreamHub{
		byExecution: map[string]map[int]chan domain.ExecutionEvent{},
		byProject:   map[string]map[int]chan domain.ExecutionEvent{},
		buffer:      buffer,
	}
}

// SubscribeExecution streams events of one execution.
func (h *StreamHub) SubscribeExecution(executionID string) (<-chan domain.ExecutionEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	ch := make(chan domain.ExecutionEvent, h.buffer)
	if h.byExecution[executionID] == nil {
		h.byExecution[executionID] = map[int]chan domain.ExecutionEvent{}
	}
	h.byExecution[executionID][id] = ch
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if subs, ok := h.byExecution[executionID]; ok {
			if c, ok := subs[id]; ok {
				delete(subs, id)
				close(c)
			}
			if len(subs) == 0 {
				delete(h.byExecution, executionID)
			}
		}
	}
}

// SubscribeProject streams execution output of every execution in a project.
func (h *StreamHub) SubscribeProject(projectID string) (<-chan domain.ExecutionEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	ch := make(chan domain.ExecutionEvent, h.buffer)
	if h.byProject[projectID] == nil {
		h.byProject[projectID] = map[int]chan domain.ExecutionEvent{}
	}
	h.byProject[projectID][id] = ch
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if subs, ok := h.byProject[projectID]; ok {
			if c, ok := subs[id]; ok {
				delete(subs, id)
				close(c)
			}
			if len(subs) == 0 {
				delete(h.byProject, projectID)
			}
		}
	}
}

// Publish delivers an execution event to all matching subscribers.
func (h *StreamHub) Publish(evt domain.ExecutionEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.byExecution[evt.ExecutionID] {
		select {
		case ch <- evt:
		default:
		}
	}
	if evt.ProjectID != "" {
		for _, ch := range h.byProject[evt.ProjectID] {
			select {
			case ch <- evt:
			default:
			}
		}
	}
}

// SubscriberCount reports the number of live subscribers, for diagnostics.
func (h *StreamHub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, subs := range h.byExecution {
		n += len(subs)
	}
	for _, subs := range h.byProject {
		n += len(subs)
	}
	return n
}
