package events

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEventDispatcherDispatchAndRegistration(t *testing.T) {
	dispatcher := NewEventDispatcher(nil)
	handled := false

	dispatcher.Register(EventUserRegistered, func(event *DomainEvent) error {
		handled = true
		if event.ID == "" || event.Timestamp.IsZero() || event.Version == 0 {
			t.Fatalf("expected dispatcher defaults to be populated")
		}
		return nil
	})

	sub := dispatcher.Subscribe([]EventType{EventUserRegistered})
	defer dispatcher.Unsubscribe(sub.ID)

	err := dispatcher.Dispatch(context.Background(), &DomainEvent{
		Type:          EventUserRegistered,
		AggregateID:   "agg-1",
		AggregateType: "User",
		Data:          map[string]interface{}{"x": "y"},
	})
	if err != nil {
		t.Fatalf("expected dispatch success, got %v", err)
	}
	if !handled {
		t.Fatalf("expected registered handler to run")
	}

	select {
	case ev := <-sub.Ch:
		if ev.Type != EventUserRegistered {
			t.Fatalf("expected event type propagated to subscriber")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("expected subscriber to receive event")
	}
}

func TestEventDispatcherHandlerErrorDoesNotFailDispatch(t *testing.T) {
	dispatcher := NewEventDispatcher(nil)
	dispatcher.Register(EventLoginFailed, func(event *DomainEvent) error {
		return errors.New("handler failure")
	})

	err := dispatcher.Dispatch(context.Background(), &DomainEvent{
		Type:          EventLoginFailed,
		AggregateType: "Auth",
		Data:          map[string]interface{}{"reason": "bad"},
	})
	if err != nil {
		t.Fatalf("expected dispatch to continue despite handler failure, got %v", err)
	}
}

func TestEventDispatcherSubscribeUnsubscribe(t *testing.T) {
	dispatcher := NewEventDispatcher(nil)
	sub := dispatcher.Subscribe(nil)
	dispatcher.Unsubscribe(sub.ID)

	_, open := <-sub.Ch
	if open {
		t.Fatalf("expected subscriber channel to be closed after unsubscribe")
	}
}
