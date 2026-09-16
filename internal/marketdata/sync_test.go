package marketdata

import (
	"context"
	"errors"
	"testing"
)

func TestSyncerAppliesSnapshotAndUpdate(t *testing.T) {
	book := NewBook("BTC-USD")
	syncer := NewSyncer(book, &fakeFeed{})

	if err := syncer.Apply(context.Background(), Message{
		Type:     MessageTypeSnapshot,
		Snapshot: &Snapshot{Symbol: "BTC-USD", Sequence: 1, Bids: []Level{{Price: 100, Quantity: 1}}},
	}); err != nil {
		t.Fatalf("Apply(snapshot) error = %v", err)
	}
	if err := syncer.Apply(context.Background(), Message{
		Type:   MessageTypeUpdate,
		Update: &Update{Symbol: "BTC-USD", Sequence: 2, Bids: []Level{{Price: 100, Quantity: 2}}},
	}); err != nil {
		t.Fatalf("Apply(update) error = %v", err)
	}

	snapshot := book.Snapshot()
	assertLevels(t, snapshot.Bids, []Level{{Price: 100, Quantity: 2}})
}

func TestSyncerTracksHeartbeats(t *testing.T) {
	syncer := NewSyncer(NewBook("BTC-USD"), &fakeFeed{})

	if err := syncer.Apply(context.Background(), Message{Type: MessageTypeHeartbeat}); err != nil {
		t.Fatalf("Apply(heartbeat) error = %v", err)
	}
	if syncer.Heartbeats() != 1 {
		t.Fatalf("Heartbeats() = %d, want 1", syncer.Heartbeats())
	}
}

func TestSyncerReconnectsOnGap(t *testing.T) {
	feed := &fakeFeed{}
	book := syncedBook(t)
	syncer := NewSyncer(book, feed)

	err := syncer.Apply(context.Background(), Message{
		Type:   MessageTypeUpdate,
		Update: &Update{Symbol: "BTC-USD", Sequence: 12},
	})
	if !errors.Is(err, ErrGapDetected) {
		t.Fatalf("Apply() error = %v, want %v", err, ErrGapDetected)
	}
	if syncer.Reconnects() != 1 || feed.reconnects != 1 {
		t.Fatalf("reconnects syncer=%d feed=%d, want 1", syncer.Reconnects(), feed.reconnects)
	}
}

func TestSyncerRejectsMalformedMessage(t *testing.T) {
	syncer := NewSyncer(NewBook("BTC-USD"), &fakeFeed{})

	err := syncer.Apply(context.Background(), Message{Type: MessageTypeSnapshot})
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("Apply() error = %v, want %v", err, ErrInvalidSnapshot)
	}
}

type fakeFeed struct {
	reconnects int
}

func (f *fakeFeed) Next(context.Context) (Message, error) {
	return Message{}, context.Canceled
}

func (f *fakeFeed) Reconnect(context.Context) error {
	f.reconnects++
	return nil
}
