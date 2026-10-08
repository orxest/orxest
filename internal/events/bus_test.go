package events_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/events"
)

type recordingSink struct {
	mu     sync.Mutex
	events []domain.Event
	err    error
}

func (s *recordingSink) Append(_ context.Context, e *domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, *e)
	return nil
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func TestBusPersistsAndFansOut(t *testing.T) {
	sink := &recordingSink{}
	bus := events.NewBus(sink, 4, nil)
	all := bus.Subscribe("")
	defer all.Close()
	projectA := bus.Subscribe("prj_a")
	defer projectA.Close()
	projectB := bus.Subscribe("prj_b")
	defer projectB.Close()

	ctx := context.Background()
	bus.Publish(ctx, domain.Event{ProjectID: "prj_a", Type: domain.EventTaskCreated, Message: "a"})
	bus.Publish(ctx, domain.Event{ProjectID: "prj_b", Type: domain.EventTaskCreated, Message: "b"})

	// Persistence happens for every event.
	if sink.count() != 2 {
		t.Fatalf("expected 2 persisted events, got %d", sink.count())
	}
	// The unscoped subscriber sees both, the scoped ones only their project.
	if got := drain(all.Events()); len(got) != 2 {
		t.Errorf("expected 2 events for the unscoped subscriber, got %d", len(got))
	}
	a := drain(projectA.Events())
	if len(a) != 1 || a[0].ProjectID != "prj_a" {
		t.Errorf("unexpected events for project A: %+v", a)
	}
	b := drain(projectB.Events())
	if len(b) != 1 || b[0].ProjectID != "prj_b" {
		t.Errorf("unexpected events for project B: %+v", b)
	}
}

func TestBusFillsMissingIdentity(t *testing.T) {
	bus := events.NewBus(nil, 4, nil)
	sub := bus.Subscribe("")
	defer sub.Close()
	bus.Publish(context.Background(), domain.Event{Type: domain.EventTaskReady})
	select {
	case evt := <-sub.Events():
		if evt.ID == "" {
			t.Error("expected an id to be assigned")
		}
		if evt.CreatedAt.IsZero() {
			t.Error("expected a timestamp to be assigned")
		}
	case <-time.After(time.Second):
		t.Fatal("expected the event to be delivered")
	}
}

func TestBusDropsForSlowSubscribersInsteadOfBlocking(t *testing.T) {
	bus := events.NewBus(nil, 1, nil)
	sub := bus.Subscribe("")
	defer sub.Close()
	// The first event fills the single buffered slot; the rest are dropped.
	for i := 0; i < 5; i++ {
		bus.Publish(context.Background(), domain.Event{Type: domain.EventTaskProgress})
	}
	if sub.Dropped() == 0 {
		t.Error("expected dropped events to be counted for a slow subscriber")
	}
	if got := len(drain(sub.Events())); got != 1 {
		t.Errorf("expected only the buffered event, got %d", got)
	}
}

func TestBusSurvivesSinkFailure(t *testing.T) {
	sink := &recordingSink{err: errors.New("database is on fire")}
	bus := events.NewBus(sink, 4, nil)
	sub := bus.Subscribe("")
	defer sub.Close()
	// A failing sink must never prevent live delivery.
	bus.Publish(context.Background(), domain.Event{Type: domain.EventTaskFailed})
	if got := len(drain(sub.Events())); got != 1 {
		t.Errorf("expected delivery despite the sink error, got %d", got)
	}
}

func TestSubscriptionCloseIsIdempotent(t *testing.T) {
	bus := events.NewBus(nil, 4, nil)
	sub := bus.Subscribe("")
	sub.Close()
	sub.Close()
	if _, open := <-sub.Events(); open {
		t.Error("expected the channel to be closed")
	}
	// Publishing after a subscriber left must not panic.
	bus.Publish(context.Background(), domain.Event{Type: domain.EventTaskCreated})
}

func TestStreamHubRoutesExecutionOutput(t *testing.T) {
	hub := events.NewStreamHub(4)
	execCh, cancelExec := hub.SubscribeExecution("exe_1")
	defer cancelExec()
	projectCh, cancelProject := hub.SubscribeProject("prj_a")
	defer cancelProject()

	if hub.SubscriberCount() != 2 {
		t.Fatalf("expected 2 subscribers, got %d", hub.SubscriberCount())
	}
	hub.Publish(domain.ExecutionEvent{ID: "e1", ExecutionID: "exe_1", ProjectID: "prj_a", Type: "output", Message: "hello"})
	hub.Publish(domain.ExecutionEvent{ID: "e2", ExecutionID: "exe_2", ProjectID: "prj_b", Type: "output", Message: "other"})

	exec := drainExecution(execCh)
	if len(exec) != 1 || exec[0].ID != "e1" {
		t.Errorf("unexpected execution events: %+v", exec)
	}
	project := drainExecution(projectCh)
	if len(project) != 1 || project[0].ProjectID != "prj_a" {
		t.Errorf("unexpected project events: %+v", project)
	}

	// Cancelling one subscription must not affect the other.
	cancelExec()
	if hub.SubscriberCount() != 1 {
		t.Errorf("expected 1 remaining subscriber, got %d", hub.SubscriberCount())
	}
}

func drain(ch <-chan domain.Event) []domain.Event {
	var out []domain.Event
	for {
		select {
		case evt, open := <-ch:
			if !open {
				return out
			}
			out = append(out, evt)
		default:
			return out
		}
	}
}

func drainExecution(ch <-chan domain.ExecutionEvent) []domain.ExecutionEvent {
	var out []domain.ExecutionEvent
	for {
		select {
		case evt, open := <-ch:
			if !open {
				return out
			}
			out = append(out, evt)
		default:
			return out
		}
	}
}
