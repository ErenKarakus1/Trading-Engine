package execution

import (
	"errors"
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/marketdata"
)

func TestSimulateBuyMarketOrderConsumesAsks(t *testing.T) {
	snapshot := testSnapshot()

	result, err := SimulateMarketOrder(snapshot, domain.SideBuy, 7)
	if err != nil {
		t.Fatalf("SimulateMarketOrder() error = %v", err)
	}

	if result.FilledQuantity != 7 || result.RemainingQuantity != 0 {
		t.Fatalf("filled=%d remaining=%d, want 7/0", result.FilledQuantity, result.RemainingQuantity)
	}
	if result.Notional != 709 {
		t.Fatalf("Notional = %d, want 709", result.Notional)
	}
	if result.AveragePrice != 101 {
		t.Fatalf("AveragePrice = %d, want 101", result.AveragePrice)
	}
	assertFills(t, result.Fills,
		Fill{Price: 101, Quantity: 5, Notional: 505},
		Fill{Price: 102, Quantity: 2, Notional: 204},
	)
	if result.SlippageBpsVsBest != 0 {
		t.Fatalf("SlippageBpsVsBest = %d, want 0", result.SlippageBpsVsBest)
	}
	if result.SlippageBpsVsMid != 0 {
		t.Fatalf("SlippageBpsVsMid = %d, want 0", result.SlippageBpsVsMid)
	}
}

func TestSimulateSellMarketOrderConsumesBids(t *testing.T) {
	snapshot := testSnapshot()

	result, err := SimulateMarketOrder(snapshot, domain.SideSell, 6)
	if err != nil {
		t.Fatalf("SimulateMarketOrder() error = %v", err)
	}

	if result.FilledQuantity != 6 || result.RemainingQuantity != 0 {
		t.Fatalf("filled=%d remaining=%d, want 6/0", result.FilledQuantity, result.RemainingQuantity)
	}
	if result.Notional != 600 {
		t.Fatalf("Notional = %d, want 600", result.Notional)
	}
	if result.AveragePrice != 100 {
		t.Fatalf("AveragePrice = %d, want 100", result.AveragePrice)
	}
	assertFills(t, result.Fills,
		Fill{Price: 101, Quantity: 3, Notional: 303},
		Fill{Price: 99, Quantity: 3, Notional: 297},
	)
}

func TestSimulateReportsPartialFill(t *testing.T) {
	result, err := SimulateMarketOrder(testSnapshot(), domain.SideBuy, 20)
	if err != nil {
		t.Fatalf("SimulateMarketOrder() error = %v", err)
	}

	if result.FilledQuantity != 15 || result.RemainingQuantity != 5 {
		t.Fatalf("filled=%d remaining=%d, want 15/5", result.FilledQuantity, result.RemainingQuantity)
	}
}

func TestSimulateDoesNotMutateSnapshot(t *testing.T) {
	snapshot := testSnapshot()
	before := snapshot

	if _, err := SimulateMarketOrder(snapshot, domain.SideBuy, 7); err != nil {
		t.Fatalf("SimulateMarketOrder() error = %v", err)
	}

	assertLevels(t, snapshot.Bids, before.Bids)
	assertLevels(t, snapshot.Asks, before.Asks)
}

func TestSimulateRejectsInvalidOrder(t *testing.T) {
	_, err := SimulateMarketOrder(testSnapshot(), domain.SideBuy, 0)
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("SimulateMarketOrder() error = %v, want %v", err, ErrInvalidOrder)
	}
}

func testSnapshot() marketdata.Snapshot {
	return marketdata.Snapshot{
		Symbol: "BTC-USD",
		Bids: []marketdata.Level{
			{Price: 101, Quantity: 3},
			{Price: 99, Quantity: 4},
		},
		Asks: []marketdata.Level{
			{Price: 101, Quantity: 5},
			{Price: 102, Quantity: 10},
		},
	}
}

func assertFills(t *testing.T, got []Fill, want ...Fill) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(Fills) = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Fills[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func assertLevels(t *testing.T, got, want []marketdata.Level) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(levels) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("levels[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
