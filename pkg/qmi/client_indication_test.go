package qmi

import (
	"testing"
	"time"
)

func TestModemResetIndicationNotDroppedWhenQueueFull(t *testing.T) {
	c := &Client{
		opts:              ClientOptions{ReadDeadline: 5 * time.Millisecond},
		eventCh:           make(chan Event, 4),
		indicationInCh:    make(chan Event, 1),
		coalescedSignalCh: make(chan struct{}, 1),
		closeCh:           make(chan struct{}),
		coalesced: coalescedEventStore{
			events: make(map[string]Event),
		},
	}

	// Fill indication queue first so enqueueIndication must use coalesced fallback.
	c.indicationInCh <- Event{Type: EventUnknown}
	c.enqueueIndication(Event{
		Type:      EventModemReset,
		ServiceID: ServiceControl,
		MessageID: CTLRevokeClientIDInd,
	})

	done := make(chan struct{})
	c.wg.Add(1)
	go func() {
		c.indicationLoop()
		close(done)
	}()
	defer func() {
		close(c.closeCh)
		<-done
	}()

	deadline := time.After(1 * time.Second)
	for {
		select {
		case evt := <-c.eventCh:
			if evt.Type == EventModemReset {
				return
			}
		case <-deadline:
			t.Fatal("expected EventModemReset to be delivered")
		}
	}
}
