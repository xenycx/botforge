package events

import (
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribeAndIsolation(t *testing.T) {
	b := NewBus()
	a, other := b.Subscribe("a"), b.Subscribe("b")
	defer a.Close()
	defer other.Close()
	b.Publish(Status{BotID: "a", ObservedState: "running"})
	select {
	case st := <-a.C:
		if st.ObservedState != "running" {
			t.Fatal(st)
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
	select {
	case st := <-other.C:
		t.Fatalf("leaked across bots: %+v", st)
	default:
	}
}

func TestSlowSubscriberNeverBlocksAndKeepsLatest(t *testing.T) {
	b := NewBus()
	s := b.Subscribe("a")
	defer s.Close()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Publish(Status{BotID: "a", Generation: int64(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked by a stalled subscriber")
	}
	var last int64 = -1
	for len(s.C) > 0 {
		last = (<-s.C).Generation
	}
	if last != 999 || cap(s.C) != 4 {
		t.Fatalf("latest state lost: %d", last)
	}
}

func TestCloseAndNilBus(t *testing.T) {
	b := NewBus()
	s := b.Subscribe("a")
	s.Close()
	s.Close()
	if b.Subscribers("a") != 0 {
		t.Fatal("still subscribed")
	}
	b.Publish(Status{BotID: "a"}) // no subscribers: fine
	var nilBus *Bus
	nilBus.Publish(Status{})
}

func TestConcurrentUse(t *testing.T) {
	b := NewBus()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s := b.Subscribe("a")
				b.Publish(Status{BotID: "a"})
				s.Close()
			}
		}()
	}
	wg.Wait()
}
