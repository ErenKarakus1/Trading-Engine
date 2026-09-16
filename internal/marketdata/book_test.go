package marketdata

import (
	"errors"
	"testing"
)

func TestApplySnapshotReplacesBook(t *testing.T) {
	book := NewBook("BTC-USD")

	if err := book.ApplySnapshot(Snapshot{
		Symbol:   "BTC-USD",
		Sequence: 10,
		Bids:     []Level{{Price: 100, Quantity: 2}, {Price: 99, Quantity: 3}},
		Asks:     []Level{{Price: 101, Quantity: 4}, {Price: 102, Quantity: 5}},
	}); err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}

	snapshot := book.Snapshot()
	if snapshot.Sequence != 10 {
		t.Fatalf("Sequence = %d, want 10", snapshot.Sequence)
	}
	assertLevels(t, snapshot.Bids, []Level{{Price: 100, Quantity: 2}, {Price: 99, Quantity: 3}})
	assertLevels(t, snapshot.Asks, []Level{{Price: 101, Quantity: 4}, {Price: 102, Quantity: 5}})
	if !book.Synced() {
		t.Fatal("Synced() = false, want true")
	}
}

func TestApplyUpdateChangesAndRemovesLevels(t *testing.T) {
	book := syncedBook(t)

	err := book.ApplyUpdate(Update{
		Symbol:   "BTC-USD",
		Sequence: 11,
		Bids:     []Level{{Price: 100, Quantity: 0}, {Price: 98, Quantity: 7}},
		Asks:     []Level{{Price: 101, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("ApplyUpdate() error = %v", err)
	}

	snapshot := book.Snapshot()
	assertLevels(t, snapshot.Bids, []Level{{Price: 99, Quantity: 3}, {Price: 98, Quantity: 7}})
	assertLevels(t, snapshot.Asks, []Level{{Price: 101, Quantity: 1}, {Price: 102, Quantity: 5}})
}

func TestApplyUpdateDetectsGapAndMarksUnsynced(t *testing.T) {
	book := syncedBook(t)

	err := book.ApplyUpdate(Update{Symbol: "BTC-USD", Sequence: 12})
	if !errors.Is(err, ErrGapDetected) {
		t.Fatalf("ApplyUpdate() error = %v, want %v", err, ErrGapDetected)
	}
	if book.Synced() {
		t.Fatal("Synced() = true, want false")
	}

	err = book.ApplyUpdate(Update{Symbol: "BTC-USD", Sequence: 13})
	if !errors.Is(err, ErrNotSynced) {
		t.Fatalf("ApplyUpdate() after gap error = %v, want %v", err, ErrNotSynced)
	}
}

func TestRejectsWrongSymbol(t *testing.T) {
	book := NewBook("BTC-USD")

	err := book.ApplySnapshot(Snapshot{Symbol: "ETH-USD", Sequence: 1})
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("ApplySnapshot() error = %v, want %v", err, ErrInvalidSnapshot)
	}
}

func TestRejectsMalformedLevels(t *testing.T) {
	book := NewBook("BTC-USD")

	err := book.ApplySnapshot(Snapshot{
		Symbol:   "BTC-USD",
		Sequence: 1,
		Bids:     []Level{{Price: 0, Quantity: 1}},
	})
	if !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("ApplySnapshot() error = %v, want %v", err, ErrInvalidUpdate)
	}
}

func syncedBook(t *testing.T) *Book {
	t.Helper()
	book := NewBook("BTC-USD")
	if err := book.ApplySnapshot(Snapshot{
		Symbol:   "BTC-USD",
		Sequence: 10,
		Bids:     []Level{{Price: 100, Quantity: 2}, {Price: 99, Quantity: 3}},
		Asks:     []Level{{Price: 101, Quantity: 4}, {Price: 102, Quantity: 5}},
	}); err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}
	return book
}

func assertLevels(t *testing.T, got, want []Level) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(levels) = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("levels[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
